package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/rafixcs/app-parceiros/backend/internal/domain"
)

// billingNoticeKind is the kind of the notifications about payments.
const billingNoticeKind = "billing"

// billingAccounts is what billing uses from the accounts: the workspace, the
// plan limits and the access (access_until and seats bought).
type billingAccounts interface {
	Workspace(ctx context.Context, m domain.Member) (domain.WorkspaceView, error)
	PlanLimit(ctx context.Context, m domain.Member, key string) (int64, error)
	SeatsInUse(ctx context.Context, m domain.Member) (int64, error)
	CheckSeats(ctx context.Context, m domain.Member, n int64) error
	SetSeats(ctx context.Context, m domain.Member, n int32) error
	GrantAccess(ctx context.Context, workspaceID uuid.UUID, until time.Time, seats *int32) error
	RevokeAccess(ctx context.Context, workspaceID uuid.UUID) error
	Contact(ctx context.Context, userID uuid.UUID) (domain.Contact, error)
}

// BillingService handles what a workspace pays: the plan (solo per
// workspace, mentorship per seat), the checkout at the payment gateway and the
// gateway events that extend or suspend the access. Only the owner pays and
// changes the subscription; owner and mentors see it.
type BillingService struct {
	repo     domain.BillingRepository
	tx       domain.Transactor
	gw       domain.PaymentGateway
	accounts billingAccounts
	// notifier tells the owner about payments. Nil tells nothing.
	notifier domain.Notifier
	log      *slog.Logger
	now      func() time.Time
}

// NewBillingService builds the service. notifier may be nil; a nil gateway
// answers ErrBillingUnavailable to every call that needs it.
func NewBillingService(repo domain.BillingRepository, tx domain.Transactor, gw domain.PaymentGateway,
	accounts billingAccounts, notifier domain.Notifier, log *slog.Logger) *BillingService {
	return &BillingService{repo: repo, tx: tx, gw: gw, accounts: accounts, notifier: notifier, log: log, now: time.Now}
}

// NewSubscriptionRequest is the checkout request.
type NewSubscriptionRequest struct {
	// Seats asked for in a mentorship. Ignored in the solo plan.
	Seats int64
	// TaxID (CPF or CNPJ) of the payer, required by the gateway.
	TaxID string
}

// View returns the subscription and the numbers of the plan. Owner and
// mentors see it.
func (s *BillingService) View(ctx context.Context, m domain.Member) (domain.SubscriptionView, error) {
	if !m.Role.Manages() {
		return domain.SubscriptionView{}, domain.ErrBillingManagersOnly
	}
	w, err := s.accounts.Workspace(ctx, m)
	if err != nil {
		return domain.SubscriptionView{}, err
	}
	out := domain.SubscriptionView{
		Plan:         w.Plan,
		AccessStatus: w.Status,
		AccessUntil:  w.AccessUntil,
		Status:       domain.SubscriptionNone,
		Simulated:    s.simulated(),
	}
	if out.PriceCents, err = s.price(ctx, m, w.Workspace); err != nil {
		return domain.SubscriptionView{}, err
	}
	if w.Kind == domain.WorkspaceMentorship {
		if out.SeatsInUse, err = s.accounts.SeatsInUse(ctx, m); err != nil {
			return domain.SubscriptionView{}, err
		}
		if out.MaxSeats, err = s.accounts.PlanLimit(ctx, m, domain.LimitSeats); err != nil {
			return domain.SubscriptionView{}, err
		}
	} else {
		out.MaxSeats = 1
	}
	if w.Seats != nil {
		out.Seats = int64(*w.Seats)
	}

	sub, err := s.repo.Subscription(ctx, m.Actor())
	if errors.Is(err, domain.ErrNotFound) {
		return out, nil
	}
	if err != nil {
		return domain.SubscriptionView{}, err
	}
	out.Status = sub.Status
	out.Provider = sub.Provider
	out.AmountCents = sub.AmountCents
	if sub.CancelledAt == nil {
		out.Seats = int64(sub.Seats)
	}
	if !sub.NextDueDate.IsZero() {
		d := sub.NextDueDate
		out.NextDueDate = &d
	}
	if sub.PaymentURL != nil && *sub.PaymentURL != "" {
		out.PaymentURL = sub.PaymentURL
	}
	return out, nil
}

// price is the monthly price of the plan: of the workspace in the solo plan,
// of each seat in the mentorship.
func (s *BillingService) price(ctx context.Context, m domain.Member, w domain.Workspace) (int64, error) {
	key := domain.LimitPriceCents
	if w.Kind == domain.WorkspaceMentorship {
		key = domain.LimitSeatPriceCents
	}
	return s.accounts.PlanLimit(ctx, m, key)
}

