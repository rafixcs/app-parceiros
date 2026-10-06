package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/rafixcs/app-parceiros/backend/internal/domain"
)

const (
	videoPartTTL       = time.Hour
	videoReadTTL       = 6 * time.Hour
	videoDownloadTTL   = 5 * time.Minute
	videoInternalTTL   = time.Hour
	uploadDeadline     = 24 * time.Hour
	embedRevalidateGap = 7 * 24 * time.Hour
)

// videoFormats are the accepted uploads, by the Content-Type the browser
// reports.
var videoFormats = map[string]string{
	"video/mp4":       ".mp4",
	"video/quicktime": ".mov",
	"video/webm":      ".webm",
}

// mediaProducts reads the catalog (service.ProductService).
type mediaProducts interface {
	Products(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]domain.Product, error)
}

// mediaAccounts reads the limits of the plan (service.AccountService).
type mediaAccounts interface {
	PlanLimit(ctx context.Context, m domain.Member, key string) (int64, error)
}

// MediaService handles the video library: references to other creators'
// videos by embed (only the metadata of the official oEmbed; the video plays
// in the platform's player and is never downloaded) and the user's own videos,
// sent straight to the bucket by presigned URLs, with thumbnail and preview
// made by ffmpeg in the worker.
//
// A video belongs to its owner inside the workspace. Owner and mentors of a
// mentorship can share theirs with the group. Links tie videos to catalog
// products and to curated lists; the curation module creates the list links
// through this service (LinkList, UnlinkList, UnlinkTarget, ForTargets).
type MediaService struct {
	repo     domain.MediaRepository
	tx       domain.Transactor
	products mediaProducts
	accounts mediaAccounts
	embeds   domain.EmbedResolver
	// objects is nil without a bucket: only embeds work.
	objects domain.ObjectStore
	// processor is only used in the worker.
	processor domain.VideoProcessor
	// queue is nil when nothing is scheduled.
	queue domain.MediaQueue
	log   *slog.Logger
	now   func() time.Time
}

func NewMediaService(repo domain.MediaRepository, tx domain.Transactor, products mediaProducts, accounts mediaAccounts,
	embeds domain.EmbedResolver, objects domain.ObjectStore, processor domain.VideoProcessor, queue domain.MediaQueue,
	log *slog.Logger,
) *MediaService {
	return &MediaService{
		repo: repo, tx: tx, products: products, accounts: accounts, embeds: embeds, objects: objects,
		processor: processor, queue: queue, log: log, now: time.Now,
	}
}

// Videos lists the videos the member sees in the workspace: theirs and the
// shared ones. With productID, only those linked to the product; with
// onlyMine, only theirs.
func (s *MediaService) Videos(ctx context.Context, m domain.Member, productID *uuid.UUID, onlyMine bool) ([]domain.VideoView, error) {
	if productID != nil {
		by, err := s.ForTargets(ctx, m, domain.TargetProduct, []uuid.UUID{*productID})
		if err != nil {
			return nil, err
		}
		out := []domain.VideoView{}
		for _, v := range by[*productID] {
			if !onlyMine || v.Mine {
				out = append(out, v)
			}
		}
		return out, nil
	}
	rows, err := s.repo.Videos(ctx, m.Actor(), onlyMine)
	if err != nil {
		return nil, err
	}
	return s.views(ctx, m.Actor(), rows)
}

