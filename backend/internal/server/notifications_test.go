package server

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/rafixcs/app-parceiros/backend/internal/domain"
	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/database"
)

// fakeNotificationQueue keeps the deliver_notification jobs; the test runs
// them with deliverNotifications.
type fakeNotificationQueue struct {
	mu      sync.Mutex
	pending []domain.NotificationDelivery
}

func (q *fakeNotificationQueue) EnqueueNotifications(_ context.Context, ds ...domain.NotificationDelivery) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.pending = append(q.pending, ds...)
	return nil
}

func (q *fakeNotificationQueue) take() []domain.NotificationDelivery {
	q.mu.Lock()
	defer q.mu.Unlock()
	ds := q.pending
	q.pending = nil
	return ds
}

// deliverNotifications works the enqueued deliver_notification jobs, as the
// worker would.
func (a *testApp) deliverNotifications() {
	a.t.Helper()
	for _, d := range a.notices.take() {
		if err := a.svcs.notifications.Deliver(context.Background(), d); err != nil {
			a.t.Fatalf("delivering %+v: %v", d, err)
		}
	}
}

type fakeMailer struct {
	mu   sync.Mutex
	sent []domain.Email
	fail bool
}

func (m *fakeMailer) Send(_ context.Context, e domain.Email) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail {
		return errors.New("smtp down")
	}
	m.sent = append(m.sent, e)
	return nil
}

type fakePush struct {
	mu      sync.Mutex
	sent    []string
	msgs    []domain.PushMessage
	expired map[string]bool
}

func (p *fakePush) PublicKey() string { return "public-key" }

func (p *fakePush) Send(_ context.Context, sub domain.PushSubscription, msg domain.PushMessage) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.expired[sub.Endpoint] {
		return domain.ErrPushSubscriptionExpired
	}
	p.sent = append(p.sent, sub.Endpoint)
	p.msgs = append(p.msgs, msg)
	return nil
}

// withNotificationChannels turns on email and Web Push with fakes.
func withNotificationChannels(m *fakeMailer, p *fakePush) func(*infra) {
	return func(in *infra) {
		if m != nil {
			in.mailer = m
		}
		if p != nil {
			in.push = p
		}
	}
}

// JSON of the notification routes, as clients see them.

type notificationJSON struct {
	ID        uuid.UUID  `json:"id"`
	Kind      string     `json:"kind"`
	Title     string     `json:"title"`
	Body      string     `json:"body"`
	URL       string     `json:"url"`
	CreatedAt time.Time  `json:"created_at"`
	ReadAt    *time.Time `json:"read_at"`
}

type inboxJSON struct {
	Notifications []notificationJSON `json:"notifications"`
	Unread        int64              `json:"unread"`
}

type notificationPreferencesJSON struct {
	Email             bool    `json:"email"`
	PushPublicKey     *string `json:"push_public_key"`
	PushSubscriptions int64   `json:"push_subscriptions"`
}

func pushSubscription(endpoint string) map[string]any {
	return map[string]any{"endpoint": endpoint, "keys": map[string]string{"p256dh": "k", "auth": "a"}}
}

func (a *testApp) inbox(sub string, ws uuid.UUID) inboxJSON {
	a.t.Helper()
	var in inboxJSON
	a.must(sub, http.MethodGet, wsPath(ws, "/notifications"), nil, &in, http.StatusOK)
	return in
}

func (a *testApp) notificationPreferences(sub string) notificationPreferencesJSON {
	a.t.Helper()
	var p notificationPreferencesJSON
	a.must(sub, http.MethodGet, "/v1/me/notifications", nil, &p, http.StatusOK)
	return p
}

func (a *testApp) notify(n domain.Notice) {
	a.t.Helper()
	if err := a.svcs.notifications.Notify(context.Background(), n); err != nil {
		a.t.Fatal(err)
	}
}

