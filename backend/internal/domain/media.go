package domain

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

// VideoKind tells a reference to another creator's video (embed: only the
// metadata of the official oEmbed, the video plays in the platform's player)
// from the user's own video (upload: sent straight to the bucket).
type VideoKind string

const (
	VideoEmbed  VideoKind = "embed"
	VideoUpload VideoKind = "upload"
)

// VideoStatus of a video.
type VideoStatus string

const (
	// VideoUploading: the multipart upload is open.
	VideoUploading VideoStatus = "uploading"
	// VideoProcessing: queued for process_video.
	VideoProcessing VideoStatus = "processing"
	VideoReady      VideoStatus = "ready"
	// VideoFailed: the uploaded file could not be processed.
	VideoFailed VideoStatus = "failed"
	// VideoUnavailable: the embed is gone from the platform (or went private).
	VideoUnavailable VideoStatus = "unavailable"
)

// VideoTarget is what a video links to.
type VideoTarget string

const (
	TargetProduct VideoTarget = "product"
	TargetList    VideoTarget = "list"
)

// Video platforms.
const (
	PlatformYouTube = "youtube"
	PlatformTikTok  = "tiktok"
	PlatformUpload  = "upload"
)

const (
	// MaxVideoBytes is the largest video a user can upload (1 GB).
	MaxVideoBytes = 1 << 30
	// MaxVideoParts follows the S3 limit for multipart uploads.
	MaxVideoParts = 10000
	// MaxVideoText bounds title, author and file name.
	MaxVideoText = 200
)

// Video is a video of the library as stored. It belongs to its owner inside
// the workspace; owner and mentors can share theirs with the group.
type Video struct {
	ID          uuid.UUID
	WorkspaceID uuid.UUID
	OwnerID     uuid.UUID
	Kind        VideoKind
	Platform    string
	Status      VideoStatus
	Title       string
	Author      string
	Shared      bool
	// Embed: the page of the video at the platform, its id there and the
	// thumbnail the platform reports.
	URL          *string
	EmbedID      *string
	ThumbnailURL *string
	// Upload: the prefix of its objects in the bucket, the open multipart
	// upload and the file data.
	StorageKey  *string
	UploadID    *string
	FileName    *string
	ContentType *string
	SizeBytes   int64
	DurationS   *int32
	Width       *int32
	Height      *int32
	CreatedAt   time.Time
}

// OriginalKey, ThumbnailKey and PreviewKey are the objects of an upload.
func (v Video) OriginalKey() string  { return v.objectKey("original") }
func (v Video) ThumbnailKey() string { return v.objectKey("thumb.jpg") }
func (v Video) PreviewKey() string   { return v.objectKey("preview.mp4") }

func (v Video) objectKey(name string) string {
	if v.StorageKey == nil {
		return name
	}
	return *v.StorageKey + name
}

// VideoView is a video as a member sees it: with the official player (embed),
// signed URLs of thumbnail and preview (ready upload) and, on their own
// videos, the products and lists it links to.
type VideoView struct {
	Video
	PlayerURL  *string
	PreviewURL *string
	// Mine says whether the member is the owner.
	Mine       bool
	ProductIDs []uuid.UUID
	ListIDs    []uuid.UUID
}

// PlayerURL is the official player of the platform, for the iframe.
func PlayerURL(platform, embedID string) string {
	switch platform {
	case PlatformYouTube:
		return "https://www.youtube-nocookie.com/embed/" + embedID
	case PlatformTikTok:
		return "https://www.tiktok.com/player/v1/" + embedID
	}
	return ""
}

// StartedUpload is the answer to the start of an upload: the video
// (uploading) and the multipart upload in the bucket, which the browser fills
// part by part.
type StartedUpload struct {
	Video    VideoView
	UploadID string
	Key      string
}

// NewUpload is what the browser reports of a file to upload.
type NewUpload struct {
	FileName    string
	ContentType string
	SizeBytes   int64
	// UsageRights is the statement that the user may use the video.
	UsageRights bool
	ProductID   *uuid.UUID
}

// VideoQuota is the upload space used (or reserved by open uploads) and the
// limit of the plan.
type VideoQuota struct {
	UsedBytes  int64
	LimitBytes int64
}

// EmbedRef is what the official oEmbed says of another creator's video. Only
// metadata: the video itself is never downloaded.
type EmbedRef struct {
	Platform     string `json:"platform"`
	EmbedID      string `json:"embed_id"`
	URL          string `json:"url"`
	Title        string `json:"title"`
	Author       string `json:"author"`
	ThumbnailURL string `json:"thumbnail_url"`
}

