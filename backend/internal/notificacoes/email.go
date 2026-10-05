package notificacoes

import (
	"github.com/rafixcs/app-parceiros/backend/internal/contas"
	"github.com/rafixcs/app-parceiros/backend/internal/domain"
	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/mail"
	"github.com/rafixcs/app-parceiros/backend/internal/notificacoes/notificacoesdb"
)

func (s *Service) emailNotificacao(c contas.Contato, n notificacoesdb.Notificacao) domain.Email {
	return mail.Render(c.Email, n.Titulo, mail.Content{
		Title: n.Titulo, Body: n.Corpo, Link: s.appURL + n.Url, Button: "Abrir no app",
		Footer: "Você recebeu este e-mail porque participa de uma mentoria no App Parceiros. " +
			"Para não receber mais, desligue os e-mails em Notificações, no app.",
	})
}

func emailConvite(c contas.EnvioConvite) domain.Email {
	titulo := "Convite para a mentoria " + c.WorkspaceNome
	return mail.Render(c.Email, titulo, mail.Content{
		Title: titulo,
		Body:  "Entre no App Parceiros para receber as listas de produtos da mentoria com o seu link de afiliado.",
		Link:  c.URL, Button: "Aceitar convite",
		Footer: "O convite vale até " + c.ExpiraEm.In(mail.BrasiliaTime).Format("02/01/2006 15:04") +
			" (horário de Brasília). Se você não esperava este e-mail, ignore-o.",
	})
}