func TestNotificationPreferencesAndPush(t *testing.T) {
	mailer, push := &fakeMailer{}, &fakePush{expired: map[string]bool{}}
	a := newTestApp(t, withNotificationChannels(mailer, push))
	ana := a.me("ana")
	ws := a.personal("ana")

	p := a.notificationPreferences("ana")
	if !p.Email || p.PushPublicKey == nil || *p.PushPublicKey != "public-key" || p.PushSubscriptions != 0 {
		t.Fatalf("initial preferences: %+v", p)
	}
	a.mustFail("ana", http.MethodPost, "/v1/me/push", pushSubscription("https://internal.local/x"), http.StatusUnprocessableEntity, "invalid_push_subscription")
	a.mustFail("ana", http.MethodPost, "/v1/me/push", map[string]any{"endpoint": "https://fcm.googleapis.com/x"}, http.StatusUnprocessableEntity, "invalid_push_subscription")
	alive, dead := "https://fcm.googleapis.com/fcm/send/alive", "https://web.push.apple.com/dead"
	a.must("ana", http.MethodPost, "/v1/me/push", pushSubscription(alive), nil, http.StatusNoContent)
	a.must("ana", http.MethodPost, "/v1/me/push", pushSubscription(alive), nil, http.StatusNoContent) // repeating does not duplicate
	a.must("ana", http.MethodPost, "/v1/me/push", pushSubscription(dead), nil, http.StatusNoContent)
	a.must("ana", http.MethodPut, "/v1/me/notifications", map[string]any{"email": false}, &p, http.StatusOK)
	if p.Email || p.PushSubscriptions != 2 {
		t.Fatalf("after turning email off: %+v", p)
	}
	a.mustFail("ana", http.MethodPut, "/v1/me/notifications", map[string]any{}, http.StatusUnprocessableEntity, "invalid_notification_preferences")

	// With email off, only push goes; the expired subscription is deleted.
	push.expired[dead] = true
	notice := domain.Notice{WorkspaceID: ws.ID, UserIDs: []uuid.UUID{ana.ID}, Kind: "test", Key: "k1", Title: "Oi", Body: "Corpo", URL: "/"}
	a.notify(notice)
	a.deliverNotifications()
	if len(mailer.sent) != 0 || len(push.sent) != 1 || push.sent[0] != alive {
		t.Fatalf("delivery: emails %+v, push %+v", mailer.sent, push.sent)
	}
	if m := push.msgs[0]; m.Title != "Oi" || m.Body != "Corpo" || m.URL != "/" || m.ID == uuid.Nil {
		t.Fatalf("push message: %+v", m)
	}
	if p := a.notificationPreferences("ana"); p.PushSubscriptions != 1 {
		t.Fatalf("the expired subscription is still there: %+v", p)
	}

	// With email on, a new notification goes by email. If SMTP fails, the
	// job fails (and is retried) without repeating the push.
	a.must("ana", http.MethodPut, "/v1/me/notifications", map[string]any{"email": true}, &p, http.StatusOK)
	mailer.fail = true
	notice.Key = "k2"
	a.notify(notice)
	ds := a.notices.take()
	if len(ds) != 1 {
		t.Fatalf("enqueued %d deliveries", len(ds))
	}
	if err := a.svcs.notifications.Deliver(context.Background(), ds[0]); err == nil {
		t.Fatal("delivery with SMTP down did not fail")
	}
	mailer.fail = false
	if err := a.svcs.notifications.Deliver(context.Background(), ds[0]); err != nil {
		t.Fatal(err)
	}
	if len(mailer.sent) != 1 || mailer.sent[0].To != "ana@dev.local" || len(push.sent) != 2 {
		t.Fatalf("second delivery: emails %+v, push %+v", mailer.sent, push.sent)
	}
	if !strings.Contains(mailer.sent[0].Text, "https://app.test/") {
		t.Fatalf("email without the app link: %q", mailer.sent[0].Text)
	}
	// A retry after everything was sent sends nothing again.
	if err := a.svcs.notifications.Deliver(context.Background(), ds[0]); err != nil {
		t.Fatal(err)
	}
	if len(mailer.sent) != 1 || len(push.sent) != 2 {
		t.Fatalf("retry sent again: emails %d, push %d", len(mailer.sent), len(push.sent))
	}

	a.must("ana", http.MethodDelete, "/v1/me/push", map[string]any{"endpoint": alive}, nil, http.StatusNoContent)
	a.must("ana", http.MethodDelete, "/v1/me/push", map[string]any{"endpoint": alive}, nil, http.StatusNoContent) // missing is fine
	if p := a.notificationPreferences("ana"); p.PushSubscriptions != 0 {
		t.Fatalf("after unsubscribing: %+v", p)
	}
	a.mustFail("ana", http.MethodPost, wsPath(ws.ID, "/notifications/"+uuid.NewString()+"/read"), nil, http.StatusNotFound, "notification_not_found")
	a.mustFail("ana", http.MethodPost, wsPath(ws.ID, "/notifications/not-a-uuid/read"), nil, http.StatusNotFound, "notification_not_found")
}