// ForTargets returns, per target, the videos linked to it that the member
// sees.
func (s *MediaService) ForTargets(ctx context.Context, m domain.Member, target domain.VideoTarget, ids []uuid.UUID) (map[uuid.UUID][]domain.VideoView, error) {
	out := map[uuid.UUID][]domain.VideoView{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := s.repo.VideosForTargets(ctx, m.Actor(), target, ids)
	if err != nil {
		return nil, err
	}
	videos := make([]domain.Video, 0, len(rows))
	seen := map[uuid.UUID]bool{}
	for _, r := range rows {
		if !seen[r.Video.ID] {
			seen[r.Video.ID] = true
			videos = append(videos, r.Video)
		}
	}
	views, err := s.views(ctx, m.Actor(), videos)
	if err != nil {
		return nil, err
	}
	byID := make(map[uuid.UUID]domain.VideoView, len(views))
	for _, v := range views {
		byID[v.ID] = v
	}
	for _, r := range rows {
		out[r.TargetID] = append(out[r.TargetID], byID[r.Video.ID])
	}
	return out, nil
}

// Get returns a video the member sees.
func (s *MediaService) Get(ctx context.Context, m domain.Member, id uuid.UUID) (domain.VideoView, error) {
	v, err := s.video(ctx, m.Actor(), id)
	if err != nil {
		return domain.VideoView{}, err
	}
	vs, err := s.views(ctx, m.Actor(), []domain.Video{v})
	if err != nil {
		return domain.VideoView{}, err
	}
	return vs[0], nil
}

func (s *MediaService) video(ctx context.Context, a domain.Actor, id uuid.UUID) (domain.Video, error) {
	v, err := s.repo.Video(ctx, a, id)
	if errors.Is(err, domain.ErrNotFound) {
		return v, domain.ErrVideoNotFound
	}
	return v, err
}

// own returns the actor's own video: not found when they do not see it,
// owner only when it is someone else's (shared).
func (s *MediaService) own(ctx context.Context, a domain.Actor, id uuid.UUID) (domain.Video, error) {
	v, err := s.video(ctx, a, id)
	if err != nil {
		return v, err
	}
	if v.OwnerID != a.UserID {
		return v, domain.ErrVideoOwnerOnly
	}
	return v, nil
}

// PasteLink resolves a YouTube or TikTok link through the official oEmbed and
// saves the reference (metadata only; the video is never downloaded). Pasting
// the same video again returns the existing one, refreshed. With productID,
// the video also links to the product. It says whether the video was created.
func (s *MediaService) PasteLink(ctx context.Context, m domain.Member, link string, productID *uuid.UUID) (domain.VideoView, bool, error) {
	a := m.Actor()
	if productID != nil {
		if err := s.requireProduct(ctx, *productID); err != nil {
			return domain.VideoView{}, false, err
		}
	}
	ref, err := s.embeds.Resolve(ctx, link, true)
	if err != nil {
		if errors.Is(err, domain.ErrPlatformUnavailable) {
			s.log.WarnContext(ctx, "oEmbed did not answer", "err", err)
		}
		return domain.VideoView{}, false, err
	}
	var id uuid.UUID
	var created bool
	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		var err error
		id, created, err = s.repo.CreateEmbed(ctx, a, domain.NewEmbedVideo{
			Platform: ref.Platform, Title: ref.Title, Author: ref.Author, URL: ref.URL, EmbedID: ref.EmbedID,
			ThumbnailURL: mediaOptional(ref.ThumbnailURL),
		})
		if err != nil || productID == nil {
			return err
		}
		return s.repo.Link(ctx, a, id, domain.TargetProduct, *productID)
	})
	if err != nil {
		return domain.VideoView{}, false, err
	}
	if created {
		s.schedule(ctx, "revalidate_embed", func() error {
			return s.queue.ScheduleRevalidateEmbed(ctx, videoJob(a, id), s.now().Add(embedRevalidateGap))
		})
	}
	v, err := s.Get(ctx, m, id)
	return v, created, err
}

// schedule enqueues a maintenance job. If the queue fails the video keeps
// working; it only misses the revalidation or the automatic cleanup.
func (s *MediaService) schedule(ctx context.Context, kind string, fn func() error) {
	if s.queue == nil {
		return
	}
	if err := fn(); err != nil {
		s.log.ErrorContext(ctx, "could not schedule media job", "kind", kind, "err", err)
	}
}

func videoJob(a domain.Actor, id uuid.UUID) domain.VideoJob {
	return domain.VideoJob{VideoID: id, WorkspaceID: a.WorkspaceID, OwnerID: a.UserID}
}

