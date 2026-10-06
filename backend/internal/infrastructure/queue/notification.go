package queue

import (
	"context"

	"github.com/google/uuid"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"github.com/rafixcs/app-parceiros/backend/internal/domain"
)

// DeliverNotificationArgs delivers a notification to one user. It carries
// only the content and the ids: the recipient's email is read at delivery.
type DeliverNotificationArgs struct {
	WorkspaceID uuid.UUID `json:"workspace_id"`
	UserID      uuid.UUID `json:"user_id"`
	NoticeKind  string    `json:"notice_kind"`
	Key         string    `json:"key"`
	Title       string    `json:"title"`
	Body        string    `json:"body"`
	URL         string    `json:"url"`
}

func (DeliverNotificationArgs) Kind() string { return "deliver_notification" }

func (DeliverNotificationArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue:       QueueDefault,
		MaxAttempts: 8,
		UniqueOpts: river.UniqueOpts{ByArgs: true, ByState: []rivertype.JobState{
			rivertype.JobStateAvailable, rivertype.JobStatePending, rivertype.JobStateRetryable,
			rivertype.JobStateRunning, rivertype.JobStateScheduled,
		}},
	}
}

func (a DeliverNotificationArgs) delivery() domain.NotificationDelivery {
	return domain.NotificationDelivery{
		WorkspaceID: a.WorkspaceID, UserID: a.UserID, Kind: a.NoticeKind, Key: a.Key,
		Title: a.Title, Body: a.Body, URL: a.URL,
	}
}

var _ domain.NotificationQueue = (*River)(nil)

// EnqueueNotifications enqueues one deliver_notification job per delivery.
func (r *River) EnqueueNotifications(ctx context.Context, ds ...domain.NotificationDelivery) error {
	params := make([]river.InsertManyParams, len(ds))
	for i, d := range ds {
		params[i] = river.InsertManyParams{Args: DeliverNotificationArgs{
			WorkspaceID: d.WorkspaceID, UserID: d.UserID, NoticeKind: d.Kind, Key: d.Key,
			Title: d.Title, Body: d.Body, URL: d.URL,
		}}
	}
	return r.insert(ctx, params...)
}

// notificationDeliverer is what the worker needs from the notifications
// service.
type notificationDeliverer interface {
	Deliver(ctx context.Context, d domain.NotificationDelivery) error
}

// DeliverNotificationWorker records the notification in the inbox and sends
// the email and the push.
type DeliverNotificationWorker struct {
	river.WorkerDefaults[DeliverNotificationArgs]
	Service notificationDeliverer
}

func (w *DeliverNotificationWorker) Work(ctx context.Context, job *river.Job[DeliverNotificationArgs]) error {
	return w.Service.Deliver(ctx, job.Args.delivery())
}
