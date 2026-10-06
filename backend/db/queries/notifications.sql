-- Queries of the notifications module. The inbox filters by workspace and
-- user; push subscriptions and preferences, by user. RLS repeats the filters.

-- name: CreateNotification :one
-- Idempotent by key: repeating the delivery returns the same notification.
INSERT INTO notifications (workspace_id, user_id, kind, key, title, body, url)
VALUES (@workspace_id, @user_id, @kind, @key, @title, @body, @url)
ON CONFLICT (workspace_id, user_id, key) DO UPDATE SET key = excluded.key
RETURNING *;

-- name: NotificationInbox :many
SELECT * FROM notifications
WHERE workspace_id = @workspace_id AND user_id = @user_id
ORDER BY created_at DESC, id
LIMIT @max_rows;

-- name: CountUnreadNotifications :one
SELECT count(*) FROM notifications
WHERE workspace_id = @workspace_id AND user_id = @user_id AND read_at IS NULL;

-- name: MarkNotificationRead :execrows
UPDATE notifications SET read_at = coalesce(read_at, now())
WHERE id = @id AND workspace_id = @workspace_id AND user_id = @user_id;

-- name: MarkAllNotificationsRead :exec
UPDATE notifications SET read_at = now()
WHERE workspace_id = @workspace_id AND user_id = @user_id AND read_at IS NULL;

-- name: MarkNotificationEmailed :exec
UPDATE notifications SET emailed_at = now()
WHERE id = @id AND workspace_id = @workspace_id AND user_id = @user_id;

-- name: MarkNotificationPushed :exec
UPDATE notifications SET pushed_at = now()
WHERE id = @id AND workspace_id = @workspace_id AND user_id = @user_id;

-- name: SavePushSubscription :exec
INSERT INTO push_subscriptions (user_id, endpoint, p256dh, auth)
VALUES (@user_id, @endpoint, @p256dh, @auth)
ON CONFLICT (user_id, endpoint) DO UPDATE SET p256dh = excluded.p256dh, auth = excluded.auth;

-- name: CountPushSubscriptions :one
SELECT count(*) FROM push_subscriptions WHERE user_id = @user_id;

-- name: PushSubscriptionsByUser :many
SELECT * FROM push_subscriptions WHERE user_id = @user_id ORDER BY created_at, id;

-- name: DeletePushSubscription :execrows
DELETE FROM push_subscriptions WHERE user_id = @user_id AND endpoint = @endpoint;

-- name: NotificationPreferencesByUser :one
SELECT * FROM notification_preferences WHERE user_id = @user_id;

-- name: SaveNotificationPreferences :exec
INSERT INTO notification_preferences (user_id, email)
VALUES (@user_id, @email)
ON CONFLICT (user_id) DO UPDATE SET email = excluded.email, updated_at = now();
