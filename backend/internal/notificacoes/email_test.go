package notificacoes

import (
	"bytes"
	"io"
	"mime"
	"mime/multipart"
	"net/mail"
	"strings"
	"testing"
	"time"
)

func TestMontarMensagem(t *testing.T) {
	de, _ := mail.ParseAddress("App Parceiros <nao-responda@parceiros.local>")
	para, _ := mail.ParseAddress("ana@exemplo.com")
	e := renderizar(para.Address, "Nova lista: Achados de verão", dadosEmail{
		Titulo: "Nova lista: Achados de verão", Corpo: "Turma <b>do mestre</b> publicou 3 produtos.",
		Link: "https://app.teste/w/1/listas/2", Botao: "Abrir no app", Rodape: "Rodapé",
	})
	b, err := montarMensagem(de, para, e, time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	m, err := mail.ReadMessage(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	assunto, err := new(mime.WordDecoder).DecodeHeader(m.Header.Get("Subject"))
	if err != nil || assunto != "Nova lista: Achados de verão" {
		t.Fatalf("assunto %q %v", assunto, err)
	}
	_, params, err := mime.ParseMediaType(m.Header.Get("Content-Type"))
	if err != nil {
		t.Fatal(err)
	}
	partes := map[string]string{}
	r := multipart.NewReader(m.Body, params["boundary"])
	for {
		p, err := r.NextPart() // decodifica o quoted-printable
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		corpo, _ := io.ReadAll(p)
		tipo, _, _ := mime.ParseMediaType(p.Header.Get("Content-Type"))
		partes[tipo] = string(corpo)
	}
	if !strings.Contains(partes["text/plain"], "Abrir no app: https://app.teste/w/1/listas/2") {
		t.Fatalf("texto: %q", partes["text/plain"])
	}
	// O HTML escapa o que veio do usuário.
	if !strings.Contains(partes["text/html"], "Turma &lt;b&gt;do mestre&lt;/b&gt;") || strings.Contains(partes["text/html"], "<b>do") {
		t.Fatalf("html: %q", partes["text/html"])
	}
}

func TestEndpointPermitido(t *testing.T) {
	for endpoint, quer := range map[string]bool{
		"https://fcm.googleapis.com/fcm/send/abc":                 true,
		"https://updates.push.services.mozilla.com/wpush/v2/x":    true,
		"https://wns2-bl2p.notify.windows.com/w/?token=x":         true,
		"https://web.push.apple.com/QGx":                          true,
		"http://fcm.googleapis.com/fcm/send/abc":                  false,
		"https://fcm.googleapis.com:8443/x":                       false,
		"https://user@fcm.googleapis.com/x":                       false,
		"https://evil-fcm.googleapis.com.exemplo.com/x":           false,
		"https://169.254.169.254/latest/meta-data":                false,
		"https://localhost/x":                                     false,
		"https://notfcm.googleapis.com.evil/x":                    false,
		"https://fcm.googleapis.com/" + strings.Repeat("a", 1000): false,
	} {
		if got := endpointPermitido(endpoint); got != quer {
			t.Errorf("%s: %v, quer %v", endpoint, got, quer)
		}
	}
}
