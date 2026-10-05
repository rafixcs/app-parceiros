package mail

import (
	"bytes"
	"html/template"
	"time"

	"github.com/rafixcs/app-parceiros/backend/internal/domain"
)

// The layout is customer-facing, so its text is in pt-BR.
var layout = template.Must(template.New("email").Parse(`<!doctype html>
<html lang="pt-BR"><body style="margin:0;background:#f4f4f5;font-family:Arial,sans-serif;color:#18181b">
<div style="max-width:520px;margin:24px auto;background:#fff;border-radius:12px;padding:24px">
<p style="margin:0 0 4px;color:#ee4d2d;font-weight:bold">App Parceiros</p>
<h1 style="margin:0 0 12px;font-size:20px">{{.Title}}</h1>
{{if .Body}}<p style="margin:0 0 20px;line-height:1.5">{{.Body}}</p>{{end}}
<a href="{{.Link}}" style="display:inline-block;background:#ee4d2d;color:#fff;text-decoration:none;padding:10px 18px;border-radius:8px;font-weight:bold">{{.Button}}</a>
<p style="margin:24px 0 0;font-size:12px;color:#71717a">{{.Footer}}</p>
</div></body></html>`))

// Content fills the layout: a title, an optional paragraph, one button with
// its link and a footer.
type Content struct {
	Title, Body, Link, Button, Footer string
}

// Render builds the email in plain text and HTML. User data is escaped in
// the HTML.
func Render(to, subject string, c Content) domain.Email {
	var html bytes.Buffer
	_ = layout.Execute(&html, c) // the layout is fixed and the data is text
	text := c.Title + "\n\n"
	if c.Body != "" {
		text += c.Body + "\n\n"
	}
	text += c.Button + ": " + c.Link + "\n\n" + c.Footer + "\n"
	return domain.Email{To: to, Subject: subject, Text: text, HTML: html.String()}
}

// BrasiliaTime is the time zone of the dates written in emails.
var BrasiliaTime = func() *time.Location {
	if l, err := time.LoadLocation("America/Sao_Paulo"); err == nil {
		return l
	}
	return time.FixedZone("BRT", -3*60*60)
}()
