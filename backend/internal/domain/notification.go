package domain

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Notice is a notification to several users of a workspace. Key makes it
// idempotent per user (e.g. "list:<id>"); URL is a path of the app (e.g.
// "/w/<id>/listas/<id>"). Title and Body are shown to the customer, so they
// are in pt-BR.
type Notice struct {
	WorkspaceID uuid.UUID
	UserIDs     []uuid.UUID
	Kind        string
	Key         string
	Title       string
	Body        string
	URL         string
}

// Notifier delivers notices in the app inbox, by email and by Web Push
// (implemented by service.NotificationService).
type Notifier interface {
	Notify(ctx context.Context, n Notice) error
}

// Limits of a notification, as the table enforces them.
const (
	MaxNotificationTitle = 200
	MaxNotificationBody  = 1000
)

// Notification is one notice in the inbox of a user in a workspace.
// EmailedAt and PushedAt record the channels already used, so a retried
// delivery does not send again.
type Notification struct {
	ID          uuid.UUID
	WorkspaceID uuid.UUID
	UserID      uuid.UUID
	Kind        string
	Key         string
	Title       string
	Body        string
	URL         string
	CreatedAt   time.Time
	ReadAt      *time.Time
	EmailedAt   *time.Time
	PushedAt    *time.Time
}

// Inbox is the latest notifications of a user in a workspace and how many of
// them (all of them, not only the listed ones) are unread.
type Inbox struct {
	Notifications []Notification
	Unread        int64
}

// NotificationPreferences of a user, valid in every workspace.
type NotificationPreferences struct {
	Email bool
	// PushPublicKey is the VAPID key the browser subscribes with; nil when the
	// server has no Web Push configured.
	PushPublicKey *string
	// PushSubscriptions counts the browsers subscribed to Web Push.
	PushSubscriptions int64
}

// PushSubscription is the PushSubscription of a browser
// (PushSubscription.toJSON()).
type PushSubscription struct {
	Endpoint string
	P256dh   string
	Auth     string
}

// Limits of the keys of a push subscription, as the table enforces them.
const (
	MaxPushP256dh = 200
	MaxPushAuth   = 100
)

// NotificationDelivery is a notice for one recipient: the payload of the
// deliver_notification job. It carries only content and ids; the recipient's
// email is read at delivery time.
type NotificationDelivery struct {
	WorkspaceID uuid.UUID
	UserID      uuid.UUID
	Kind        string
	Key         string
	Title       string
	Body        string
	URL         string
}

// Recipient is the inbox the delivery goes to.
func (d NotificationDelivery) Recipient() Actor {
	return Actor{UserID: d.UserID, WorkspaceID: d.WorkspaceID}
}

// NotificationQueue enqueues one deliver_notification job per delivery.
type NotificationQueue interface {
	EnqueueNotifications(ctx context.Context, ds ...NotificationDelivery) error
}

// PushMessage is what the browser shows as a notification.
type PushMessage struct {
	ID    uuid.UUID
	Title string
	Body  string
	URL   string
}

// ErrPushSubscriptionExpired: the push service says the subscription no
// longer exists (404 or 410), so it must be deleted.
var ErrPushSubscriptionExpired = errors.New("push subscription expired")

// PushSender sends Web Push messages (signed with VAPID).
type PushSender interface {
	PublicKey() string
	// Send returns ErrPushSubscriptionExpired when the subscription is gone.
	Send(ctx context.Context, sub PushSubscription, msg PushMessage) error
}

// NotificationMailer writes and sends the notification and invite emails.
type NotificationMailer interface {
	SendNotification(ctx context.Context, to string, n Notification) error
	InviteMailer
}

// NotificationRepository stores the inboxes, the push subscriptions and the
// preferences. Inbox calls act on the inbox of the actor (user and
// workspace); subscriptions and preferences belong to the user.
type NotificationRepository interface {
	// SaveNotification records the delivery in the recipient's inbox. It is
	// idempotent by key: repeating it returns the stored notification.
	SaveNotification(ctx context.Context, d NotificationDelivery) (Notification, error)
	Inbox(ctx context.Context, a Actor, limit int32) ([]Notification, error)
	CountUnread(ctx context.Context, a Actor) (int64, error)
	// MarkRead returns ErrNotFound when the inbox has no such notification.
	MarkRead(ctx context.Context, a Actor, id uuid.UUID) error
	MarkAllRead(ctx context.Context, a Actor) error
	MarkEmailed(ctx context.Context, a Actor, id uuid.UUID) error
	MarkPushed(ctx context.Context, a Actor, id uuid.UUID) error

	// EmailEnabled is true when the user stored no preference.
	EmailEnabled(ctx context.Context, userID uuid.UUID) (bool, error)
	SetEmailEnabled(ctx context.Context, userID uuid.UUID, enabled bool) error

	CountPushSubscriptions(ctx context.Context, userID uuid.UUID) (int64, error)
	PushSubscriptions(ctx context.Context, userID uuid.UUID) ([]PushSubscription, error)
	// SavePushSubscription creates the subscription or updates the keys of
	// the same endpoint.
	SavePushSubscription(ctx context.Context, userID uuid.UUID, s PushSubscription) error
	// DeletePushSubscription says whether a subscription was deleted.
	DeletePushSubscription(ctx context.Context, userID uuid.UUID, endpoint string) (bool, error)
}

// Push services of the browsers. The worker POSTs to the endpoint the
// browser gave; accepting only these hosts keeps anyone from using the app
// to call internal addresses.
var pushHosts = []string{
	"fcm.googleapis.com",
	"android.googleapis.com",
	"push.services.mozilla.com",
	"notify.windows.com",
	"push.apple.com",
}

// MaxPushEndpoint is the longest endpoint accepted.
const MaxPushEndpoint = 1000

// PushEndpointAllowed says whether the endpoint is https on a known push
// service, without user info or port.
func PushEndpointAllowed(endpoint string) bool {
	if len(endpoint) > MaxPushEndpoint {
		return false
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" {
		return false
	}
	h := strings.ToLower(u.Hostname())
	for _, p := range pushHosts {
		if h == p || strings.HasSuffix(h, "."+p) {
			return true
		}
	}
	return false
}

var (
	ErrNotificationNotFound           = NewError(KindNotFound, "notification_not_found")
	ErrPushUnavailable                = NewError(KindUnavailable, "push_unavailable")
	ErrInvalidPushSubscription        = NewError(KindInvalid, "invalid_push_subscription")
	ErrTooManyPushSubscriptions       = NewError(KindConflict, "too_many_push_subscriptions")
	ErrInvalidNotificationPreferences = NewError(KindInvalid, "invalid_notification_preferences")
)
