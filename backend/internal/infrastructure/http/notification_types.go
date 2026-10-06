package http

import (
	"time"

	"github.com/google/uuid"

	"github.com/rafixcs/app-parceiros/backend/internal/domain"
)

type notificationResponse struct {
	ID        uuid.UUID  `json:"id"`
	Kind      string     `json:"kind"`
	Title     string     `json:"title"`
	Body      string     `json:"body"`
	URL       string     `json:"url"`
	CreatedAt time.Time  `json:"created_at"`
	ReadAt    *time.Time `json:"read_at"`
}

func notificationResponseOf(n domain.Notification) notificationResponse {
	return notificationResponse{
		ID: n.ID, Kind: n.Kind, Title: n.Title, Body: n.Body, URL: n.URL, CreatedAt: n.CreatedAt, ReadAt: n.ReadAt,
	}
}

type inboxResponse struct {
	Notifications []notificationResponse `json:"notifications"`
	Unread        int64                  `json:"unread"`
}

type notificationPreferencesResponse struct {
	Email bool `json:"email"`
	// PushPublicKey is the VAPID key for the browser to subscribe; null when
	// the server has no Web Push.
	PushPublicKey *string `json:"push_public_key"`
	// PushSubscriptions counts the browsers subscribed to Web Push.
	PushSubscriptions int64 `json:"push_subscriptions"`
}

func notificationPreferencesResponseOf(p domain.NotificationPreferences) notificationPreferencesResponse {
	return notificationPreferencesResponse{Email: p.Email, PushPublicKey: p.PushPublicKey, PushSubscriptions: p.PushSubscriptions}
}

type setNotificationPreferencesRequest struct {
	Email *bool `json:"email"`
}

// pushSubscriptionRequest is the browser's PushSubscription.toJSON().
type pushSubscriptionRequest struct {
	Endpoint string `json:"endpoint"`
	Keys     struct {
		P256dh string `json:"p256dh"`
		Auth   string `json:"auth"`
	} `json:"keys"`
}

type pushUnsubscribeRequest struct {
	Endpoint string `json:"endpoint"`
}