// EmbedResolver resolves YouTube and TikTok links through their official
// oEmbed endpoints. It returns ErrInvalidVideoLink for a link it does not
// recognize, ErrVideoUnavailable when the platform says the video does not
// exist (or is private, or cannot be embedded) and ErrPlatformUnavailable
// when the platform does not answer.
type EmbedResolver interface {
	// Resolve fetches the metadata. With useCache, good answers are cached.
	Resolve(ctx context.Context, link string, useCache bool) (EmbedRef, error)
}

// ProcessedVideo is the result of processing an upload: the 720p preview, the
// thumbnail (local files) and the data of the video.
type ProcessedVideo struct {
	DurationS     int32
	Width         int32 // of the preview, already in display orientation
	Height        int32
	PreviewPath   string // MP4
	ThumbnailPath string // JPEG
}

// VideoProcessor makes preview and thumbnail of a video (ffmpeg). The input
// may be a local path or a URL.
type VideoProcessor interface {
	Process(ctx context.Context, input, dir string) (ProcessedVideo, error)
}

// ErrNotAVideo: the processor cannot read the file as a video.
var ErrNotAVideo = errors.New("the file is not a readable video")

// UploadPart is one part of a multipart upload.
type UploadPart struct {
	Number int
	ETag   string
	Size   int64
}

// ObjectInfo describes a stored object.
type ObjectInfo struct {
	Size        int64
	ContentType string
}

// ObjectStore is the bucket of the videos (S3). Missing objects and uploads
// return an error that wraps ErrNotFound.
type ObjectStore interface {
	StartMultipart(ctx context.Context, key, contentType string) (string, error)
	SignPart(ctx context.Context, key, uploadID string, number int, ttl time.Duration) (string, error)
	Parts(ctx context.Context, key, uploadID string) ([]UploadPart, error)
	CompleteMultipart(ctx context.Context, key, uploadID string, parts []UploadPart) error
	// AbortMultipart discards an upload; one that no longer exists is not an
	// error.
	AbortMultipart(ctx context.Context, key, uploadID string) error
	Info(ctx context.Context, key string) (ObjectInfo, error)
	// SignGet is a URL for the browser; with downloadAs, it downloads the
	// object under that name.
	SignGet(ctx context.Context, key string, ttl time.Duration, downloadAs string) (string, error)
	// InternalURL is a read URL for processes inside the cluster (ffmpeg).
	InternalURL(ctx context.Context, key string, ttl time.Duration) (string, error)
	UploadFile(ctx context.Context, key, path, contentType string) error
	DeletePrefix(ctx context.Context, prefix string) error
}

// VideoJob identifies the video of a media job: the owner's library in the
// workspace.
type VideoJob struct {
	VideoID     uuid.UUID
	WorkspaceID uuid.UUID
	OwnerID     uuid.UUID
}

// Actor of the job: the owner in the workspace.
func (j VideoJob) Actor() Actor { return Actor{UserID: j.OwnerID, WorkspaceID: j.WorkspaceID} }

// MediaQueue enqueues the media jobs.
type MediaQueue interface {
	// EnqueueProcessVideo queues process_video for an upload just completed.
	EnqueueProcessVideo(ctx context.Context, j VideoJob) error
	// ScheduleRevalidateEmbed schedules revalidate_embed at `at`.
	ScheduleRevalidateEmbed(ctx context.Context, j VideoJob, at time.Time) error
	// ScheduleCleanUpload schedules clean_upload at `at`.
	ScheduleCleanUpload(ctx context.Context, j VideoJob, at time.Time) error
}

// NewEmbedVideo is a reference video to save.
type NewEmbedVideo struct {
	Platform     string
	Title        string
	Author       string
	URL          string
	EmbedID      string
	ThumbnailURL *string
}

// NewUploadVideo is an upload video to create, with its open multipart upload.
type NewUploadVideo struct {
	ID          uuid.UUID
	Title       string
	StorageKey  string
	UploadID    string
	FileName    string
	ContentType string
	SizeBytes   int64
}

// TargetVideo is a video linked to a target.
type TargetVideo struct {
	TargetID uuid.UUID
	Video    Video
}

// VideoLinkRef is a link of a video to a target.
type VideoLinkRef struct {
	VideoID    uuid.UUID
	TargetKind VideoTarget
	TargetID   uuid.UUID
}

