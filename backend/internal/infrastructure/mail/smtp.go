// Package mail sends emails over SMTP and renders the pt-BR email layout
// shared by every message of the application.
package mail

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"strings"
	"time"

	"github.com/rafixcs/app-parceiros/backend/internal/domain"
)

// SMTP sends through an SMTP server: Mailpit locally, the transactional email
// provider in production. It uses STARTTLS when the server offers it.
type SMTP struct {
	Addr     string // host:port
	Username string
	Password string
	From     string // e.g. "App Parceiros <nao-responda@example.com>"
}

var _ domain.Mailer = SMTP{}

func (s SMTP) Send(ctx context.Context, e domain.Email) error {
	from, err := mail.ParseAddress(s.From)
	if err != nil {
		return fmt.Errorf("invalid SMTP_FROM: %w", err)
	}
	to, err := mail.ParseAddress(e.To)
	if err != nil {
		return fmt.Errorf("invalid recipient: %w", err)
	}
	msg, err := buildMessage(from, to, e, time.Now())
	if err != nil {
		return err
	}

	host, _, err := net.SplitHostPort(s.Addr)
	if err != nil {
		return fmt.Errorf("invalid SMTP_ADDR: %w", err)
	}
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
	}
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", s.Addr)
	if err != nil {
		return err
	}
	if d, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(d)
	}
	c, err := smtp.NewClient(conn, host)
	if err != nil {
		_ = conn.Close()
		return err
	}
	defer func() { _ = c.Close() }()
	if ok, _ := c.Extension("STARTTLS"); ok {
		if err := c.StartTLS(&tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}); err != nil {
			return err
		}
	}
	if s.Username != "" {
		if err := c.Auth(smtp.PlainAuth("", s.Username, s.Password, host)); err != nil {
			return err
		}
	}
	if err := c.Mail(from.Address); err != nil {
		return err
	}
	if err := c.Rcpt(to.Address); err != nil {
		return err
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(msg); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}

// buildMessage builds a UTF-8 multipart/alternative with quoted-printable.
func buildMessage(from, to *mail.Address, e domain.Email, now time.Time) ([]byte, error) {
	boundary := make([]byte, 12)
	if _, err := rand.Read(boundary); err != nil {
		return nil, err
	}
	sep := "parceiros-" + hex.EncodeToString(boundary)
	domainName := from.Address[strings.LastIndex(from.Address, "@")+1:]

	var b bytes.Buffer
	header := func(k, v string) { fmt.Fprintf(&b, "%s: %s\r\n", k, v) }
	header("From", from.String())
	header("To", to.String())
	// A line break in the subject would start a new header.
	subject := strings.Join(strings.Fields(e.Subject), " ")
	header("Subject", mime.QEncoding.Encode("utf-8", subject))
	header("Date", now.Format(time.RFC1123Z))
	header("Message-ID", "<"+hex.EncodeToString(boundary)+"@"+domainName+">")
	header("MIME-Version", "1.0")
	header("Content-Type", `multipart/alternative; boundary="`+sep+`"`)
	b.WriteString("\r\n")
	for _, part := range []struct{ kind, body string }{{"text/plain", e.Text}, {"text/html", e.HTML}} {
		fmt.Fprintf(&b, "--%s\r\nContent-Type: %s; charset=utf-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\n", sep, part.kind)
		qp := quotedprintable.NewWriter(&b)
		if _, err := qp.Write([]byte(strings.ReplaceAll(part.body, "\n", "\r\n"))); err != nil {
			return nil, err
		}
		if err := qp.Close(); err != nil {
			return nil, err
		}
		b.WriteString("\r\n")
	}
	fmt.Fprintf(&b, "--%s--\r\n", sep)
	return b.Bytes(), nil
}