// StartUpload checks the file, reserves its size in the quota of the
// workspace and opens the multipart upload in the bucket. The browser sends
// the parts straight to the bucket, through the URLs of SignPart. An upload
// that does not finish in 24 h is discarded by clean_upload.
func (s *MediaService) StartUpload(ctx context.Context, m domain.Member, n domain.NewUpload) (domain.StartedUpload, error) {
	a := m.Actor()
	if s.objects == nil {
		return domain.StartedUpload{}, domain.ErrUploadsUnavailable
	}
	if !n.UsageRights {
		return domain.StartedUpload{}, domain.ErrUsageRightsRequired
	}
	ct := strings.ToLower(strings.TrimSpace(n.ContentType))
	if _, ok := videoFormats[ct]; !ok {
		return domain.StartedUpload{}, domain.ErrInvalidVideoFormat
	}
	if n.SizeBytes <= 0 {
		return domain.StartedUpload{}, domain.ErrInvalidVideoSize
	}
	if n.SizeBytes > domain.MaxVideoBytes {
		return domain.StartedUpload{}, domain.ErrVideoTooLarge
	}
	name := videoFileName(n.FileName)
	if n.ProductID != nil {
		if err := s.requireProduct(ctx, *n.ProductID); err != nil {
			return domain.StartedUpload{}, err
		}
	}
	limit, err := s.accounts.PlanLimit(ctx, m, domain.LimitVideoBytes)
	if err != nil {
		return domain.StartedUpload{}, err
	}
	if n.SizeBytes > limit {
		return domain.StartedUpload{}, domain.ErrVideoQuotaExceeded
	}

	id := uuid.New()
	prefix := fmt.Sprintf("videos/%s/%s/", a.WorkspaceID, id)
	key := prefix + "original"
	uploadID, err := s.objects.StartMultipart(ctx, key, ct)
	if err != nil {
		return domain.StartedUpload{}, err
	}
	var row domain.Video
	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		used, err := s.repo.AddUsage(ctx, a, n.SizeBytes)
		if err != nil {
			return err
		}
		if used > limit {
			return domain.ErrVideoQuotaExceeded
		}
		row, err = s.repo.CreateUpload(ctx, a, domain.NewUploadVideo{
			ID: id, Title: titleFromFileName(name), StorageKey: prefix, UploadID: uploadID, FileName: name,
			ContentType: ct, SizeBytes: n.SizeBytes,
		})
		if err != nil || n.ProductID == nil {
			return err
		}
		return s.repo.Link(ctx, a, id, domain.TargetProduct, *n.ProductID)
	})
	if err != nil {
		if errAbort := s.objects.AbortMultipart(ctx, key, uploadID); errAbort != nil {
			s.log.ErrorContext(ctx, "open upload left without a video", "key", prefix, "err", errAbort)
		}
		return domain.StartedUpload{}, err
	}
	s.schedule(ctx, "clean_upload", func() error {
		return s.queue.ScheduleCleanUpload(ctx, videoJob(a, id), s.now().Add(uploadDeadline))
	})
	vs, err := s.views(ctx, a, []domain.Video{row})
	if err != nil {
		return domain.StartedUpload{}, err
	}
	return domain.StartedUpload{Video: vs[0], UploadID: uploadID, Key: key}, nil
}

// uploading returns the actor's video whose upload is still open.
func (s *MediaService) uploading(ctx context.Context, a domain.Actor, id uuid.UUID) (domain.Video, error) {
	if s.objects == nil {
		return domain.Video{}, domain.ErrUploadsUnavailable
	}
	v, err := s.own(ctx, a, id)
	if err != nil {
		return v, err
	}
	if v.Status != domain.VideoUploading || v.UploadID == nil {
		return v, domain.ErrUploadClosed
	}
	return v, nil
}

// SignPart returns the URL for the browser to send one part of the upload.
func (s *MediaService) SignPart(ctx context.Context, m domain.Member, id uuid.UUID, number int) (string, error) {
	if number < 1 || number > domain.MaxVideoParts {
		return "", domain.ErrInvalidPartNumber
	}
	v, err := s.uploading(ctx, m.Actor(), id)
	if err != nil {
		return "", err
	}
	return s.objects.SignPart(ctx, v.OriginalKey(), *v.UploadID, number, videoPartTTL)
}

