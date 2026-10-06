package repository

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/rafixcs/app-parceiros/backend/internal/domain"
	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/database"
	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/database/dbgen"
)

// PostgresBilling is the domain.BillingRepository on Postgres.
type PostgresBilling struct {
	pool *pgxpool.Pool
}

var _ domain.BillingRepository = (*PostgresBilling)(nil)

func NewPostgresBilling(pool *pgxpool.Pool) *PostgresBilling { return &PostgresBilling{pool: pool} }

// subscriptionScope is the scope of the webhook: the workspace of the
// subscription, without a user, and its external id.
func subscriptionScope(s domain.Subscription) database.Scope {
	return database.Scope{WorkspaceID: idString(s.WorkspaceID), ExternalSubscriptionID: s.ExternalID}
}

func (r *PostgresBilling) Subscription(ctx context.Context, a domain.Actor) (domain.Subscription, error) {
	var s dbgen.Subscription
	err := run(ctx, r.pool, scopeOf(a), func(q *dbgen.Queries, _ pgx.Tx) error {
		var err error
		s, err = q.SubscriptionByWorkspace(ctx, a.WorkspaceID)
		return err
	})
	return subscriptionOf(s), notFound(err)
}

func (r *PostgresBilling) SubscriptionByExternalID(ctx context.Context, provider, externalID string) (domain.Subscription, error) {
	var s dbgen.Subscription
	err := run(ctx, r.pool, database.Scope{ExternalSubscriptionID: externalID}, func(q *dbgen.Queries, _ pgx.Tx) error {
		var err error
		s, err = q.SubscriptionByExternalID(ctx, dbgen.SubscriptionByExternalIDParams{Provider: provider, ExternalID: externalID})
		return err
	})
	return subscriptionOf(s), notFound(err)
}

func (r *PostgresBilling) SaveSubscription(ctx context.Context, a domain.Actor, n domain.NewSubscription) (bool, error) {
	err := run(ctx, r.pool, scopeOf(a), func(q *dbgen.Queries, _ pgx.Tx) error {
		_, err := q.SaveSubscription(ctx, dbgen.SaveSubscriptionParams{
			WorkspaceID:        a.WorkspaceID,
			Provider:           n.Provider,
			ExternalCustomerID: n.ExternalCustomerID,
			ExternalID:         n.ExternalID,
			Seats:              n.Seats,
			AmountCents:        n.AmountCents,
			NextDueDate:        pgtype.Date{Time: n.NextDueDate, Valid: true},
			PaymentURL:         n.PaymentURL,
			CreatedBy:          n.CreatedBy,
		})
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func (r *PostgresBilling) UpdateSubscriptionPlan(ctx context.Context, a domain.Actor, seats int32, amountCents int64) error {
	return run(ctx, r.pool, scopeOf(a), func(q *dbgen.Queries, _ pgx.Tx) error {
		_, err := q.UpdateSubscriptionPlan(ctx, dbgen.UpdateSubscriptionPlanParams{
			WorkspaceID: a.WorkspaceID, Seats: seats, AmountCents: amountCents,
		})
		return notFound(err)
	})
}

func (r *PostgresBilling) CancelSubscription(ctx context.Context, a domain.Actor) error {
	return run(ctx, r.pool, scopeOf(a), func(q *dbgen.Queries, _ pgx.Tx) error {
		_, err := q.CancelSubscription(ctx, a.WorkspaceID)
		return notFound(err)
	})
}

func (r *PostgresBilling) UpdateBilling(ctx context.Context, s domain.Subscription, u domain.BillingUpdate) error {
	return run(ctx, r.pool, subscriptionScope(s), func(q *dbgen.Queries, _ pgx.Tx) error {
		p := dbgen.UpdateSubscriptionBillingParams{
			WorkspaceID: s.WorkspaceID, Status: dbgen.SubscriptionStatus(u.Status), PaymentURL: u.PaymentURL,
		}
		if u.NextDueDate != nil {
			p.NextDueDate = pgtype.Date{Time: *u.NextDueDate, Valid: true}
		}
		_, err := q.UpdateSubscriptionBilling(ctx, p)
		return notFound(err)
	})
}

func (r *PostgresBilling) RecordEvent(ctx context.Context, s domain.Subscription, e domain.BillingEvent) (bool, error) {
	var n int64
	err := run(ctx, r.pool, subscriptionScope(s), func(q *dbgen.Queries, _ pgx.Tx) error {
		var err error
		n, err = q.RecordBillingEvent(ctx, dbgen.RecordBillingEventParams{
			Provider: e.Provider, EventID: e.ID, WorkspaceID: s.WorkspaceID, Kind: string(e.Kind),
		})
		return err
	})
	return n > 0, err
}

func subscriptionOf(s dbgen.Subscription) domain.Subscription {
	var due time.Time
	if s.NextDueDate.Valid {
		due = s.NextDueDate.Time
	}
	return domain.Subscription{
		WorkspaceID:        s.WorkspaceID,
		Provider:           s.Provider,
		ExternalCustomerID: s.ExternalCustomerID,
		ExternalID:         s.ExternalID,
		Status:             domain.SubscriptionStatus(s.Status),
		Seats:              s.Seats,
		AmountCents:        s.AmountCents,
		NextDueDate:        due,
		PaymentURL:         s.PaymentURL,
		CreatedBy:          s.CreatedBy,
		CreatedAt:          s.CreatedAt,
		CancelledAt:        s.CancelledAt,
	}
}