func TestTooManyPushSubscriptions(t *testing.T) {
	a := newTestApp(t, withNotificationChannels(&fakeMailer{}, &fakePush{}))
	endpoint := func(i int) string { return "https://fcm.googleapis.com/fcm/send/" + string(rune('a'+i)) }
	for i := range 20 {
		a.must("ana", http.MethodPost, "/v1/me/push", pushSubscription(endpoint(i)), nil, http.StatusNoContent)
	}
	a.mustFail("ana", http.MethodPost, "/v1/me/push", pushSubscription(endpoint(20)), http.StatusConflict, "too_many_push_subscriptions")
	// Subscribing a browser that is already subscribed still works.
	a.must("ana", http.MethodPost, "/v1/me/push", pushSubscription(endpoint(3)), nil, http.StatusNoContent)
	if p := a.notificationPreferences("ana"); p.PushSubscriptions != 20 {
		t.Fatalf("subscriptions = %d", p.PushSubscriptions)
	}
	// The limit is per user.
	a.must("bia", http.MethodPost, "/v1/me/push", pushSubscription(endpoint(20)), nil, http.StatusNoContent)
}

func TestNotifyInbox(t *testing.T) {
	mailer := &fakeMailer{}
	a := newTestApp(t, withNotificationChannels(mailer, nil))
	ws := a.mentorship("mentor", "Turma")
	mentor := a.me("mentor")
	bia := a.join("mentor", "bia", ws.ID)
	a.me("cris") // a user who is not notified

	long := strings.Repeat("á", 250)
	a.notify(domain.Notice{WorkspaceID: ws.ID, UserIDs: []uuid.UUID{mentor.ID, bia.ID}, Kind: "list", Key: "list:1",
		Title: long, Body: "3 produtos", URL: "/w/" + ws.ID.String() + "/listas/1"})
	a.notify(domain.Notice{WorkspaceID: ws.ID, UserIDs: []uuid.UUID{bia.ID}, Kind: "list", Key: "list:2", Title: "Segunda"})
	// The same key again does not create another notification nor email.
	a.notify(domain.Notice{WorkspaceID: ws.ID, UserIDs: []uuid.UUID{bia.ID}, Kind: "list", Key: "list:2", Title: "Segunda"})
	a.notify(domain.Notice{WorkspaceID: ws.ID, Kind: "nobody", Key: "x", Title: "x"}) // no recipient
	a.deliverNotifications()
	if len(mailer.sent) != 3 {
		t.Fatalf("emails sent = %d, want 3", len(mailer.sent))
	}

	in := a.inbox("bia", ws.ID)
	if in.Unread != 2 || len(in.Notifications) != 2 || in.Notifications[0].Title != "Segunda" {
		t.Fatalf("bia's inbox: %+v", in)
	}
	first := in.Notifications[1]
	if first.Kind != "list" || first.Body != "3 produtos" || first.ReadAt != nil ||
		len([]rune(first.Title)) != 200 || !strings.HasSuffix(first.Title, "…") {
		t.Fatalf("first notification: %+v", first)
	}
	if in := a.inbox("mentor", ws.ID); in.Unread != 1 || len(in.Notifications) != 1 {
		t.Fatalf("mentor's inbox: %+v", in)
	}
	if in := a.inbox("cris", a.personal("cris").ID); in.Unread != 0 || in.Notifications == nil || len(in.Notifications) != 0 {
		t.Fatalf("cris's inbox: %+v", in)
	}
	// The personal workspace of bia does not show the mentorship notices.
	if in := a.inbox("bia", a.personal("bia").ID); len(in.Notifications) != 0 {
		t.Fatalf("bia's personal inbox: %+v", in)
	}

	a.must("bia", http.MethodPost, wsPath(ws.ID, "/notifications/"+first.ID.String()+"/read"), nil, nil, http.StatusNoContent)
	a.must("bia", http.MethodPost, wsPath(ws.ID, "/notifications/"+first.ID.String()+"/read"), nil, nil, http.StatusNoContent) // again
	if in := a.inbox("bia", ws.ID); in.Unread != 1 || in.Notifications[1].ReadAt == nil || in.Notifications[0].ReadAt != nil {
		t.Fatalf("after reading one: %+v", in)
	}
	a.must("bia", http.MethodPost, wsPath(ws.ID, "/notifications/read"), nil, nil, http.StatusNoContent)
	if in := a.inbox("bia", ws.ID); in.Unread != 0 {
		t.Fatalf("after reading all: %+v", in)
	}
	if in := a.inbox("mentor", ws.ID); in.Unread != 1 {
		t.Fatalf("bia's reading changed the mentor's inbox: %+v", in)
	}
}