// Parts lists the parts already sent, to resume an upload.
func (s *MediaService) Parts(ctx context.Context, m domain.Member, id uuid.UUID) ([]domain.UploadPart, error) {
	v, err := s.uploading(ctx, m.Actor(), id)
	if err != nil {
		return nil, err
	}
	ps, err := s.objects.Parts(ctx, v.OriginalKey(), *v.UploadID)
	if errors.Is(err, domain.ErrNotFound) {
		return nil, domain.ErrUploadClosed
	}
	return ps, err
}

// CompleteUpload joins the parts, checks the size against what was reserved
// in the quota and queues process_video.
func (s *MediaService) CompleteUpload(ctx context.Context, m domain.Member, id uuid.UUID, parts []domain.UploadPart) (domain.VideoView, error) {
	a := m.Actor()
	if len(parts) == 0 || len(parts) > domain.MaxVideoParts {
		return domain.VideoView{}, domain.ErrUploadPartsRequired
	}
	v, err := s.uploading(ctx, a, id)
	if err != nil {
		return domain.VideoView{}, err
	}
	key := v.OriginalKey()
	if err := s.objects.CompleteMultipart(ctx, key, *v.UploadID, parts); err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return domain.VideoView{}, domain.ErrUploadClosed
		}
		// Missing parts or a wrong ETag: the upload stays open.
		s.log.WarnContext(ctx, "could not complete the upload", "video_id", id, "err", err)
		return domain.VideoView{}, domain.ErrUploadIncomplete
	}
	info, err := s.objects.Info(ctx, key)
	if err != nil {
		return domain.VideoView{}, err
	}
	if info.Size > v.SizeBytes {
		// Larger than reserved in the quota: discarded.
		if err := s.delete(ctx, a, id); err != nil {
			return domain.VideoView{}, err
		}
		return domain.VideoView{}, domain.ErrUploadIncomplete
	}
	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		cur, err := s.repo.LockOwnVideo(ctx, a, id)
		if errors.Is(err, domain.ErrNotFound) {
			return domain.ErrVideoNotFound
		}
		if err != nil {
			return err
		}
		if cur.Status != domain.VideoUploading {
			return domain.ErrUploadClosed
		}
		if _, err := s.repo.AddUsage(ctx, a, info.Size-cur.SizeBytes); err != nil {
			return err
		}
		return s.repo.MarkUploaded(ctx, a, id, info.Size)
	})
	if err != nil {
		return domain.VideoView{}, err
	}
	if s.queue != nil {
		if err := s.queue.EnqueueProcessVideo(ctx, videoJob(a, id)); err != nil {
			s.log.ErrorContext(ctx, "could not enqueue process_video", "video_id", id, "err", err)
			s.setStatus(ctx, a, id, domain.VideoFailed)
		}
	}
	return s.Get(ctx, m, id)
}

func (s *MediaService) setStatus(ctx context.Context, a domain.Actor, id uuid.UUID, st domain.VideoStatus) {
	if err := s.repo.SetStatus(ctx, a, id, st); err != nil {
		s.log.ErrorContext(ctx, "could not set the video status", "video_id", id, "status", st, "err", err)
	}
}

// Delete deletes the video, its files in the bucket (or the open upload) and
// its links, and gives the space back to the quota.
func (s *MediaService) Delete(ctx context.Context, m domain.Member, id uuid.UUID) error {
	if _, err := s.own(ctx, m.Actor(), id); err != nil {
		return err
	}
	return s.delete(ctx, m.Actor(), id)
}

func (s *MediaService) delete(ctx context.Context, a domain.Actor, id uuid.UUID) error {
	return s.tx.WithinTx(ctx, func(ctx context.Context) error {
		v, err := s.repo.LockOwnVideo(ctx, a, id)
		if errors.Is(err, domain.ErrNotFound) {
			return domain.ErrVideoNotFound
		}
		if err != nil {
			return err
		}
		if err := s.repo.DeleteVideo(ctx, a, id); err != nil {
			return err
		}
		if v.Kind != domain.VideoUpload {
			return nil
		}
		if _, err := s.repo.AddUsage(ctx, a, -v.SizeBytes); err != nil {
			return err
		}
		// The files go before the commit: if the bucket fails, the video stays.
		if s.objects == nil {
			return domain.ErrUploadsUnavailable
		}
		if v.UploadID != nil {
			if err := s.objects.AbortMultipart(ctx, v.OriginalKey(), *v.UploadID); err != nil {
				return err
			}
		}
		return s.objects.DeletePrefix(ctx, *v.StorageKey)
	})
}

