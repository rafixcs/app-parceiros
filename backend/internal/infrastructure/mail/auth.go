package mail

import (
	"context"
	"net/url"
	"strings"
	"time"

	"github.com/rafixcs/app-parceiros/backend/internal/domain"
)

// AuthMailer writes the emails of the internal identity provider, in pt-BR,
// with links to the web app.
type AuthMailer struct {
	Mailer domain.Mailer
	AppURL string // e.g. https://app.example.com
}

var _ domain.AuthMailer = AuthMailer{}

func (m AuthMailer) link(path, token string) string {
	return strings.TrimRight(m.AppURL, "/") + path + "?token=" + url.QueryEscape(token)
}

func (m AuthMailer) SendEmailVerification(ctx context.Context, to, name, token string) error {
	return m.Mailer.Send(ctx, Render(to, "Confirme o seu e-mail", Content{
		Title:  "Olá, " + name + "! Confirme o seu e-mail",
		Body:   "Confirmar o e-mail libera os convites de mentoria e os avisos por e-mail no App Parceiros.",
		Link:   m.link("/verificar-email", token),
		Button: "Confirmar e-mail",
		Footer: "O link vale por 7 dias. Se você não criou uma conta no App Parceiros, ignore este e-mail.",
	}))
}

func (m AuthMailer) SendPasswordReset(ctx context.Context, to, name, token string, expiresAt time.Time) error {
	return m.Mailer.Send(ctx, Render(to, "Redefinir a sua senha", Content{
		Title:  "Olá, " + name + "! Vamos redefinir a sua senha",
		Body:   "Recebemos um pedido para trocar a senha da sua conta no App Parceiros.",
		Link:   m.link("/redefinir-senha", token),
		Button: "Escolher nova senha",
		Footer: "O link vale até " + expiresAt.In(BrasiliaTime).Format("02/01/2006 15:04") +
			" (horário de Brasília). Se você não pediu a troca, ignore este e-mail: a senha atual continua valendo.",
	}))
}
