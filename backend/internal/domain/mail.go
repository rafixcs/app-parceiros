package domain

import "context"

// Email is a message with plain text and HTML bodies.
type Email struct {
	To      string
	Subject string
	Text    string
	HTML    string
}

// Mailer sends emails (SMTP in production).
type Mailer interface {
	Send(ctx context.Context, e Email) error
}
