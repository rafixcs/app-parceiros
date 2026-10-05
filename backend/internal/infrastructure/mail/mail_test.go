package mail

import (
	"bytes"
	"context"
	"io"
	"mime"
	"mime/multipart"
	"net/mail"
	"strings"
	"testing"
	"time"

	"github.com/rafixcs/app-parceiros/backend/internal/domain"
)

func TestBuildMessage(t *testing.T) {
	from, _ := mail.ParseAddress("App Parceiros <nao-responda@parceiros.local>")
	to, _ := mail.ParseAddress("ana@example.com")
	e := Render(to.Address, "Nova lista: Achados de verão", Content{
		Title: "Nova lista: Achados de verão", Body: "Turma <b>do mestre</b> publicou 3 produtos.",
		Link: "https://app.test/w/1/listas/2", Button: "Abrir no app", Footer: "Rodapé",
	})
	b, err := buildMessage(from, to, e, time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	parts := readMessage(t, b)
	if parts["subject"] != "Nova lista: Achados de verão" {
		t.Fatalf("subject %q", parts["subject"])
	}
	if !strings.Contains(parts["text/plain"], "Abrir no app: https://app.test/w/1/listas/2") {
		t.Fatalf("text: %q", parts["text/plain"])
	}
	// The HTML escapes what came from users.
	if !strings.Contains(parts["text/html"], "Turma &lt;b&gt;do mestre&lt;/b&gt;") || strings.Contains(parts["text/html"], "<b>do") {
		t.Fatalf("html: %q", parts["text/html"])
	}
}

// readMessage returns the decoded subject and the body of each part.
func readMessage(t *testing.T, b []byte) map[string]string {
	t.Helper()
	m, err := mail.ReadMessage(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	out["subject"], err = new(mime.WordDecoder).DecodeHeader(m.Header.Get("Subject"))
	if err != nil {
		t.Fatal(err)
	}
	_, params, err := mime.ParseMediaType(m.Header.Get("Content-Type"))
	if err != nil {
		t.Fatal(err)
	}
	r := multipart.NewReader(m.Body, params["boundary"])
	for {
		p, err := r.NextPart() // decodes the quoted-printable
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(p)
		kind, _, _ := mime.ParseMediaType(p.Header.Get("Content-Type"))
		out[kind] = string(body)
	}
	return out
}

type outbox struct{ sent []domain.Email }

func (o *outbox) Send(_ context.Context, e domain.Email) error {
	o.sent = append(o.sent, e)
	return nil
}

func TestAuthMailer(t *testing.T) {
	box := &outbox{}
	m := AuthMailer{Mailer: box, AppURL: "https://app.test/"}
	ctx := context.Background()
	if err := m.SendEmailVerification(ctx, "ana@example.com", "Ana", "tok+1"); err != nil {
		t.Fatal(err)
	}
	expires := time.Date(2026, 10, 5, 15, 0, 0, 0, time.UTC)
	if err := m.SendPasswordReset(ctx, "ana@example.com", "Ana", "tok2", expires); err != nil {
		t.Fatal(err)
	}
	if len(box.sent) != 2 {
		t.Fatalf("sent %d emails", len(box.sent))
	}
	if !strings.Contains(box.sent[0].Text, "https://app.test/verificar-email?token=tok%2B1") {
		t.Fatalf("verification link: %q", box.sent[0].Text)
	}
	if !strings.Contains(box.sent[1].Text, "https://app.test/redefinir-senha?token=tok2") ||
		!strings.Contains(box.sent[1].Text, "05/10/2026 12:00") {
		t.Fatalf("reset email: %q", box.sent[1].Text)
	}
}
