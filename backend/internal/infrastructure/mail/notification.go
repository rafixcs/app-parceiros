package mail

import (
	"context"
	"strings"

	"github.com/rafixcs/app-parceiros/backend/internal/domain"
)

// NotificationMailer writes the notification and invite emails, in pt-BR,
// with links to the web app.
type NotificationMailer struct {
	Mailer domain.Mailer
	AppURL string // e.g. https://app.example.com
}

var _ domain.NotificationMailer = NotificationMailer{}

// SendNotification emails a notification; its URL is a path of the app.
func (m NotificationMailer) SendNotification(ctx context.Context, to string, n domain.Notification) error {
	return m.Mailer.Send(ctx, Render(to, n.Title, Content{
		Title:  n.Title,
		Body:   n.Body,
		Link:   strings.TrimRight(m.AppURL, "/") + n.URL,
		Button: "Abrir no app",
		Footer: "Você recebeu este e-mail porque participa de uma mentoria no App Parceiros. " +
			"Para não receber mais, desligue os e-mails em Notificações, no app.",
	}))
}

// SendInvite emails the link of an invite to a mentorship.
func (m NotificationMailer) SendInvite(ctx context.Context, e domain.InviteEmail) error {
	title := "Convite para a mentoria " + e.WorkspaceName
	return m.Mailer.Send(ctx, Render(e.Email, title, Content{
		Title:  title,
		Body:   "Entre no App Parceiros para receber as listas de produtos da mentoria com o seu link de afiliado.",
		Link:   e.URL,
		Button: "Aceitar convite",
		Footer: "O convite vale até " + e.ExpiresAt.In(BrasiliaTime).Format("02/01/2006 15:04") +
			" (horário de Brasília). Se você não esperava este e-mail, ignore-o.",
	}))
}
