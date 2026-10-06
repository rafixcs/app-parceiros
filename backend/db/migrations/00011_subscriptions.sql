-- Subscriptions: trial, paid plans and suspension for lack of payment.
--
-- The access of each workspace lasts until workspaces.access_until. Sign-up
-- gives 7 days of trial; each confirmed payment extends the access to the end
-- of the paid cycle plus a grace period. After that date the workspace is
-- suspended without depending on any job or webhook: if a gateway event is
-- lost, the workspace suspends itself, and the next payment reactivates it.
--
-- The workspace owner pays: the solo affiliate pays their own plan and the
-- mentor pays the mentorship per seat (one seat per affiliate in the group).
--
-- The subscriptions table keeps the subscription at the gateway (Asaas by
-- default). The webhook has no user nor workspace: it finds the subscription
-- by its external id (app.external_subscription_id) and then acts with the
-- scope of its workspace.

-- +goose Up
CREATE FUNCTION app_external_subscription_id() RETURNS text
    LANGUAGE sql STABLE
    AS $$ SELECT nullif(current_setting('app.external_subscription_id', true), '') $$;

-- Limits and prices per plan. "seats" is the most a mentorship can buy; the
-- prices are in cents per month: the solo plan per workspace, the mentorship
-- per seat. Provisional values, adjustable without a deploy.
INSERT INTO plan_limits (plan, key, value) VALUES
    ('solo', 'trial_seats', 0),
    ('mentorship', 'trial_seats', 5),
    ('solo', 'price_cents', 2990),
    ('mentorship', 'seat_price_cents', 1490)
ON CONFLICT (plan, key) DO UPDATE SET value = EXCLUDED.value;

CREATE TYPE subscription_status AS ENUM ('pending', 'active', 'overdue', 'cancelled');

-- The current subscription of each workspace. A new one only replaces a
-- cancelled one.
CREATE TABLE subscriptions (
    workspace_id         uuid PRIMARY KEY REFERENCES workspaces (id) ON DELETE CASCADE,
    provider             text NOT NULL,
    external_customer_id text NOT NULL,
    external_id          text NOT NULL,
    status               subscription_status NOT NULL DEFAULT 'pending',
    seats                integer NOT NULL CHECK (seats >= 1),
    amount_cents         bigint NOT NULL CHECK (amount_cents > 0),
    -- due date of the open charge (or of the next one)
    next_due_date        date NOT NULL,
    -- open invoice at the gateway (PIX, boleto or card)
    payment_url          text,
    created_by           uuid NOT NULL REFERENCES users (id),
    created_at           timestamptz NOT NULL DEFAULT now(),
    updated_at           timestamptz NOT NULL DEFAULT now(),
    cancelled_at         timestamptz,
    UNIQUE (provider, external_id)
);

-- Webhook events already processed, to ignore redeliveries.
CREATE TABLE billing_events (
    provider     text NOT NULL,
    event_id     text NOT NULL,
    workspace_id uuid NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    kind         text NOT NULL,
    received_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (provider, event_id)
);

CREATE INDEX billing_events_workspace ON billing_events (workspace_id, received_at);

GRANT SELECT, INSERT, UPDATE ON subscriptions TO parceiros_app;
GRANT SELECT, INSERT ON billing_events TO parceiros_app;

ALTER TABLE subscriptions ENABLE ROW LEVEL SECURITY;
ALTER TABLE subscriptions FORCE ROW LEVEL SECURITY;
ALTER TABLE billing_events ENABLE ROW LEVEL SECURITY;
ALTER TABLE billing_events FORCE ROW LEVEL SECURITY;

-- Through the API, only owner and mentor see the subscription and only the
-- owner changes it. Without a user in the scope (the webhook, already in the
-- workspace of the subscription), the workspace applies.
CREATE POLICY subscriptions_select ON subscriptions FOR SELECT
    USING (
        (workspace_id = app_workspace_id() AND (app_user_id() IS NULL OR app_is_manager()))
        OR external_id = app_external_subscription_id()
    );
CREATE POLICY subscriptions_insert ON subscriptions FOR INSERT
    WITH CHECK (workspace_id = app_workspace_id() AND (app_user_id() IS NULL OR app_is_owner()));
CREATE POLICY subscriptions_update ON subscriptions FOR UPDATE
    USING (workspace_id = app_workspace_id() AND (app_user_id() IS NULL OR app_is_owner()));

CREATE POLICY billing_events_workspace ON billing_events
    USING (workspace_id = app_workspace_id())
    WITH CHECK (workspace_id = app_workspace_id());

-- +goose Down
DROP TABLE billing_events;
DROP TABLE subscriptions;
DROP TYPE subscription_status;
DELETE FROM plan_limits WHERE key IN ('trial_seats', 'price_cents', 'seat_price_cents');
DROP FUNCTION app_external_subscription_id();
