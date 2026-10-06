package queue

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/riverqueue/river"

	"github.com/rafixcs/app-parceiros/backend/internal/domain"
)

// MediaJobs runs the media jobs (service.MediaService).
type MediaJobs interface {
	Process(ctx context.Context, j domain.VideoJob) error
	FailVideo(ctx context.Context, j domain.VideoJob)
	RevalidateEmbed(ctx context.Context, j domain.VideoJob) error
	CleanUpload(ctx context.Context, j domain.VideoJob) error
}

// VideoJobArgs identify the video of a media job: the owner's library in the
// workspace.
type VideoJobArgs struct {
	VideoID     uuid.UUID `json:"video_id"`
	WorkspaceID uuid.UUID `json:"workspace_id"`
	OwnerID     uuid.UUID `json:"owner_id"`
}

func (a VideoJobArgs) job() domain.VideoJob {
	return domain.VideoJob{VideoID: a.VideoID, WorkspaceID: a.WorkspaceID, OwnerID: a.OwnerID}
}

func videoJobArgs(j domain.VideoJob) VideoJobArgs {
	return VideoJobArgs{VideoID: j.VideoID, WorkspaceID: j.WorkspaceID, OwnerID: j.OwnerID}
}

// ProcessVideoArgs makes the 720p preview and the thumbnail of a completed
// upload and reads its duration and dimensions.
type ProcessVideoArgs struct{ VideoJobArgs }

func (ProcessVideoArgs) Kind() string { return "process_video" }

func (ProcessVideoArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue: QueueMedia, MaxAttempts: 3,
		UniqueOpts: river.UniqueOpts{ByArgs: true},
	}
}

// RevalidateEmbedArgs checks at the oEmbed whether a reference video still
// exists. Each embed is checked every 7 days: the job schedules its next run.
type RevalidateEmbedArgs struct{ VideoJobArgs }

func (RevalidateEmbedArgs) Kind() string { return "revalidate_embed" }

func (RevalidateEmbedArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		// No UniqueOpts: River requires the running state in them, and the job
		// schedules its own next run. Each video schedules it once, when created.
		Queue: QueueDefault, MaxAttempts: 5,
	}
}

// CleanUploadArgs discards an upload that did not finish in time (24 h): the
// parts in the bucket, the video and the reservation in the quota.
type CleanUploadArgs struct{ VideoJobArgs }

func (CleanUploadArgs) Kind() string { return "clean_upload" }

func (CleanUploadArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: QueueMedia, MaxAttempts: 10}
}

// EnqueueProcessVideo implements domain.MediaQueue.
func (r *River) EnqueueProcessVideo(ctx context.Context, j domain.VideoJob) error {
	return r.insert(ctx, river.InsertManyParams{Args: ProcessVideoArgs{videoJobArgs(j)}})
}

// ScheduleRevalidateEmbed implements domain.MediaQueue.
func (r *River) ScheduleRevalidateEmbed(ctx context.Context, j domain.VideoJob, at time.Time) error {
	return r.insert(ctx, river.InsertManyParams{
		Args: RevalidateEmbedArgs{videoJobArgs(j)}, InsertOpts: &river.InsertOpts{ScheduledAt: at},
	})
}

// ScheduleCleanUpload implements domain.MediaQueue.
func (r *River) ScheduleCleanUpload(ctx context.Context, j domain.VideoJob, at time.Time) error {
	return r.insert(ctx, river.InsertManyParams{
		Args: CleanUploadArgs{videoJobArgs(j)}, InsertOpts: &river.InsertOpts{ScheduledAt: at},
	})
}

var _ domain.MediaQueue = (*River)(nil)

type ProcessVideoWorker struct {
	river.WorkerDefaults[ProcessVideoArgs]
	Svc MediaJobs
	Log *slog.Logger
}

func (w *ProcessVideoWorker) Timeout(*river.Job[ProcessVideoArgs]) time.Duration {
	return 30 * time.Minute
}

func (w *ProcessVideoWorker) Work(ctx context.Context, job *river.Job[ProcessVideoArgs]) error {
	j := job.Args.job()
	log := w.Log.With("job", job.Kind, "video_id", j.VideoID)
	err := w.Svc.Process(ctx, j)
	switch {
	case errors.Is(err, domain.ErrNotAVideo):
		log.Info("uploaded file is not a readable video", "err", err)
		w.Svc.FailVideo(ctx, j)
		return river.JobCancel(err)
	case err != nil && job.Attempt >= job.MaxAttempts:
		log.Error("could not process the video; giving up", "err", err)
		w.Svc.FailVideo(ctx, j)
	}
	return err
}

type RevalidateEmbedWorker struct {
	river.WorkerDefaults[RevalidateEmbedArgs]
	Svc MediaJobs
}

func (w *RevalidateEmbedWorker) Work(ctx context.Context, job *river.Job[RevalidateEmbedArgs]) error {
	return w.Svc.RevalidateEmbed(ctx, job.Args.job())
}

type CleanUploadWorker struct {
	river.WorkerDefaults[CleanUploadArgs]
	Svc MediaJobs
}

func (w *CleanUploadWorker) Work(ctx context.Context, job *river.Job[CleanUploadArgs]) error {
	return w.Svc.CleanUpload(ctx, job.Args.job())
}