// current returns the subscription in progress (not cancelled), or
// ErrNoSubscription.
func (s *BillingService) current(ctx context.Context, m domain.Member) (domain.Subscription, error) {
	sub, err := s.repo.Subscription(ctx, m.Actor())
	if errors.Is(err, domain.ErrNotFound) || (err == nil && sub.CancelledAt != nil) {
		return domain.Subscription{}, domain.ErrNoSubscription
	}
	return sub, err
}

// Subscribe buys the plan and returns the subscription with the open invoice.
// Only the owner subscribes. The access is only granted when the payment is
// confirmed (by the gateway webhook).
func (s *BillingService) Subscribe(ctx context.Context, m domain.Member, n NewSubscriptionRequest) (domain.SubscriptionView, error) {
	if m.Role != domain.RoleOwner {
		return domain.SubscriptionView{}, domain.ErrBillingOwnerOnly
	}
	taxID, err := domain.TaxID(n.TaxID)
	if err != nil {
		return domain.SubscriptionView{}, err
	}
	if s.gw == nil {
		return domain.SubscriptionView{}, domain.ErrBillingUnavailable
	}
	w, err := s.accounts.Workspace(ctx, m)
	if err != nil {
		return domain.SubscriptionView{}, err
	}
	seats, amount, err := s.amount(ctx, m, w.Workspace, n.Seats)
	if err != nil {
		return domain.SubscriptionView{}, err
	}
	// A subscription in progress is not replaced: cancelling first avoids two
	// charges at the gateway.
	if _, err := s.current(ctx, m); err == nil {
		return domain.SubscriptionView{}, domain.ErrAlreadySubscribed
	} else if !errors.Is(err, domain.ErrNoSubscription) {
		return domain.SubscriptionView{}, err
	}

	contact, err := s.accounts.Contact(ctx, m.UserID)
	if err != nil {
		return domain.SubscriptionView{}, err
	}
	due := s.now().Add(domain.FirstDueIn)
	ext, err := s.gw.Create(ctx, domain.NewExternalSubscription{
		WorkspaceID: m.WorkspaceID,
		Description: billingDescription(w.Workspace, seats),
		AmountCents: amount,
		DueDate:     due,
		Name:        contact.Name,
		Email:       contact.Email,
		TaxID:       taxID,
	})
	if err != nil {
		s.log.ErrorContext(ctx, "creating the subscription at the gateway failed", "provider", s.gw.Name(), "err", err)
		return domain.SubscriptionView{}, domain.ErrBillingUnavailable
	}
	if ext.NextDueDate.IsZero() {
		ext.NextDueDate = due
	}

	saved, err := s.repo.SaveSubscription(ctx, m.Actor(), domain.NewSubscription{
		Provider:           s.gw.Name(),
		ExternalCustomerID: ext.CustomerID,
		ExternalID:         ext.ID,
		Seats:              int32(seats),
		AmountCents:        amount,
		NextDueDate:        ext.NextDueDate,
		PaymentURL:         billingOptional(ext.PaymentURL),
		CreatedBy:          m.UserID,
	})
	if err != nil {
		return domain.SubscriptionView{}, err
	}
	if !saved {
		// Another request subscribed meanwhile: undo the one made here.
		if err := s.gw.Cancel(ctx, ext.ID); err != nil {
			s.log.ErrorContext(ctx, "duplicate subscription at the gateway, cancelling it failed",
				"provider", s.gw.Name(), "external_id", ext.ID, "err", err)
		}
		return domain.SubscriptionView{}, domain.ErrAlreadySubscribed
	}
	return s.View(ctx, m)
}

