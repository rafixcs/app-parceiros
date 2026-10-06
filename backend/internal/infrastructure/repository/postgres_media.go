package repository

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/rafixcs/app-parceiros/backend/internal/domain"
	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/database/dbgen"
)

// PostgresMedia is the domain.MediaRepository on Postgres. Every call runs
// with the scope of the actor: the RLS shows them their videos and the shared
// ones in the workspace.
type PostgresMedia struct {
	pool *pgxpool.Pool
}

var _ domain.MediaRepository = (*PostgresMedia)(nil)

func NewPostgresMedia(pool *pgxpool.Pool) *PostgresMedia { return &PostgresMedia{pool: pool} }

func (r *PostgresMedia) run(ctx context.Context, a domain.Actor, fn func(*dbgen.Queries) error) error {
	return run(ctx, r.pool, scopeOf(a), func(q *dbgen.Queries, _ pgx.Tx) error { return fn(q) })
}

func (r *PostgresMedia) CreateEmbed(ctx context.Context, a domain.Actor, e domain.NewEmbedVideo) (uuid.UUID, bool, error) {
	var row dbgen.CreateEmbedVideoRow
	err := r.run(ctx, a, func(q *dbgen.Queries) error {
		var err error
		row, err = q.CreateEmbedVideo(ctx, dbgen.CreateEmbedVideoParams{
			WorkspaceID: a.WorkspaceID, OwnerID: a.UserID, Platform: e.Platform, Title: e.Title, Author: e.Author,
			URL: &e.URL, EmbedID: &e.EmbedID, ThumbnailURL: e.ThumbnailURL,
		})
		return err
	})
	return row.ID, row.Created, err
}

func (r *PostgresMedia) CreateUpload(ctx context.Context, a domain.Actor, u domain.NewUploadVideo) (domain.Video, error) {
	var v dbgen.Video
	err := r.run(ctx, a, func(q *dbgen.Queries) error {
		var err error
		v, err = q.CreateUploadVideo(ctx, dbgen.CreateUploadVideoParams{
			ID: u.ID, WorkspaceID: a.WorkspaceID, OwnerID: a.UserID, Title: u.Title, StorageKey: &u.StorageKey,
			UploadID: &u.UploadID, FileName: &u.FileName, ContentType: &u.ContentType, SizeBytes: u.SizeBytes,
		})
		return err
	})
	return videoOf(v), err
}

func (r *PostgresMedia) Video(ctx context.Context, a domain.Actor, id uuid.UUID) (domain.Video, error) {
	var v dbgen.Video
	err := r.run(ctx, a, func(q *dbgen.Queries) error {
		var err error
		v, err = q.VideoByID(ctx, dbgen.VideoByIDParams{ID: id, WorkspaceID: a.WorkspaceID})
		return err
	})
	return videoOf(v), notFound(err)
}

func (r *PostgresMedia) LockOwnVideo(ctx context.Context, a domain.Actor, id uuid.UUID) (domain.Video, error) {
	var v dbgen.Video
	err := r.run(ctx, a, func(q *dbgen.Queries) error {
		var err error
		v, err = q.LockOwnVideo(ctx, dbgen.LockOwnVideoParams{ID: id, WorkspaceID: a.WorkspaceID, OwnerID: a.UserID})
		return err
	})
	return videoOf(v), notFound(err)
}

func (r *PostgresMedia) Videos(ctx context.Context, a domain.Actor, onlyMine bool) ([]domain.Video, error) {
	var out []domain.Video
	err := r.run(ctx, a, func(q *dbgen.Queries) error {
		rows, err := q.WorkspaceVideos(ctx, dbgen.WorkspaceVideosParams{WorkspaceID: a.WorkspaceID, OwnerID: a.UserID, OnlyMine: onlyMine})
		out = make([]domain.Video, len(rows))
		for i, v := range rows {
			out[i] = videoOf(v)
		}
		return err
	})
	return out, err
}

