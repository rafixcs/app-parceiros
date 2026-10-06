-- Queries of the billing module. Generate the code with `make sqlc`.

-- name: SubscriptionByWorkspace :one
SELECT * FROM subscriptions WHERE workspace_id = $1;

-- name: SubscriptionByExternalID :one
SELECT * FROM subscriptions WHERE provider = $1 AND external_id = $2;

-- name: SaveSubscription :one
-- Stores the subscription of the workspace. A new one only replaces a
-- cancelled one: no row returned means one is already in progress.
INSERT INTO subscriptions (
    workspace_id, provider, external_customer_id, external_id, status,
    seats, amount_cents, next_due_date, payment_url, created_by
) VALUES ($1, $2, $3, $4, 'pending', $5, $6, $7, $8, $9)
ON CONFLICT (workspace_id) DO UPDATE
    SET provider = EXCLUDED.provider,
        external_customer_id = EXCLUDED.external_customer_id,
        external_id = EXCLUDED.external_id,
        status = 'pending',
        seats = EXCLUDED.seats,
        amount_cents = EXCLUDED.amount_cents,
        next_due_date = EXCLUDED.next_due_date,
        payment_url = EXCLUDED.payment_url,
        created_by = EXCLUDED.created_by,
        updated_at = now(),
        cancelled_at = NULL
    WHERE subscriptions.cancelled_at IS NOT NULL
RETURNING *;

-- name: UpdateSubscriptionPlan :one
-- Changes seats and amount of a subscription in progress (more or fewer
-- affiliates in the group).
UPDATE subscriptions
SET seats = @seats,
    amount_cents = @amount_cents,
    updated_at = now()
WHERE workspace_id = @workspace_id AND cancelled_at IS NULL
RETURNING *;

-- name: UpdateSubscriptionBilling :one
-- Status coming from the gateway. A null due date or payment_url keeps the
-- current one (a confirmed payment brings no open invoice).
UPDATE subscriptions
SET status = @status,
    next_due_date = coalesce(sqlc.narg(next_due_date), next_due_date),
    payment_url = coalesce(sqlc.narg(payment_url), payment_url),
    updated_at = now()
WHERE workspace_id = @workspace_id
RETURNING *;

-- name: CancelSubscription :one
UPDATE subscriptions
SET status = 'cancelled',
    payment_url = NULL,
    cancelled_at = now(),
    updated_at = now()
WHERE workspace_id = $1 AND cancelled_at IS NULL
RETURNING *;

-- name: RecordBillingEvent :execrows
-- Returns 0 when the event was already processed (a gateway redelivery).
INSERT INTO billing_events (provider, event_id, workspace_id, kind)
VALUES ($1, $2, $3, $4)
ON CONFLICT (provider, event_id) DO NOTHING;
