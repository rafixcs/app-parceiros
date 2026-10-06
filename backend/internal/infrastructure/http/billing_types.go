package http

import (
	"time"

	"github.com/rafixcs/app-parceiros/backend/internal/domain"
)

type subscribeRequest struct {
	// Seats asked for in a mentorship; ignored in the solo plan.
	Seats int64 `json:"seats"`
	// TaxID is the CPF or CNPJ of the payer, with or without punctuation.
	TaxID string `json:"tax_id"`
}

type changeSeatsRequest struct {
	Seats int64 `json:"seats"`
}

type subscriptionResponse struct {
	Plan string `json:"plan"`
	// AccessStatus of the workspace: trial, active, free or suspended.
	AccessStatus string    `json:"access_status"`
	AccessUntil  time.Time `json:"access_until"`
	// Status of the subscription: none, pending, active, overdue or
	// cancelled.
	Status string `json:"status"`
	// Provider of the gateway, empty without a subscription.
	Provider   string `json:"provider"`
	PriceCents int64  `json:"price_cents"`
	Seats      int64  `json:"seats"`
	SeatsInUse int64  `json:"seats_in_use"`
	MaxSeats   int64  `json:"max_seats"`
	// AmountCents is the monthly total (0 without a subscription).
	AmountCents int64 `json:"amount_cents"`
	// NextDueDate is a date (YYYY-MM-DD) or null.
	NextDueDate *string `json:"next_due_date"`
	PaymentURL  *string `json:"payment_url"`
	Simulated   bool    `json:"simulated"`
}

func subscriptionResponseOf(v domain.SubscriptionView) subscriptionResponse {
	out := subscriptionResponse{
		Plan: v.Plan, AccessStatus: string(v.AccessStatus), AccessUntil: v.AccessUntil, Status: string(v.Status),
		Provider: v.Provider, PriceCents: v.PriceCents, Seats: v.Seats, SeatsInUse: v.SeatsInUse,
		MaxSeats: v.MaxSeats, AmountCents: v.AmountCents, PaymentURL: v.PaymentURL, Simulated: v.Simulated,
	}
	if v.NextDueDate != nil {
		d := v.NextDueDate.Format(time.DateOnly)
		out.NextDueDate = &d
	}
	return out
}
