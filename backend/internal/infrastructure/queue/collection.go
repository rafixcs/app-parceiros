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

// AffiliateLinkGenerator generates the links of a saved item
// (service.CollectionService).
type AffiliateLinkGenerator interface {
	GenerateAffiliateLinks(ctx context.Context, a domain.Actor, itemID uuid.UUID, lastAttempt bool) error
}

// GenerateAffiliateLinkArgs generates the affiliate links of an item, one per
// channel, with the credential of the item's owner.
type GenerateAffiliateLinkArgs struct {
	ItemID      uuid.UUID `json:"item_id"`
	WorkspaceID uuid.UUID `json:"workspace_id"`
	UserID      uuid.UUID `json:"user_id"`
}

func (GenerateAffiliateLinkArgs) Kind() string { return "generate_affiliate_link" }

func (GenerateAffiliateLinkArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue:       QueueShopee,
		MaxAttempts: 6,
		// One job per item in the queue at a time. Once it completes, asking
		// again generates another.
		UniqueOpts: river.UniqueOpts{ByArgs: true, ByState: []rivertype.JobState{
			rivertype.JobStateAvailable, rivertype.JobStatePending, rivertype.JobStateRetryable,
			rivertype.JobStateRunning, rivertype.JobStateScheduled,
		}},
	}
}

// EnqueueAffiliateLinks enqueues generate_affiliate_link
// (domain.AffiliateLinkQueue).
func (r *River) EnqueueAffiliateLinks(ctx context.Context, jobs ...domain.AffiliateLinkJob) error {
	params := make([]river.InsertManyParams, len(jobs))
	for i, j := range jobs {
		params[i] = river.InsertManyParams{Args: GenerateAffiliateLinkArgs{
			ItemID: j.ItemID, WorkspaceID: j.WorkspaceID, UserID: j.UserID,
		}}
	}
	return r.insert(ctx, params...)
}

type GenerateAffiliateLinkWorker struct {
	river.WorkerDefaults[GenerateAffiliateLinkArgs]
	Svc AffiliateLinkGenerator
	Log *slog.Logger
}

func (w *GenerateAffiliateLinkWorker) Work(ctx context.Context, job *river.Job[GenerateAffiliateLinkArgs]) error {
	a := domain.Actor{UserID: job.Args.UserID, WorkspaceID: job.Args.WorkspaceID}
	err := w.Svc.GenerateAffiliateLinks(ctx, a, job.Args.ItemID, job.Attempt >= job.MaxAttempts)
	switch {
	case errors.Is(err, domain.ErrSourceLimit):
		return river.JobSnooze(time.Minute)
	case errors.Is(err, domain.ErrProductNotFound):
		w.Log.Error("the product of the item left the catalog", "job", job.Kind, "item_id", job.Args.ItemID, "err", err)
		return river.JobCancel(err)
	}
	return err
}