func (r *PostgresMedia) VideosForTargets(ctx context.Context, a domain.Actor, target domain.VideoTarget, ids []uuid.UUID) ([]domain.TargetVideo, error) {
	var out []domain.TargetVideo
	err := r.run(ctx, a, func(q *dbgen.Queries) error {
		rows, err := q.VideosForTargets(ctx, dbgen.VideosForTargetsParams{
			WorkspaceID: a.WorkspaceID, TargetKind: dbgen.VideoTarget(target), TargetIds: ids,
		})
		out = make([]domain.TargetVideo, len(rows))
		for i, row := range rows {
			out[i] = domain.TargetVideo{TargetID: row.TargetID, Video: videoOf(row.Video)}
		}
		return err
	})
	return out, err
}

func (r *PostgresMedia) LinksOfVideos(ctx context.Context, a domain.Actor, videoIDs []uuid.UUID) ([]domain.VideoLinkRef, error) {
	var out []domain.VideoLinkRef
	err := r.run(ctx, a, func(q *dbgen.Queries) error {
		rows, err := q.LinksOfVideos(ctx, dbgen.LinksOfVideosParams{WorkspaceID: a.WorkspaceID, VideoIds: videoIDs})
		out = make([]domain.VideoLinkRef, len(rows))
		for i, row := range rows {
			out[i] = domain.VideoLinkRef{VideoID: row.VideoID, TargetKind: domain.VideoTarget(row.TargetKind), TargetID: row.TargetID}
		}
		return err
	})
	return out, err
}

func (r *PostgresMedia) Link(ctx context.Context, a domain.Actor, videoID uuid.UUID, target domain.VideoTarget, targetID uuid.UUID) error {
	return r.run(ctx, a, func(q *dbgen.Queries) error {
		_, err := q.LinkVideo(ctx, dbgen.LinkVideoParams{
			VideoID: videoID, WorkspaceID: a.WorkspaceID, OwnerID: a.UserID, TargetKind: dbgen.VideoTarget(target), TargetID: targetID,
		})
		return err
	})
}

func (r *PostgresMedia) Unlink(ctx context.Context, a domain.Actor, videoID uuid.UUID, target domain.VideoTarget, targetID uuid.UUID) (int64, error) {
	var n int64
	err := r.run(ctx, a, func(q *dbgen.Queries) error {
		var err error
		n, err = q.UnlinkVideo(ctx, dbgen.UnlinkVideoParams{
			WorkspaceID: a.WorkspaceID, VideoID: videoID, TargetKind: dbgen.VideoTarget(target), TargetID: targetID,
		})
		return err
	})
	return n, err
}

func (r *PostgresMedia) UnlinkTarget(ctx context.Context, a domain.Actor, target domain.VideoTarget, targetID uuid.UUID) error {
	return r.run(ctx, a, func(q *dbgen.Queries) error {
		return q.UnlinkVideoTarget(ctx, dbgen.UnlinkVideoTargetParams{
			WorkspaceID: a.WorkspaceID, TargetKind: dbgen.VideoTarget(target), TargetID: targetID,
		})
	})
}

func (r *PostgresMedia) SetShared(ctx context.Context, a domain.Actor, id uuid.UUID, shared bool) error {
	return r.run(ctx, a, func(q *dbgen.Queries) error {
		_, err := q.ShareVideo(ctx, dbgen.ShareVideoParams{ID: id, WorkspaceID: a.WorkspaceID, OwnerID: a.UserID, Shared: shared})
		return err
	})
}

func (r *PostgresMedia) Rename(ctx context.Context, a domain.Actor, id uuid.UUID, title string) error {
	return r.run(ctx, a, func(q *dbgen.Queries) error {
		_, err := q.RenameVideo(ctx, dbgen.RenameVideoParams{ID: id, WorkspaceID: a.WorkspaceID, OwnerID: a.UserID, Title: title})
		return err
	})
}

