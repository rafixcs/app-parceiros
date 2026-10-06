package repository

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/rafixcs/app-parceiros/backend/internal/domain"
	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/database/dbgen"
)

// PostgresNotification is the domain.NotificationRepository on Postgres.
// The inbox runs with the scope of the user in the workspace (the worker
// writes with the recipient's scope); subscriptions and preferences with the
// user's scope.
type PostgresNotification struct {
	pool *pgxpool.Pool
}

var _ domain.NotificationRepository = (*PostgresNotification)(nil)

func NewPostgresNotification(pool *pgxpool.Pool) *PostgresNotification {
	return &PostgresNotification{pool: pool}
}

func (r *PostgresNotification) SaveNotification(ctx context.Context, d domain.NotificationDelivery) (domain.Notification, error) {
	var n dbgen.Notification
	err := run(ctx, r.pool, scopeOf(d.Recipient()), func(q *dbgen.Queries, _ pgx.Tx) error {
		var err error
		n, err = q.CreateNotification(ctx, dbgen.CreateNotificationParams{
			WorkspaceID: d.WorkspaceID, UserID: d.UserID, Kind: d.Kind, Key: d.Key,
			Title: d.Title, Body: d.Body, URL: d.URL,
		})
		return err
	})
	return notificationOf(n), err
}

func (r *PostgresNotification) Inbox(ctx context.Context, a domain.Actor, limit int32) ([]domain.Notification, error) {
	var out []domain.Notification
	err := run(ctx, r.pool, scopeOf(a), func(q *dbgen.Queries, _ pgx.Tx) error {
		rows, err := q.NotificationInbox(ctx, dbgen.NotificationInboxParams{
			WorkspaceID: a.WorkspaceID, UserID: a.UserID, MaxRows: limit,
		})
		for _, row := range rows {
			out = append(out, notificationOf(row))
		}
		return err
	})
	return out, err
}

func (r *PostgresNotification) CountUnread(ctx context.Context, a domain.Actor) (int64, error) {
	var n int64
	err := run(ctx, r.pool, scopeOf(a), func(q *dbgen.Queries, _ pgx.Tx) error {
		var err error
		n, err = q.CountUnreadNotifications(ctx, dbgen.CountUnreadNotificationsParams{WorkspaceID: a.WorkspaceID, UserID: a.UserID})
		return err
	})
	return n, err
}

func (r *PostgresNotification) MarkRead(ctx context.Context, a domain.Actor, id uuid.UUID) error {
	return run(ctx, r.pool, scopeOf(a), func(q *dbgen.Queries, _ pgx.Tx) error {
		n, err := q.MarkNotificationRead(ctx, dbgen.MarkNotificationReadParams{ID: id, WorkspaceID: a.WorkspaceID, UserID: a.UserID})
		if err == nil && n == 0 {
			return domain.ErrNotFound
		}
		return err
	})
}

func (r *PostgresNotification) MarkAllRead(ctx context.Context, a domain.Actor) error {
	return run(ctx, r.pool, scopeOf(a), func(q *dbgen.Queries, _ pgx.Tx) error {
		return q.MarkAllNotificationsRead(ctx, dbgen.MarkAllNotificationsReadParams{WorkspaceID: a.WorkspaceID, UserID: a.UserID})
	})
}

func (r *PostgresNotification) MarkEmailed(ctx context.Context, a domain.Actor, id uuid.UUID) error {
	return run(ctx, r.pool, scopeOf(a), func(q *dbgen.Queries, _ pgx.Tx) error {
		return q.MarkNotificationEmailed(ctx, dbgen.MarkNotificationEmailedParams{ID: id, WorkspaceID: a.WorkspaceID, UserID: a.UserID})
	})
}

func (r *PostgresNotification) MarkPushed(ctx context.Context, a domain.Actor, id uuid.UUID) error {
	return run(ctx, r.pool, scopeOf(a), func(q *dbgen.Queries, _ pgx.Tx) error {
		return q.MarkNotificationPushed(ctx, dbgen.MarkNotificationPushedParams{ID: id, WorkspaceID: a.WorkspaceID, UserID: a.UserID})
	})
}

func (r *PostgresNotification) EmailEnabled(ctx context.Context, userID uuid.UUID) (bool, error) {
	enabled := true
	err := run(ctx, r.pool, userScope(userID), func(q *dbgen.Queries, _ pgx.Tx) error {
		p, err := q.NotificationPreferencesByUser(ctx, userID)
		switch {
		case err == nil:
			enabled = p.Email
		case !errors.Is(err, pgx.ErrNoRows):
			return err
		}
		return nil
	})
	return enabled, err
}

func (r *PostgresNotification) SetEmailEnabled(ctx context.Context, userID uuid.UUID, enabled bool) error {
	return run(ctx, r.pool, userScope(userID), func(q *dbgen.Queries, _ pgx.Tx) error {
		return q.SaveNotificationPreferences(ctx, dbgen.SaveNotificationPreferencesParams{UserID: userID, Email: enabled})
	})
}

func (r *PostgresNotification) CountPushSubscriptions(ctx context.Context, userID uuid.UUID) (int64, error) {
	var n int64
	err := run(ctx, r.pool, userScope(userID), func(q *dbgen.Queries, _ pgx.Tx) error {
		var err error
		n, err = q.CountPushSubscriptions(ctx, userID)
		return err
	})
	return n, err
}

func (r *PostgresNotification) PushSubscriptions(ctx context.Context, userID uuid.UUID) ([]domain.PushSubscription, error) {
	var out []domain.PushSubscription
	err := run(ctx, r.pool, userScope(userID), func(q *dbgen.Queries, _ pgx.Tx) error {
		rows, err := q.PushSubscriptionsByUser(ctx, userID)
		for _, s := range rows {
			out = append(out, domain.PushSubscription{Endpoint: s.Endpoint, P256dh: s.P256dh, Auth: s.Auth})
		}
		return err
	})
	return out, err
}

func (r *PostgresNotification) SavePushSubscription(ctx context.Context, userID uuid.UUID, s domain.PushSubscription) error {
	return run(ctx, r.pool, userScope(userID), func(q *dbgen.Queries, _ pgx.Tx) error {
		return q.SavePushSubscription(ctx, dbgen.SavePushSubscriptionParams{
			UserID: userID, Endpoint: s.Endpoint, P256dh: s.P256dh, Auth: s.Auth,
		})
	})
}

func (r *PostgresNotification) DeletePushSubscription(ctx context.Context, userID uuid.UUID, endpoint string) (bool, error) {
	var n int64
	err := run(ctx, r.pool, userScope(userID), func(q *dbgen.Queries, _ pgx.Tx) error {
		var err error
		n, err = q.DeletePushSubscription(ctx, dbgen.DeletePushSubscriptionParams{UserID: userID, Endpoint: endpoint})
		return err
	})
	return n > 0, err
}

func notificationOf(n dbgen.Notification) domain.Notification {
	return domain.Notification{
		ID: n.ID, WorkspaceID: n.WorkspaceID, UserID: n.UserID, Kind: n.Kind, Key: n.Key,
		Title: n.Title, Body: n.Body, URL: n.URL, CreatedAt: n.CreatedAt,
		ReadAt: n.ReadAt, EmailedAt: n.EmailedAt, PushedAt: n.PushedAt,
	}
}