// ChangeSeats adjusts the seats of the mentorship. Fewer seats apply at once
// (never below the seats in use); more seats apply when the next payment is
// confirmed, with the new amount already in the open charge.
func (s *BillingService) ChangeSeats(ctx context.Context, m domain.Member, seats int64) (domain.SubscriptionView, error) {
	if m.Role != domain.RoleOwner {
		return domain.SubscriptionView{}, domain.ErrBillingOwnerOnly
	}
	if s.gw == nil {
		return domain.SubscriptionView{}, domain.ErrBillingUnavailable
	}
	w, err := s.accounts.Workspace(ctx, m)
	if err != nil {
		return domain.SubscriptionView{}, err
	}
	n, amount, err := s.amount(ctx, m, w.Workspace, seats)
	if err != nil {
		return domain.SubscriptionView{}, err
	}
	sub, err := s.current(ctx, m)
	if err != nil {
		return domain.SubscriptionView{}, err
	}
	if err := s.gw.ChangeAmount(ctx, sub.ExternalID, amount); err != nil {
		s.log.ErrorContext(ctx, "changing the amount at the gateway failed", "provider", s.gw.Name(), "err", err)
		return domain.SubscriptionView{}, domain.ErrBillingUnavailable
	}
	err = s.repo.UpdateSubscriptionPlan(ctx, m.Actor(), int32(n), amount)
	if errors.Is(err, domain.ErrNotFound) {
		return domain.SubscriptionView{}, domain.ErrNoSubscription
	}
	if err != nil {
		return domain.SubscriptionView{}, err
	}
	// Fewer seats apply now; more seats, only with the payment.
	if w.Seats != nil && n < int64(*w.Seats) {
		if err := s.accounts.SetSeats(ctx, m, int32(n)); err != nil {
			return domain.SubscriptionView{}, err
		}
	}
	return s.View(ctx, m)
}

// Cancel ends the subscription at the gateway. The access goes on until the
// end of the period already paid.
func (s *BillingService) Cancel(ctx context.Context, m domain.Member) (domain.SubscriptionView, error) {
	if m.Role != domain.RoleOwner {
		return domain.SubscriptionView{}, domain.ErrBillingOwnerOnly
	}
	sub, err := s.current(ctx, m)
	if err != nil {
		return domain.SubscriptionView{}, err
	}
	if s.gw == nil {
		return domain.SubscriptionView{}, domain.ErrBillingUnavailable
	}
	if err := s.gw.Cancel(ctx, sub.ExternalID); err != nil {
		s.log.ErrorContext(ctx, "cancelling at the gateway failed", "provider", s.gw.Name(), "err", err)
		return domain.SubscriptionView{}, domain.ErrBillingUnavailable
	}
	if err := s.repo.CancelSubscription(ctx, m.Actor()); err != nil && !errors.Is(err, domain.ErrNotFound) {
		return domain.SubscriptionView{}, err
	}
	return s.View(ctx, m)
}

// amount checks the seats asked for and returns how many and the monthly
// amount.
func (s *BillingService) amount(ctx context.Context, m domain.Member, w domain.Workspace, seats int64) (int64, int64, error) {
	price, err := s.price(ctx, m, w)
	if err != nil {
		return 0, 0, err
	}
	if price <= 0 {
		return 0, 0, domain.ErrNoPrice
	}
	if w.Kind != domain.WorkspaceMentorship {
		return 1, price, nil
	}
	if seats < 1 {
		return 0, 0, domain.ErrMinimumSeats
	}
	if err := s.accounts.CheckSeats(ctx, m, seats); err != nil {
		return 0, 0, err
	}
	return seats, seats * price, nil
}

// billingDescription is the text of the charge at the gateway, seen by the
// payer (pt-BR).
func billingDescription(w domain.Workspace, seats int64) string {
	if w.Kind == domain.WorkspaceMentorship {
		return fmt.Sprintf("App Parceiros · Mentoria %s · %d assentos", w.Name, seats)
	}
	return "App Parceiros · Plano avulso"
}

// Webhook checks and processes a gateway event. Events that do not matter
// and events of subscriptions that are not ours return nil: there is nothing
// to do and the gateway must not redeliver them. ErrInvalidBillingWebhook is
// a request that did not come from the gateway.
func (s *BillingService) Webhook(ctx context.Context, h domain.HeaderReader, body []byte) error {
	if s.gw == nil {
		return domain.ErrBillingUnavailable
	}
	e, err := s.gw.ParseEvent(h, body)
	if errors.Is(err, domain.ErrBillingEventIgnored) {
		return nil
	}
	if err != nil {
		return err
	}
	err = s.Process(ctx, e)
	if errors.Is(err, domain.ErrNoSubscription) {
		s.log.WarnContext(ctx, "billing event of an unknown subscription", "provider", e.Provider)
		return nil
	}
	return err
}

