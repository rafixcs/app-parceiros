package queue

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"github.com/rafixcs/app-parceiros/backend/internal/domain"
)

// ConversionSyncInterval is how often every connected user is synced.
const ConversionSyncInterval = 24 * time.Hour

// ConversionSyncer syncs a user's conversions (service.ResultService).
type ConversionSyncer interface {
	Sync(ctx context.Context, userID uuid.UUID, lastAttempt bool) (int, error)
}

// ConnectedUsers lists the users with Shopee connected
// (service.ShopeeCredentialService).
type ConnectedUsers interface {
	ConnectedUsers(ctx context.Context) ([]uuid.UUID, error)
}

// SyncConversionsArgs syncs the conversions of a user with their credential.
type SyncConversionsArgs struct {
	UserID uuid.UUID `json:"user_id"`
}

func (SyncConversionsArgs) Kind() string { return "sync_conversions" }

func (SyncConversionsArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue:       QueueShopee,
		MaxAttempts: 5,
		// One sync per user in the queue at a time.
		UniqueOpts: river.UniqueOpts{ByArgs: true, ByState: []rivertype.JobState{
			rivertype.JobStateAvailable, rivertype.JobStatePending, rivertype.JobStateRetryable,
			rivertype.JobStateRunning, rivertype.JobStateScheduled,
		}},
	}
}

// EnqueueConversionSync enqueues sync_conversions (domain.ConversionSyncQueue).
func (r *River) EnqueueConversionSync(ctx context.Context, userID uuid.UUID) error {
	return r.insert(ctx, river.InsertManyParams{Args: SyncConversionsArgs{UserID: userID}})
}

type SyncConversionsWorker struct {
	river.WorkerDefaults[SyncConversionsArgs]
	Svc ConversionSyncer
	Log *slog.Logger
}

// Timeout: the pages are read in sequence and respect the rate limit, so it
// may take a while.
func (w *SyncConversionsWorker) Timeout(*river.Job[SyncConversionsArgs]) time.Duration {
	return 15 * time.Minute
}

func (w *SyncConversionsWorker) Work(ctx context.Context, job *river.Job[SyncConversionsArgs]) error {
	n, err := w.Svc.Sync(ctx, job.Args.UserID, job.Attempt >= job.MaxAttempts)
	switch {
	case err == nil:
		w.Log.InfoContext(ctx, "conversions synced", "job", job.Kind, "user_id", job.Args.UserID, "conversions", n)
		return nil
	case errors.Is(err, domain.ErrSourceLimit):
		// The scrollId expires: the next attempt starts over.
		return river.JobSnooze(5 * time.Minute)
	}
	return err
}

// ScheduleConversionSyncsArgs is the periodic job that enqueues the sync of
// each user with Shopee connected.
type ScheduleConversionSyncsArgs struct{}

func (ScheduleConversionSyncsArgs) Kind() string { return "schedule_conversion_syncs" }

type ScheduleConversionSyncsWorker struct {
	river.WorkerDefaults[ScheduleConversionSyncsArgs]
	Users ConnectedUsers
	Queue *River
}

func (w *ScheduleConversionSyncsWorker) Work(ctx context.Context, _ *river.Job[ScheduleConversionSyncsArgs]) error {
	ids, err := w.Users.ConnectedUsers(ctx)
	if err != nil {
		return err
	}
	params := make([]river.InsertManyParams, len(ids))
	for i, id := range ids {
		params[i] = river.InsertManyParams{Args: SyncConversionsArgs{UserID: id}}
	}
	return w.Queue.insert(ctx, params...)
}

// PeriodicConversionSyncs schedules ScheduleConversionSyncsArgs once a day.
// It does not run when the worker starts, so a deploy does not fire
// everyone's sync; the user can ask for theirs on the dashboard.
func PeriodicConversionSyncs() *river.PeriodicJob {
	return river.NewPeriodicJob(
		river.PeriodicInterval(ConversionSyncInterval),
		func() (river.JobArgs, *river.InsertOpts) { return ScheduleConversionSyncsArgs{}, nil },
		&river.PeriodicJobOpts{ID: "schedule_conversion_syncs"},
	)
}
