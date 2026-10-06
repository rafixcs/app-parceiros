package queue

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/riverqueue/river"

	"github.com/rafixcs/app-parceiros/backend/internal/domain"
)

// CatalogSnapshotter collects the catalog (service.ProductService).
type CatalogSnapshotter interface {
	Snapshot(ctx context.Context, categoryID int64, pages int) (int, error)
	MonitoredCategories(ctx context.Context, source domain.Source) ([]int64, error)
}

// SnapshotCatalogArgs collects a category (0 = all) of the catalog, page by
// page, up to Pages pages.
type SnapshotCatalogArgs struct {
	CategoryID int64 `json:"category_id"`
	Pages      int   `json:"pages"`
}

func (SnapshotCatalogArgs) Kind() string { return "snapshot_catalog" }

func (SnapshotCatalogArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue:       QueueShopee,
		MaxAttempts: 5,
		// One collection per category per hour, even if the scheduler repeats.
		UniqueOpts: river.UniqueOpts{ByArgs: true, ByPeriod: time.Hour},
	}
}

type SnapshotCatalogWorker struct {
	river.WorkerDefaults[SnapshotCatalogArgs]
	Svc CatalogSnapshotter
	Log *slog.Logger
}

// Timeout is long: the pagination is sequential and respects the rate limit.
func (w *SnapshotCatalogWorker) Timeout(*river.Job[SnapshotCatalogArgs]) time.Duration {
	return 20 * time.Minute
}

func (w *SnapshotCatalogWorker) Work(ctx context.Context, job *river.Job[SnapshotCatalogArgs]) error {
	log := w.Log.With("job", job.Kind, "category", job.Args.CategoryID)
	_, err := w.Svc.Snapshot(ctx, job.Args.CategoryID, job.Args.Pages)
	switch {
	case errors.Is(err, domain.ErrSourceLimit):
		// The pages already recorded stay; the collection starts over later.
		log.Warn("source rate limit; postponing the collection", "err", err)
		return river.JobSnooze(5 * time.Minute)
	case errors.Is(err, domain.ErrSourceInvalidCredential), errors.Is(err, domain.ErrSourceAccessDenied):
		log.Error("the source refused the app credential; check SHOPEE_APP_ID and SHOPEE_APP_SECRET", "err", err)
		return river.JobCancel(err)
	}
	return err
}

// ScheduleSnapshotsArgs is the periodic job that enqueues a snapshot per
// monitored category, plus a general one (all categories).
type ScheduleSnapshotsArgs struct{}

func (ScheduleSnapshotsArgs) Kind() string { return "schedule_snapshots" }

type ScheduleSnapshotsWorker struct {
	river.WorkerDefaults[ScheduleSnapshotsArgs]
	Svc    CatalogSnapshotter
	Source domain.Source
	Pages  int
	Queue  *River
}

func (w *ScheduleSnapshotsWorker) Work(ctx context.Context, _ *river.Job[ScheduleSnapshotsArgs]) error {
	cats, err := w.Svc.MonitoredCategories(ctx, w.Source)
	if err != nil {
		return err
	}
	params := []river.InsertManyParams{{Args: SnapshotCatalogArgs{CategoryID: 0, Pages: w.Pages}}}
	for _, c := range cats {
		params = append(params, river.InsertManyParams{Args: SnapshotCatalogArgs{CategoryID: c, Pages: w.Pages}})
	}
	return w.Queue.insert(ctx, params...)
}

// PeriodicSnapshots schedules ScheduleSnapshotsArgs every interval, and once
// when the worker starts.
func PeriodicSnapshots(interval time.Duration) *river.PeriodicJob {
	return river.NewPeriodicJob(
		river.PeriodicInterval(interval),
		func() (river.JobArgs, *river.InsertOpts) { return ScheduleSnapshotsArgs{}, nil },
		&river.PeriodicJobOpts{ID: "schedule_snapshots", RunOnStart: true},
	)
}

// TrendComputer computes the radar (service.TrendService).
type TrendComputer interface {
	Compute(ctx context.Context) (int, error)
}

// ComputeTrendsArgs recomputes the radar. It is enqueued at the end of each
// snapshot, with a small delay to join the collections that end together.
type ComputeTrendsArgs struct{}

func (ComputeTrendsArgs) Kind() string { return "compute_trends" }

func (ComputeTrendsArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue: QueueDefault,
		// One computation per 5-minute window; the next snapshot that ends
		// after it schedules another.
		UniqueOpts: river.UniqueOpts{ByPeriod: 5 * time.Minute},
	}
}

// EnqueueTrends schedules the computation of the radar 30 seconds from now
// (domain.TrendQueue).
func (r *River) EnqueueTrends(ctx context.Context) error {
	opts := ComputeTrendsArgs{}.InsertOpts()
	opts.ScheduledAt = time.Now().Add(30 * time.Second)
	return r.insert(ctx, river.InsertManyParams{Args: ComputeTrendsArgs{}, InsertOpts: &opts})
}

type ComputeTrendsWorker struct {
	river.WorkerDefaults[ComputeTrendsArgs]
	Svc TrendComputer
	Log *slog.Logger
}

func (w *ComputeTrendsWorker) Timeout(*river.Job[ComputeTrendsArgs]) time.Duration {
	return 10 * time.Minute
}

func (w *ComputeTrendsWorker) Work(ctx context.Context, job *river.Job[ComputeTrendsArgs]) error {
	n, err := w.Svc.Compute(ctx)
	if err != nil {
		return err
	}
	w.Log.Info("trends computed", "job", job.Kind, "products", n)
	return nil
}
