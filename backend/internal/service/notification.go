package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/rafixcs/app-parceiros/backend/internal/domain"
)

const (
	notificationInboxSize     = 50
	maxPushSubscriptions      = 20
	notificationInviteTimeout = 10 * time.Second
)

// notificationContacts gives the recipient's name and email (implemented by
// AccountService).
type notificationContacts interface {
	Contact(ctx context.Context, userID uuid.UUID) (domain.Contact, error)
}

// NotificationService delivers notices to the users: the app inbox (per
// workspace), email and the browser's Web Push.
//
// Other modules call Notify, which enqueues one deliver_notification job per
// recipient. The worker runs Deliver: it records the notification with the
// recipient's scope and sends the email and the push, without repeating what
// was already sent when the job is retried.
type NotificationService struct {
	repo     domain.NotificationRepository
	tx       domain.Transactor
	queue    domain.NotificationQueue
	contacts notificationContacts
	mail     domain.NotificationMailer // nil: no email
	push     domain.PushSender         // nil: no Web Push
	log      *slog.Logger
}

var (
	_ domain.Notifier     = (*NotificationService)(nil)
	_ domain.InviteMailer = (*NotificationService)(nil)
)

// NewNotificationService builds the service. mail and push may be nil (the
// channel is off); queue may be nil in the worker, which does not enqueue.
func NewNotificationService(repo domain.NotificationRepository, tx domain.Transactor, queue domain.NotificationQueue,
	contacts notificationContacts, mail domain.NotificationMailer, push domain.PushSender, log *slog.Logger,
) *NotificationService {
	return &NotificationService{repo: repo, tx: tx, queue: queue, contacts: contacts, mail: mail, push: push, log: log}
}

// Notify enqueues the delivery for each recipient.
func (s *NotificationService) Notify(ctx context.Context, n domain.Notice) error {
	if len(n.UserIDs) == 0 {
		return nil
	}
	if s.queue == nil {
		return errors.New("notifications: no queue to enqueue the deliveries")
	}
	ds := make([]domain.NotificationDelivery, len(n.UserIDs))
	for i, u := range n.UserIDs {
		ds[i] = domain.NotificationDelivery{
			WorkspaceID: n.WorkspaceID, UserID: u, Kind: n.Kind, Key: n.Key,
			Title: truncateNotification(n.Title, domain.MaxNotificationTitle), Body: truncateNotification(n.Body, domain.MaxNotificationBody), URL: n.URL,
		}
	}
	return s.queue.EnqueueNotifications(ctx, ds...)
}

// Inbox returns the latest notifications of the member in the workspace.
func (s *NotificationService) Inbox(ctx context.Context, m domain.Member) (domain.Inbox, error) {
	ns, err := s.repo.Inbox(ctx, m.Actor(), notificationInboxSize)
	if err != nil {
		return domain.Inbox{}, err
	}
	unread, err := s.repo.CountUnread(ctx, m.Actor())
	if err != nil {
		return domain.Inbox{}, err
	}
	if ns == nil {
		ns = []domain.Notification{}
	}
	return domain.Inbox{Notifications: ns, Unread: unread}, nil
}

// MarkRead marks one notification of the member's inbox as read.
func (s *NotificationService) MarkRead(ctx context.Context, m domain.Member, id uuid.UUID) error {
	err := s.repo.MarkRead(ctx, m.Actor(), id)
	if errors.Is(err, domain.ErrNotFound) {
		return domain.ErrNotificationNotFound
	}
	return err
}

// MarkAllRead marks every notification of the member's inbox as read.
func (s *NotificationService) MarkAllRead(ctx context.Context, m domain.Member) error {
	return s.repo.MarkAllRead(ctx, m.Actor())
}

// Preferences of the user. Without a stored preference, email is on.
func (s *NotificationService) Preferences(ctx context.Context, userID uuid.UUID) (domain.NotificationPreferences, error) {
	var out domain.NotificationPreferences
	if s.push != nil {
		k := s.push.PublicKey()
		out.PushPublicKey = &k
	}
	var err error
	if out.Email, err = s.repo.EmailEnabled(ctx, userID); err != nil {
		return domain.NotificationPreferences{}, err
	}
	if out.PushSubscriptions, err = s.repo.CountPushSubscriptions(ctx, userID); err != nil {
		return domain.NotificationPreferences{}, err
	}
	return out, nil
}

// SetEmail turns the notification emails on or off. email is required.
func (s *NotificationService) SetEmail(ctx context.Context, userID uuid.UUID, email *bool) (domain.NotificationPreferences, error) {
	if email == nil {
		return domain.NotificationPreferences{}, domain.ErrInvalidNotificationPreferences
	}
	if err := s.repo.SetEmailEnabled(ctx, userID, *email); err != nil {
		return domain.NotificationPreferences{}, err
	}
	return s.Preferences(ctx, userID)
}