// Update changes the title and the sharing with the group (nil keeps).
func (s *MediaService) Update(ctx context.Context, m domain.Member, id uuid.UUID, title *string, shared *bool) (domain.VideoView, error) {
	a := m.Actor()
	if _, err := s.own(ctx, a, id); err != nil {
		return domain.VideoView{}, err
	}
	var t string
	if title != nil {
		t = strings.Join(strings.Fields(*title), " ")
		if utf8.RuneCountInString(t) > domain.MaxVideoText {
			return domain.VideoView{}, domain.ErrVideoTitleTooLong
		}
	}
	if shared != nil && *shared && !canShareVideos(m) {
		return domain.VideoView{}, domain.ErrVideoShareManagers
	}
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		if title != nil {
			if err := s.repo.Rename(ctx, a, id, t); err != nil {
				return err
			}
		}
		if shared != nil {
			return s.repo.SetShared(ctx, a, id, *shared)
		}
		return nil
	})
	if err != nil {
		return domain.VideoView{}, err
	}
	return s.Get(ctx, m, id)
}

// canShareVideos: only owner and mentors of a mentorship share with the group.
func canShareVideos(m domain.Member) bool {
	return m.WorkspaceKind == domain.WorkspaceMentorship && m.Role.Manages()
}

// Download returns a short-lived URL to download the original file of an
// uploaded video (the member's own or one shared with the group).
func (s *MediaService) Download(ctx context.Context, m domain.Member, id uuid.UUID) (string, error) {
	v, err := s.video(ctx, m.Actor(), id)
	if err != nil {
		return "", err
	}
	if v.Kind != domain.VideoUpload {
		return "", domain.ErrEmbedNotDownloadable
	}
	if v.Status == domain.VideoUploading || v.Status == domain.VideoProcessing {
		return "", domain.ErrVideoNotReady
	}
	if s.objects == nil {
		return "", domain.ErrUploadsUnavailable
	}
	name := "video.mp4"
	if v.FileName != nil && *v.FileName != "" {
		name = *v.FileName
	}
	return s.objects.SignGet(ctx, v.OriginalKey(), videoDownloadTTL, name)
}

// LinkProduct links a video of the member to a catalog product.
func (s *MediaService) LinkProduct(ctx context.Context, m domain.Member, id, productID uuid.UUID) (domain.VideoView, error) {
	if _, err := s.own(ctx, m.Actor(), id); err != nil {
		return domain.VideoView{}, err
	}
	if err := s.requireProduct(ctx, productID); err != nil {
		return domain.VideoView{}, err
	}
	if err := s.repo.Link(ctx, m.Actor(), id, domain.TargetProduct, productID); err != nil {
		return domain.VideoView{}, err
	}
	return s.Get(ctx, m, id)
}

// UnlinkProduct removes the video from the product.
func (s *MediaService) UnlinkProduct(ctx context.Context, m domain.Member, id, productID uuid.UUID) (domain.VideoView, error) {
	if _, err := s.own(ctx, m.Actor(), id); err != nil {
		return domain.VideoView{}, err
	}
	if _, err := s.repo.Unlink(ctx, m.Actor(), id, domain.TargetProduct, productID); err != nil {
		return domain.VideoView{}, err
	}
	return s.Get(ctx, m, id)
}

// LinkList attaches a video of the member to a curated list and shares it
// with the group. The caller (the curation module) checks the list and the
// role of who attaches.
func (s *MediaService) LinkList(ctx context.Context, m domain.Member, id, listID uuid.UUID) error {
	a := m.Actor()
	if _, err := s.own(ctx, a, id); err != nil {
		return err
	}
	if !canShareVideos(m) {
		return domain.ErrVideoShareManagers
	}
	return s.tx.WithinTx(ctx, func(ctx context.Context) error {
		if err := s.repo.SetShared(ctx, a, id, true); err != nil {
			return err
		}
		return s.repo.Link(ctx, a, id, domain.TargetList, listID)
	})
}