// Process applies a billing event: it extends or suspends the access and
// stores the status of the subscription. Redeliveries of the same event do
// nothing. It returns ErrNoSubscription for a subscription that is not ours.
func (s *BillingService) Process(ctx context.Context, e domain.BillingEvent) error {
	// The event has no user nor workspace: the subscription is found by its
	// external id.
	sub, err := s.repo.SubscriptionByExternalID(ctx, e.Provider, e.ExternalSubscriptionID)
	if errors.Is(err, domain.ErrNotFound) {
		return domain.ErrNoSubscription
	}
	if err != nil {
		return err
	}

	var applied bool
	// Recording the event, the access and the status go together: if any of
	// them fails, nothing is recorded and the gateway's redelivery retries.
	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		isNew, err := s.repo.RecordEvent(ctx, sub, e)
		if err != nil || !isNew {
			return err
		}
		u := domain.BillingUpdate{Status: sub.Status}
		switch e.Kind {
		case domain.BillingEventPaid:
			u.Status = domain.SubscriptionActive
			// The paid cycle goes from the due date to the next one; the
			// grace period gives the next payment time to arrive.
			due := e.DueDate
			if due.IsZero() {
				due = s.now()
			}
			end := due.Add(domain.BillingCycle)
			u.NextDueDate = &end
			seats := sub.Seats
			if err := s.accounts.GrantAccess(ctx, sub.WorkspaceID, end.Add(domain.GracePeriod), &seats); err != nil {
				return err
			}
			empty := ""
			u.PaymentURL = &empty
		case domain.BillingEventOverdue:
			// The access ends by the date, not by this event.
			u.Status = domain.SubscriptionOverdue
			u.PaymentURL = billingOptional(e.PaymentURL)
		case domain.BillingEventRefunded:
			u.Status = domain.SubscriptionOverdue
			if err := s.accounts.RevokeAccess(ctx, sub.WorkspaceID); err != nil {
				return err
			}
		case domain.BillingEventCancelled:
			u.Status = domain.SubscriptionCancelled
		case domain.BillingEventCharge:
			u.PaymentURL = billingOptional(e.PaymentURL)
			if !e.DueDate.IsZero() {
				d := e.DueDate
				u.NextDueDate = &d
			}
		}
		if err := s.repo.UpdateBilling(ctx, sub, u); err != nil {
			return err
		}
		applied = true
		return nil
	})
	if err != nil || !applied {
		return err
	}
	s.notify(ctx, sub, e)
	return nil
}

// notify tells the owner about the payment. A failure here does not undo the
// event: the payment was already applied.
func (s *BillingService) notify(ctx context.Context, sub domain.Subscription, e domain.BillingEvent) {
	if s.notifier == nil {
		return
	}
	var title, body string
	switch e.Kind {
	case domain.BillingEventPaid:
		title = "Pagamento confirmado"
		body = "A assinatura do seu workspace está ativa."
	case domain.BillingEventOverdue:
		title = "Pagamento em atraso"
		body = "A cobrança da assinatura venceu. Pague para o workspace não ser suspenso."
	case domain.BillingEventRefunded:
		title = "Pagamento estornado"
		body = "O pagamento da assinatura foi estornado e o workspace está suspenso."
	default:
		return
	}
	n := domain.Notice{
		WorkspaceID: sub.WorkspaceID, UserIDs: []uuid.UUID{sub.CreatedBy}, Kind: billingNoticeKind,
		Key: "billing:" + e.ID, Title: title, Body: body,
		URL: "/w/" + sub.WorkspaceID.String() + "/assinatura",
	}
	if err := s.notifier.Notify(ctx, n); err != nil {
		s.log.ErrorContext(ctx, "notifying about the payment failed", "err", err)
	}
}

// SimulatePayment confirms the open payment without a real gateway. It only
// exists with the local mock gateway.
func (s *BillingService) SimulatePayment(ctx context.Context, m domain.Member) (domain.SubscriptionView, error) {
	if !s.simulated() {
		return domain.SubscriptionView{}, domain.ErrSimulationOnlyLocal
	}
	if m.Role != domain.RoleOwner {
		return domain.SubscriptionView{}, domain.ErrBillingOwnerOnly
	}
	sub, err := s.current(ctx, m)
	if err != nil {
		return domain.SubscriptionView{}, err
	}
	due := s.now()
	if !sub.NextDueDate.IsZero() {
		due = sub.NextDueDate
	}
	err = s.Process(ctx, domain.BillingEvent{
		ID: "mock_" + uuid.NewString(), Provider: s.gw.Name(), Kind: domain.BillingEventPaid,
		ExternalSubscriptionID: sub.ExternalID, DueDate: due,
	})
	if err != nil {
		return domain.SubscriptionView{}, err
	}
	return s.View(ctx, m)
}

func (s *BillingService) simulated() bool { return s.gw != nil && s.gw.Simulated() }

func billingOptional(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}
