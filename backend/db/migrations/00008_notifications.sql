-- Notifications: the inbox of each user in each workspace, the Web Push
-- subscriptions of the browsers and the email preference.
--
-- The worker writes the notification with the recipient's scope, so the
-- policy requires the workspace AND the user. `key` makes the delivery
-- idempotent (e.g. "list:<id>"), and emailed_at/pushed_at avoid sending again
-- when the job is retried.

-- +goose Up
CREATE TABLE notifications (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    user_id      uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    kind         text NOT NULL,
    key          text NOT NULL,
    title        text NOT NULL CHECK (length(title) <= 200),
    body         text NOT NULL DEFAULT '' CHECK (length(body) <= 1000),
    url          text NOT NULL DEFAULT '',
    created_at   timestamptz NOT NULL DEFAULT now(),
    read_at      timestamptz,
    emailed_at   timestamptz,
    pushed_at    timestamptz,
    UNIQUE (workspace_id, user_id, key)
);

CREATE INDEX notifications_inbox ON notifications (workspace_id, user_id, created_at DESC);

-- Web Push subscriptions (one per browser). They belong to the user and work
-- in any workspace, like the Shopee credential.
CREATE TABLE push_subscriptions (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    endpoint   text NOT NULL CHECK (length(endpoint) <= 1000),
    p256dh     text NOT NULL CHECK (length(p256dh) <= 200),
    auth       text NOT NULL CHECK (length(auth) <= 100),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (user_id, endpoint)
);

CREATE TABLE notification_preferences (
    user_id    uuid PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    email      boolean NOT NULL DEFAULT true,
    updated_at timestamptz NOT NULL DEFAULT now()
);

GRANT SELECT, INSERT, UPDATE ON notifications TO parceiros_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON push_subscriptions, notification_preferences TO parceiros_app;

ALTER TABLE notifications ENABLE ROW LEVEL SECURITY;
ALTER TABLE notifications FORCE ROW LEVEL SECURITY;
ALTER TABLE push_subscriptions ENABLE ROW LEVEL SECURITY;
ALTER TABLE push_subscriptions FORCE ROW LEVEL SECURITY;
ALTER TABLE notification_preferences ENABLE ROW LEVEL SECURITY;
ALTER TABLE notification_preferences FORCE ROW LEVEL SECURITY;

CREATE POLICY notifications_owner ON notifications
    USING (workspace_id = app_workspace_id() AND user_id = app_user_id())
    WITH CHECK (workspace_id = app_workspace_id() AND user_id = app_user_id());
CREATE POLICY push_subscriptions_owner ON push_subscriptions
    USING (user_id = app_user_id())
    WITH CHECK (user_id = app_user_id());
CREATE POLICY notification_preferences_owner ON notification_preferences
    USING (user_id = app_user_id())
    WITH CHECK (user_id = app_user_id());

-- +goose Down
DROP TABLE notification_preferences;
DROP TABLE push_subscriptions;
DROP TABLE notifications;
