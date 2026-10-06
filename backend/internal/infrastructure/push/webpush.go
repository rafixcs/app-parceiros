package push

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/SherClockHolmes/webpush-go"

	"github.com/rafixcs/app-parceiros/backend/internal/domain"
)

// WebPush signs with VAPID and encrypts the message (RFC 8291).
type WebPush struct {
	PublicKeyB64  string
	PrivateKeyB64 string
	Subject       string // contact email of the sender (the VAPID "sub" claim)
	HTTP          *http.Client
}

var _ domain.PushSender = WebPush{}

func (w WebPush) PublicKey() string { return w.PublicKeyB64 }

func (w WebPush) Send(ctx context.Context, sub domain.PushSubscription, msg domain.PushMessage) error {
	// The endpoint was checked on subscription; checking again protects
	// against old rows, since the worker makes the HTTP call.
	if !domain.PushEndpointAllowed(sub.Endpoint) {
		return domain.ErrPushSubscriptionExpired
	}
	client := w.HTTP
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	resp, err := webpush.SendNotificationWithContext(ctx, Payload(msg), &webpush.Subscription{
		Endpoint: sub.Endpoint,
		Keys:     webpush.Keys{P256dh: sub.P256dh, Auth: sub.Auth},
	}, &webpush.Options{
		HTTPClient: client, Subscriber: w.Subject, TTL: int((24 * time.Hour).Seconds()),
		Urgency: webpush.UrgencyNormal, VAPIDPublicKey: w.PublicKeyB64, VAPIDPrivateKey: w.PrivateKeyB64,
	})
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
	switch {
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone:
		return domain.ErrPushSubscriptionExpired
	case resp.StatusCode >= 300:
		return fmt.Errorf("push service answered %d", resp.StatusCode)
	}
	return nil
}

// Payload is the JSON the front end's service worker (web/public/push-sw.js)
// shows as a notification: {"id", "title", "body", "url"}.
func Payload(msg domain.PushMessage) []byte {
	b, _ := json.Marshal(struct {
		ID    string `json:"id"`
		Title string `json:"title"`
		Body  string `json:"body"`
		URL   string `json:"url"`
	}{msg.ID.String(), msg.Title, msg.Body, msg.URL})
	return b
}