// An unverified email gets no notification email (it may be someone else's).
func TestNotificationEmailNeedsVerifiedEmail(t *testing.T) {
	mailer := &fakeMailer{}
	a := newTestApp(t, withNotificationChannels(mailer, nil))
	ana := a.me("ana")
	ws := a.personal("ana")
	a.admin("UPDATE users SET email_verified = false WHERE id = $1", ana.ID)
	a.notify(domain.Notice{WorkspaceID: ws.ID, UserIDs: []uuid.UUID{ana.ID}, Kind: "test", Key: "k", Title: "Oi"})
	a.deliverNotifications()
	if len(mailer.sent) != 0 {
		t.Fatalf("emailed an unverified address: %+v", mailer.sent)
	}
	if in := a.inbox("ana", ws.ID); in.Unread != 1 {
		t.Fatalf("inbox: %+v", in)
	}
}

func TestInviteEmail(t *testing.T) {
	mailer := &fakeMailer{}
	a := newTestApp(t, withNotificationChannels(mailer, &fakePush{}))
	ws := a.mentorship("mentor", "Turma Top")
	i := a.invite("mentor", ws.ID, map[string]any{"email": "new@example.com"})
	if i.EmailSent == nil || !*i.EmailSent || len(mailer.sent) != 1 {
		t.Fatalf("invite: %+v, emails %+v", i, mailer.sent)
	}
	e := mailer.sent[0]
	if e.To != "new@example.com" || !strings.Contains(e.Subject, "Turma Top") || !strings.Contains(e.Text, i.URL) {
		t.Fatalf("invite email: %+v", e)
	}
	// Without an email in the invite, nothing is sent and the field is absent.
	i = a.invite("mentor", ws.ID, nil)
	if i.EmailSent != nil || len(mailer.sent) != 1 {
		t.Fatalf("invite without email: %+v", i)
	}
	// A failing SMTP still creates the invite, with email_sent = false.
	mailer.fail = true
	i = a.invite("mentor", ws.ID, map[string]any{"email": "other@example.com"})
	if i.EmailSent == nil || *i.EmailSent || i.URL == "" {
		t.Fatalf("invite with SMTP down: %+v", i)
	}

	// Without SMTP, the invite has the link and email_sent = false; without
	// VAPID keys, there is no push.
	b := newTestApp(t)
	ws = b.mentorship("mentor", "Turma")
	i = b.invite("mentor", ws.ID, map[string]any{"email": "x@example.com"})
	if i.EmailSent == nil || *i.EmailSent || i.URL == "" {
		t.Fatalf("invite without SMTP: %+v", i)
	}
	if p := b.notificationPreferences("ana"); p.PushPublicKey != nil {
		t.Fatalf("push without keys: %+v", p)
	}
	b.mustFail("ana", http.MethodPost, "/v1/me/push", pushSubscription("https://fcm.googleapis.com/x"), http.StatusServiceUnavailable, "push_unavailable")
}

// Leak through the API: another member of the workspace does not see nor
// read someone else's notifications, and a non-member gets 404.
func TestNotificationLeakAPI(t *testing.T) {
	a := newTestApp(t)
	ws := a.mentorship("mentor", "Turma")
	mentor := a.me("mentor")
	a.join("mentor", "bia", ws.ID)
	a.me("intruder")
	a.notify(domain.Notice{WorkspaceID: ws.ID, UserIDs: []uuid.UUID{mentor.ID}, Kind: "test", Key: "k", Title: "Só do mentor"})
	a.deliverNotifications()
	n := a.inbox("mentor", ws.ID).Notifications[0]

	if in := a.inbox("bia", ws.ID); len(in.Notifications) != 0 || in.Unread != 0 {
		t.Fatalf("bia sees the mentor's notifications: %+v", in)
	}
	a.mustFail("bia", http.MethodPost, wsPath(ws.ID, "/notifications/"+n.ID.String()+"/read"), nil, http.StatusNotFound, "notification_not_found")
	a.must("bia", http.MethodPost, wsPath(ws.ID, "/notifications/read"), nil, nil, http.StatusNoContent)
	// From another workspace of the owner, the id does not exist either.
	a.mustFail("mentor", http.MethodPost, wsPath(a.personal("mentor").ID, "/notifications/"+n.ID.String()+"/read"), nil, http.StatusNotFound, "notification_not_found")
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, wsPath(ws.ID, "/notifications")},
		{http.MethodPost, wsPath(ws.ID, "/notifications/read")},
		{http.MethodPost, wsPath(ws.ID, "/notifications/"+n.ID.String()+"/read")},
	} {
		a.mustFail("intruder", tc.method, tc.path, nil, http.StatusNotFound, "workspace_not_found")
	}
	if in := a.inbox("mentor", ws.ID); in.Unread != 1 {
		t.Fatalf("others read the mentor's notification: %+v", in)
	}
	a.mustFail("", http.MethodGet, "/v1/me/notifications", nil, http.StatusUnauthorized, "unauthenticated")
}