// UnlinkList removes a video from a list (of any owner; the RLS only lets
// owner and mentors). The video stays shared.
func (s *MediaService) UnlinkList(ctx context.Context, m domain.Member, id, listID uuid.UUID) error {
	n, err := s.repo.Unlink(ctx, m.Actor(), id, domain.TargetList, listID)
	if err == nil && n == 0 {
		return domain.ErrVideoNotFound
	}
	return err
}

// UnlinkTarget removes every video from a deleted target (a list).
func (s *MediaService) UnlinkTarget(ctx context.Context, m domain.Member, target domain.VideoTarget, targetID uuid.UUID) error {
	return s.repo.UnlinkTarget(ctx, m.Actor(), target, targetID)
}

// Quota returns the upload space used and the limit of the plan.
func (s *MediaService) Quota(ctx context.Context, m domain.Member) (domain.VideoQuota, error) {
	limit, err := s.accounts.PlanLimit(ctx, m, domain.LimitVideoBytes)
	if err != nil {
		return domain.VideoQuota{}, err
	}
	used, err := s.repo.Usage(ctx, m.Actor())
	return domain.VideoQuota{UsedBytes: used, LimitBytes: limit}, err
}

func (s *MediaService) requireProduct(ctx context.Context, id uuid.UUID) error {
	ps, err := s.products.Products(ctx, []uuid.UUID{id})
	if err != nil {
		return err
	}
	if _, ok := ps[id]; !ok {
		return domain.ErrProductNotFound
	}
	return nil
}

// views adds the links (only on the actor's own videos), the official player
// of the embeds and the signed URLs of thumbnail and preview of the ready
// uploads.
func (s *MediaService) views(ctx context.Context, a domain.Actor, rows []domain.Video) ([]domain.VideoView, error) {
	out := make([]domain.VideoView, len(rows))
	var mine []uuid.UUID
	for _, r := range rows {
		if r.OwnerID == a.UserID {
			mine = append(mine, r.ID)
		}
	}
	var links []domain.VideoLinkRef
	if len(mine) > 0 {
		var err error
		if links, err = s.repo.LinksOfVideos(ctx, a, mine); err != nil {
			return nil, err
		}
	}
	for i, r := range rows {
		v := domain.VideoView{Video: r, Mine: r.OwnerID == a.UserID, ProductIDs: []uuid.UUID{}, ListIDs: []uuid.UUID{}}
		if r.Kind == domain.VideoEmbed && r.EmbedID != nil {
			v.PlayerURL = mediaOptional(domain.PlayerURL(r.Platform, *r.EmbedID))
		}
		if r.Kind == domain.VideoUpload && r.Status == domain.VideoReady && s.objects != nil {
			thumb, err := s.objects.SignGet(ctx, r.ThumbnailKey(), videoReadTTL, "")
			if err != nil {
				return nil, err
			}
			preview, err := s.objects.SignGet(ctx, r.PreviewKey(), videoReadTTL, "")
			if err != nil {
				return nil, err
			}
			v.ThumbnailURL, v.PreviewURL = &thumb, &preview
		}
		for _, l := range links {
			if l.VideoID != r.ID {
				continue
			}
			if l.TargetKind == domain.TargetProduct {
				v.ProductIDs = append(v.ProductIDs, l.TargetID)
			} else {
				v.ListIDs = append(v.ListIDs, l.TargetID)
			}
		}
		out[i] = v
	}
	return out, nil
}

// ownJobVideo reads the video of a job with the owner's scope. Without the
// video (deleted), ok is false.
func (s *MediaService) ownJobVideo(ctx context.Context, j domain.VideoJob) (domain.Video, bool, error) {
	v, err := s.video(ctx, j.Actor(), j.VideoID)
	if errors.Is(err, domain.ErrVideoNotFound) {
		return v, false, nil
	}
	return v, err == nil && v.OwnerID == j.OwnerID, err
}

