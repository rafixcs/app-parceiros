package notificacoes

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"html/template"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"strings"
	"time"

	"github.com/rafixcs/app-parceiros/backend/internal/contas"
	"github.com/rafixcs/app-parceiros/backend/internal/notificacoes/notificacoesdb"
)

// Email é uma mensagem com texto puro e HTML.
type Email struct {
	Para    string
	Assunto string
	Texto   string
	HTML    string
}

// Remetente envia e-mails (em produção, por SMTP).
type Remetente interface {
	Enviar(ctx context.Context, e Email) error
}

// SMTP envia por um servidor SMTP: Mailpit no ambiente local, o provedor de
// e-mail transacional em produção. Usa STARTTLS quando o servidor oferece.
type SMTP struct {
	Addr    string // host:porta
	Usuario string
	Senha   string
	De      string // ex.: "App Parceiros <nao-responda@exemplo.com.br>"
}

func (s SMTP) Enviar(ctx context.Context, e Email) error {
	de, err := mail.ParseAddress(s.De)
	if err != nil {
		return fmt.Errorf("SMTP_REMETENTE inválido: %w", err)
	}
	para, err := mail.ParseAddress(e.Para)
	if err != nil {
		return fmt.Errorf("destinatário inválido: %w", err)
	}
	msg, err := montarMensagem(de, para, e, time.Now())
	if err != nil {
		return err
	}

	host, _, err := net.SplitHostPort(s.Addr)
	if err != nil {
		return fmt.Errorf("SMTP_ADDR inválido: %w", err)
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
	if s.Usuario != "" {
		if err := c.Auth(smtp.PlainAuth("", s.Usuario, s.Senha, host)); err != nil {
			return err
		}
	}
	if err := c.Mail(de.Address); err != nil {
		return err
	}
	if err := c.Rcpt(para.Address); err != nil {
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

// montarMensagem monta um multipart/alternative em UTF-8 com quoted-printable.
func montarMensagem(de, para *mail.Address, e Email, agora time.Time) ([]byte, error) {
	limite := make([]byte, 12)
	if _, err := rand.Read(limite); err != nil {
		return nil, err
	}
	sep := "parceiros-" + hex.EncodeToString(limite)
	dominio := de.Address[strings.LastIndex(de.Address, "@")+1:]

	var b bytes.Buffer
	cab := func(k, v string) { fmt.Fprintf(&b, "%s: %s\r\n", k, v) }
	cab("From", de.String())
	cab("To", para.String())
	cab("Subject", mime.QEncoding.Encode("utf-8", e.Assunto))
	cab("Date", agora.Format(time.RFC1123Z))
	cab("Message-ID", "<"+hex.EncodeToString(limite)+"@"+dominio+">")
	cab("MIME-Version", "1.0")
	cab("Content-Type", `multipart/alternative; boundary="`+sep+`"`)
	b.WriteString("\r\n")
	for _, parte := range []struct{ tipo, corpo string }{{"text/plain", e.Texto}, {"text/html", e.HTML}} {
		fmt.Fprintf(&b, "--%s\r\nContent-Type: %s; charset=utf-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\n", sep, parte.tipo)
		qp := quotedprintable.NewWriter(&b)
		if _, err := qp.Write([]byte(strings.ReplaceAll(parte.corpo, "\n", "\r\n"))); err != nil {
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

var modeloHTML = template.Must(template.New("email").Parse(`<!doctype html>
<html lang="pt-BR"><body style="margin:0;background:#f4f4f5;font-family:Arial,sans-serif;color:#18181b">
<div style="max-width:520px;margin:24px auto;background:#fff;border-radius:12px;padding:24px">
<p style="margin:0 0 4px;color:#ee4d2d;font-weight:bold">App Parceiros</p>
<h1 style="margin:0 0 12px;font-size:20px">{{.Titulo}}</h1>
{{if .Corpo}}<p style="margin:0 0 20px;line-height:1.5">{{.Corpo}}</p>{{end}}
<a href="{{.Link}}" style="display:inline-block;background:#ee4d2d;color:#fff;text-decoration:none;padding:10px 18px;border-radius:8px;font-weight:bold">{{.Botao}}</a>
<p style="margin:24px 0 0;font-size:12px;color:#71717a">{{.Rodape}}</p>
</div></body></html>`))

type dadosEmail struct {
	Titulo, Corpo, Link, Botao, Rodape string
}

func renderizar(para, assunto string, d dadosEmail) Email {
	var html bytes.Buffer
	_ = modeloHTML.Execute(&html, d) // o modelo é fixo e os dados são texto
	texto := d.Titulo + "\n\n"
	if d.Corpo != "" {
		texto += d.Corpo + "\n\n"
	}
	texto += d.Botao + ": " + d.Link + "\n\n" + d.Rodape + "\n"
	return Email{Para: para, Assunto: assunto, Texto: texto, HTML: html.String()}
}

func (s *Service) emailNotificacao(c contas.Contato, n notificacoesdb.Notificacao) Email {
	return renderizar(c.Email, n.Titulo, dadosEmail{
		Titulo: n.Titulo, Corpo: n.Corpo, Link: s.appURL + n.Url, Botao: "Abrir no app",
		Rodape: "Você recebeu este e-mail porque participa de uma mentoria no App Parceiros. " +
			"Para não receber mais, desligue os e-mails em Notificações, no app.",
	})
}

func emailConvite(c contas.EnvioConvite) Email {
	titulo := "Você foi convidado para a mentoria " + c.WorkspaceNome
	return renderizar(c.Email, titulo, dadosEmail{
		Titulo: titulo,
		Corpo:  "Entre no App Parceiros para receber as listas de produtos da mentoria com o seu link de afiliado.",
		Link:   c.URL, Botao: "Aceitar convite",
		Rodape: "O convite vale até " + c.ExpiraEm.In(fusoBrasilia).Format("02/01/2006 15:04") +
			" (horário de Brasília). Se você não esperava este e-mail, ignore-o.",
	})
}

var fusoBrasilia = func() *time.Location {
	if l, err := time.LoadLocation("America/Sao_Paulo"); err == nil {
		return l
	}
	return time.FixedZone("BRT", -3*60*60)
}()