// Subscribe stores the Web Push subscription of a browser. Only endpoints of
// the known push services are accepted, since the worker POSTs to them.
func (s *NotificationService) Subscribe(ctx context.Context, userID uuid.UUID, sub domain.PushSubscription) error {
	if s.push == nil {
		return domain.ErrPushUnavailable
	}
	if !domain.PushEndpointAllowed(sub.Endpoint) || sub.P256dh == "" || sub.Auth == "" ||
		len(sub.P256dh) > domain.MaxPushP256dh || len(sub.Auth) > domain.MaxPushAuth {
		return domain.ErrInvalidPushSubscription
	}
	return s.tx.WithinTx(ctx, func(ctx context.Context) error {
		n, err := s.repo.CountPushSubscriptions(ctx, userID)
		if err != nil {
			return err
		}
		if n >= maxPushSubscriptions {
			// Subscribing a browser that is already subscribed still works.
			deleted, err := s.repo.DeletePushSubscription(ctx, userID, sub.Endpoint)
			if err != nil {
				return err
			}
			if !deleted {
				return domain.ErrTooManyPushSubscriptions
			}
		}
		return s.repo.SavePushSubscription(ctx, userID, sub)
	})
}

// Unsubscribe deletes the browser's subscription. A missing one is not an
// error.
func (s *NotificationService) Unsubscribe(ctx context.Context, userID uuid.UUID, endpoint string) error {
	_, err := s.repo.DeletePushSubscription(ctx, userID, endpoint)
	return err
}

// SendInvite emails an invite to a mentorship. AccountService calls it when
// the invite is created (SendInvitesWith): the token does not go through the
// queue, which would keep the link in plain text in the database. Without
// email it fails, so the invite reports the email as not sent.
func (s *NotificationService) SendInvite(ctx context.Context, e domain.InviteEmail) error {
	if s.mail == nil {
		return errors.New("notifications: email is off")
	}
	ctx, cancel := context.WithTimeout(ctx, notificationInviteTimeout)
	defer cancel()
	err := s.mail.SendInvite(ctx, e)
	if err != nil {
		s.log.WarnContext(ctx, "could not send the invite email", "err", err)
	}
	return err
}

// Deliver records the notification in the recipient's inbox and sends the
// email and the push, each only once.
func (s *NotificationService) Deliver(ctx context.Context, d domain.NotificationDelivery) error {
	d.Title = truncateNotification(d.Title, domain.MaxNotificationTitle)
	d.Body = truncateNotification(d.Body, domain.MaxNotificationBody)
	n, err := s.repo.SaveNotification(ctx, d)
	if err != nil {
		return err
	}
	if n.EmailedAt == nil && s.mail != nil {
		if err := s.deliverEmail(ctx, n); err != nil {
			return err
		}
	}
	if n.PushedAt == nil && s.push != nil {
		if err := s.deliverPush(ctx, n); err != nil {
			return err
		}
	}
	return nil
}

func (s *NotificationService) deliverEmail(ctx context.Context, n domain.Notification) error {
	enabled, err := s.repo.EmailEnabled(ctx, n.UserID)
	if err != nil {
		return err
	}
	if enabled {
		c, err := s.contacts.Contact(ctx, n.UserID)
		if err != nil {
			return err
		}
		// An unverified email may belong to someone else: it gets nothing.
		if c.EmailVerified && c.Email != "" {
			if err := s.mail.SendNotification(ctx, c.Email, n); err != nil {
				return fmt.Errorf("sending email: %w", err)
			}
		}
	}
	return s.repo.MarkEmailed(ctx, notificationRecipient(n), n.ID)
}

// deliverPush sends to each subscribed browser. Subscriptions the push
// service reported as expired are deleted; other failures only go to the
// log, so the browsers that already got it do not get it again.
func (s *NotificationService) deliverPush(ctx context.Context, n domain.Notification) error {
	subs, err := s.repo.PushSubscriptions(ctx, n.UserID)
	if err != nil {
		return err
	}
	msg := domain.PushMessage{ID: n.ID, Title: n.Title, Body: n.Body, URL: n.URL}
	for _, sub := range subs {
		err := s.push.Send(ctx, sub, msg)
		switch {
		case errors.Is(err, domain.ErrPushSubscriptionExpired):
			if err := s.Unsubscribe(ctx, n.UserID, sub.Endpoint); err != nil {
				return err
			}
		case err != nil:
			s.log.WarnContext(ctx, "push not delivered", "notification_id", n.ID, "err", err)
		}
	}
	return s.repo.MarkPushed(ctx, notificationRecipient(n), n.ID)
}

func notificationRecipient(n domain.Notification) domain.Actor {
	return domain.Actor{UserID: n.UserID, WorkspaceID: n.WorkspaceID}
}

// truncateNotification cuts s to limit runes, ending with an ellipsis when cut.
func truncateNotification(s string, limit int) string {
	if utf8.RuneCountInString(s) <= limit {
		return s
	}
	return string([]rune(s)[:limit-1]) + "…"
}