// Process makes the preview and the thumbnail of a completed upload and
// records duration and dimensions (process_video). ffmpeg reads the original
// straight from the bucket by a signed URL. It returns domain.ErrNotAVideo for
// a file that is not a video.
func (s *MediaService) Process(ctx context.Context, j domain.VideoJob) error {
	v, ok, err := s.ownJobVideo(ctx, j)
	if err != nil || !ok || v.Status != domain.VideoProcessing {
		return err // deleted or already processed
	}
	if s.objects == nil || s.processor == nil {
		return domain.ErrUploadsUnavailable
	}
	input, err := s.objects.InternalURL(ctx, v.OriginalKey(), videoInternalTTL)
	if err != nil {
		return err
	}
	dir, err := os.MkdirTemp("", "video-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	p, err := s.processor.Process(ctx, input, dir)
	if err != nil {
		return err
	}
	if err := s.objects.UploadFile(ctx, v.PreviewKey(), p.PreviewPath, "video/mp4"); err != nil {
		return err
	}
	if err := s.objects.UploadFile(ctx, v.ThumbnailKey(), p.ThumbnailPath, "image/jpeg"); err != nil {
		return err
	}
	return s.repo.MarkProcessed(ctx, j.Actor(), j.VideoID, p)
}

// FailVideo marks the video of a job as failed (process_video gave up).
func (s *MediaService) FailVideo(ctx context.Context, j domain.VideoJob) {
	s.setStatus(ctx, j.Actor(), j.VideoID, domain.VideoFailed)
}

// RevalidateEmbed checks the embed at the oEmbed again (without cache):
// marks it unavailable when the video is gone or went private, refreshes
// title and thumbnail when it still exists, and schedules the next check
// (revalidate_embed).
func (s *MediaService) RevalidateEmbed(ctx context.Context, j domain.VideoJob) error {
	v, ok, err := s.ownJobVideo(ctx, j)
	if err != nil || !ok || v.Kind != domain.VideoEmbed || v.URL == nil {
		return err
	}
	ref, err := s.embeds.Resolve(ctx, *v.URL, false)
	r := domain.EmbedRevalidation{Status: domain.VideoReady}
	switch {
	case errors.Is(err, domain.ErrVideoUnavailable), errors.Is(err, domain.ErrInvalidVideoLink):
		r.Status = domain.VideoUnavailable
	case err != nil:
		return err // the platform did not answer: River tries again
	default:
		r.Title, r.Author, r.ThumbnailURL = &ref.Title, &ref.Author, mediaOptional(ref.ThumbnailURL)
	}
	if err := s.repo.MarkRevalidated(ctx, j.Actor(), j.VideoID, r); err != nil {
		return err
	}
	if s.queue == nil {
		return nil
	}
	return s.queue.ScheduleRevalidateEmbed(ctx, j, s.now().Add(embedRevalidateGap))
}

// CleanUpload discards the upload if it has not finished (clean_upload).
func (s *MediaService) CleanUpload(ctx context.Context, j domain.VideoJob) error {
	v, ok, err := s.ownJobVideo(ctx, j)
	if err != nil || !ok || v.Status != domain.VideoUploading {
		return err
	}
	err = s.delete(ctx, j.Actor(), j.VideoID)
	if errors.Is(err, domain.ErrVideoNotFound) {
		return nil
	}
	return err
}

// videoFileName cleans the name the browser reports: no folders nor control
// characters, up to 200 characters.
func videoFileName(name string) string {
	name = path.Base(strings.ReplaceAll(strings.TrimSpace(name), "\\", "/"))
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, name)
	if name == "." || name == "/" || name == "" {
		name = "video"
	}
	name = strings.TrimSpace(name)
	if utf8.RuneCountInString(name) > domain.MaxVideoText {
		name = string([]rune(name)[:domain.MaxVideoText])
	}
	return name
}

// titleFromFileName is the first title: the file name without extension.
func titleFromFileName(name string) string {
	t := strings.TrimSuffix(name, path.Ext(name))
	t = strings.Join(strings.Fields(strings.NewReplacer("_", " ", "-", " ").Replace(t)), " ")
	if t == "" {
		return "Vídeo"
	}
	return t
}

func mediaOptional(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