// EmbedRevalidation is the outcome of checking an embed again: the status
// and, when it still exists, its new metadata.
type EmbedRevalidation struct {
	Status       VideoStatus
	Title        *string
	Author       *string
	ThumbnailURL *string
}

// MediaRepository stores the videos, their links and the upload usage of the
// workspace. Every call acts as `a` (the member, or the owner in the jobs):
// the user sees their videos and the shared ones. Missing records return
// ErrNotFound.
type MediaRepository interface {
	// CreateEmbed saves a reference video, or refreshes the one already pasted
	// by the user, and says whether it was created.
	CreateEmbed(ctx context.Context, a Actor, e NewEmbedVideo) (uuid.UUID, bool, error)
	CreateUpload(ctx context.Context, a Actor, u NewUploadVideo) (Video, error)
	Video(ctx context.Context, a Actor, id uuid.UUID) (Video, error)
	// LockOwnVideo reads a video of the actor, locking it until the end of the
	// transaction.
	LockOwnVideo(ctx context.Context, a Actor, id uuid.UUID) (Video, error)
	Videos(ctx context.Context, a Actor, onlyMine bool) ([]Video, error)
	VideosForTargets(ctx context.Context, a Actor, target VideoTarget, ids []uuid.UUID) ([]TargetVideo, error)
	LinksOfVideos(ctx context.Context, a Actor, videoIDs []uuid.UUID) ([]VideoLinkRef, error)
	Link(ctx context.Context, a Actor, videoID uuid.UUID, target VideoTarget, targetID uuid.UUID) error
	// Unlink removes a link and says how many were removed.
	Unlink(ctx context.Context, a Actor, videoID uuid.UUID, target VideoTarget, targetID uuid.UUID) (int64, error)
	UnlinkTarget(ctx context.Context, a Actor, target VideoTarget, targetID uuid.UUID) error
	SetShared(ctx context.Context, a Actor, id uuid.UUID, shared bool) error
	Rename(ctx context.Context, a Actor, id uuid.UUID, title string) error
	// MarkUploaded closes the upload with the real size and moves the video
	// to processing.
	MarkUploaded(ctx context.Context, a Actor, id uuid.UUID, sizeBytes int64) error
	MarkProcessed(ctx context.Context, a Actor, id uuid.UUID, p ProcessedVideo) error
	SetStatus(ctx context.Context, a Actor, id uuid.UUID, s VideoStatus) error
	MarkRevalidated(ctx context.Context, a Actor, id uuid.UUID, r EmbedRevalidation) error
	DeleteVideo(ctx context.Context, a Actor, id uuid.UUID) error
	// AddUsage adds delta (negative subtracts) to the upload bytes of the
	// workspace, never below zero, and returns the new total.
	AddUsage(ctx context.Context, a Actor, delta int64) (int64, error)
	Usage(ctx context.Context, a Actor) (int64, error)
}

// Media errors.
var (
	ErrVideoNotFound        = NewError(KindNotFound, "video_not_found")
	ErrInvalidVideoLink     = NewError(KindInvalid, "invalid_video_link")
	ErrVideoUnavailable     = NewError(KindInvalid, "video_unavailable")
	ErrPlatformUnavailable  = NewError(KindUpstream, "platform_unavailable")
	ErrUsageRightsRequired  = NewError(KindInvalid, "usage_rights_required")
	ErrInvalidVideoFormat   = NewError(KindInvalid, "invalid_video_format")
	ErrInvalidVideoSize     = NewError(KindInvalid, "invalid_video_size")
	ErrVideoTooLarge        = NewError(KindInvalid, "video_too_large")
	ErrVideoQuotaExceeded   = NewError(KindConflict, "video_quota_exceeded")
	ErrUploadsUnavailable   = NewError(KindUnavailable, "uploads_unavailable")
	ErrUploadClosed         = NewError(KindConflict, "upload_closed")
	ErrUploadIncomplete     = NewError(KindInvalid, "upload_incomplete")
	ErrUploadPartsRequired  = NewError(KindInvalid, "upload_parts_required")
	ErrInvalidPartNumber    = NewError(KindInvalid, "invalid_part_number")
	ErrVideoNotReady        = NewError(KindConflict, "video_not_ready")
	ErrVideoOwnerOnly       = NewError(KindForbidden, "video_owner_only")
	ErrVideoShareManagers   = NewError(KindForbidden, "video_share_managers_only")
	ErrVideoTitleTooLong    = NewError(KindInvalid, "video_title_too_long")
	ErrEmbedNotDownloadable = NewError(KindInvalid, "embed_not_downloadable")
)