// Leak in the database: inboxes are per user and workspace, subscriptions
// and preferences per user, even for queries without a filter.
func TestNotificationLeakRLS(t *testing.T) {
	a := newTestApp(t, withNotificationChannels(&fakeMailer{}, &fakePush{}))
	ctx := context.Background()
	ws := a.mentorship("mentor", "Turma")
	mentor := a.me("mentor")
	bia := a.join("mentor", "bia", ws.ID)
	a.notify(domain.Notice{WorkspaceID: ws.ID, UserIDs: []uuid.UUID{mentor.ID}, Kind: "test", Key: "k", Title: "Oi"})
	a.deliverNotifications()
	a.must("mentor", http.MethodPost, "/v1/me/push", pushSubscription("https://fcm.googleapis.com/fcm/send/m"), nil, http.StatusNoContent)
	a.must("mentor", http.MethodPut, "/v1/me/notifications", map[string]any{"email": false}, nil, http.StatusOK)

	count := func(s database.Scope, table string) int {
		t.Helper()
		var n int
		err := database.InTx(ctx, a.pool, s, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&n)
		})
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	biaInWs := database.Scope{UserID: bia.ID.String(), WorkspaceID: ws.ID.String()}
	mentorElsewhere := database.Scope{UserID: mentor.ID.String(), WorkspaceID: a.personal("mentor").ID.String()}
	for _, table := range []string{"notifications", "push_subscriptions", "notification_preferences"} {
		if n := count(biaInWs, table); n != 0 {
			t.Errorf("%s visible to another member = %d", table, n)
		}
		if n := count(database.Scope{}, table); n != 0 {
			t.Errorf("%s visible without scope = %d", table, n)
		}
	}
	if n := count(mentorElsewhere, "notifications"); n != 0 {
		t.Errorf("notifications visible from another workspace = %d", n)
	}
	mentorInWs := database.Scope{UserID: mentor.ID.String(), WorkspaceID: ws.ID.String()}
	if n := count(mentorInWs, "notifications"); n != 1 {
		t.Errorf("mentor's own notifications = %d, want 1", n)
	}

	writes := map[string]func(pgx.Tx) error{
		"notification for someone else": func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, "INSERT INTO notifications (workspace_id, user_id, kind, key, title) VALUES ($1, $2, 'x', 'x', 'x')", ws.ID, mentor.ID)
			return err
		},
		"notification in another workspace": func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, "INSERT INTO notifications (workspace_id, user_id, kind, key, title) VALUES ($1, $2, 'x', 'x', 'x')", a.personal("mentor").ID, bia.ID)
			return err
		},
		"push subscription for someone else": func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, "INSERT INTO push_subscriptions (user_id, endpoint, p256dh, auth) VALUES ($1, 'https://fcm.googleapis.com/x', 'k', 'a')", mentor.ID)
			return err
		},
		"preferences for someone else": func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, "INSERT INTO notification_preferences (user_id, email) VALUES ($1, true)", mentor.ID)
			return err
		},
	}
	for name, write := range writes {
		if err := database.InTx(ctx, a.pool, biaInWs, write); !isRLSViolation(err) {
			t.Errorf("%s: err = %v, want an RLS violation", name, err)
		}
	}

	err := database.InTx(ctx, a.pool, biaInWs, func(tx pgx.Tx) error {
		for _, sql := range []string{
			"UPDATE notifications SET read_at = now() WHERE user_id = $1",
			"DELETE FROM push_subscriptions WHERE user_id = $1",
			"UPDATE notification_preferences SET email = true WHERE user_id = $1",
		} {
			tag, err := tx.Exec(ctx, sql, mentor.ID)
			if err != nil {
				return err
			}
			if tag.RowsAffected() != 0 {
				return errors.New(sql + ": changed rows of another user")
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