func (r *PostgresMedia) MarkUploaded(ctx context.Context, a domain.Actor, id uuid.UUID, sizeBytes int64) error {
	return r.run(ctx, a, func(q *dbgen.Queries) error {
		return q.MarkVideoUploaded(ctx, dbgen.MarkVideoUploadedParams{ID: id, WorkspaceID: a.WorkspaceID, OwnerID: a.UserID, SizeBytes: sizeBytes})
	})
}

func (r *PostgresMedia) MarkProcessed(ctx context.Context, a domain.Actor, id uuid.UUID, p domain.ProcessedVideo) error {
	return r.run(ctx, a, func(q *dbgen.Queries) error {
		return q.MarkVideoProcessed(ctx, dbgen.MarkVideoProcessedParams{
			ID: id, WorkspaceID: a.WorkspaceID, OwnerID: a.UserID, DurationS: &p.DurationS, Width: &p.Width, Height: &p.Height,
		})
	})
}

func (r *PostgresMedia) SetStatus(ctx context.Context, a domain.Actor, id uuid.UUID, s domain.VideoStatus) error {
	return r.run(ctx, a, func(q *dbgen.Queries) error {
		return q.SetVideoStatus(ctx, dbgen.SetVideoStatusParams{ID: id, WorkspaceID: a.WorkspaceID, OwnerID: a.UserID, Status: dbgen.VideoStatus(s)})
	})
}

func (r *PostgresMedia) MarkRevalidated(ctx context.Context, a domain.Actor, id uuid.UUID, rv domain.EmbedRevalidation) error {
	return r.run(ctx, a, func(q *dbgen.Queries) error {
		return q.MarkEmbedRevalidated(ctx, dbgen.MarkEmbedRevalidatedParams{
			ID: id, WorkspaceID: a.WorkspaceID, OwnerID: a.UserID, Status: dbgen.VideoStatus(rv.Status),
			Title: rv.Title, Author: rv.Author, ThumbnailURL: rv.ThumbnailURL,
		})
	})
}

func (r *PostgresMedia) DeleteVideo(ctx context.Context, a domain.Actor, id uuid.UUID) error {
	return r.run(ctx, a, func(q *dbgen.Queries) error {
		n, err := q.DeleteVideo(ctx, dbgen.DeleteVideoParams{ID: id, WorkspaceID: a.WorkspaceID, OwnerID: a.UserID})
		if err == nil && n == 0 {
			return domain.ErrNotFound
		}
		return err
	})
}

func (r *PostgresMedia) AddUsage(ctx context.Context, a domain.Actor, delta int64) (int64, error) {
	var n int64
	err := r.run(ctx, a, func(q *dbgen.Queries) error {
		var err error
		n, err = q.AddVideoUsage(ctx, dbgen.AddVideoUsageParams{WorkspaceID: a.WorkspaceID, Delta: delta})
		return err
	})
	return n, err
}

func (r *PostgresMedia) Usage(ctx context.Context, a domain.Actor) (int64, error) {
	var n int64
	err := r.run(ctx, a, func(q *dbgen.Queries) error {
		var err error
		n, err = q.VideoUsage(ctx, a.WorkspaceID)
		return err
	})
	return n, err
}

func videoOf(v dbgen.Video) domain.Video {
	return domain.Video{
		ID: v.ID, WorkspaceID: v.WorkspaceID, OwnerID: v.OwnerID, Kind: domain.VideoKind(v.Kind), Platform: v.Platform,
		Status: domain.VideoStatus(v.Status), Title: v.Title, Author: v.Author, Shared: v.Shared, URL: v.URL,
		EmbedID: v.EmbedID, ThumbnailURL: v.ThumbnailURL, StorageKey: v.StorageKey, UploadID: v.UploadID,
		FileName: v.FileName, ContentType: v.ContentType, SizeBytes: v.SizeBytes, DurationS: v.DurationS,
		Width: v.Width, Height: v.Height, CreatedAt: v.CreatedAt,
	}
}
