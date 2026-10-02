package notificacoes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/SherClockHolmes/webpush-go"

	"github.com/rafixcs/app-parceiros/backend/internal/notificacoes/notificacoesdb"
)

// ErrInscricaoExpirada: o serviço de push diz que a inscrição não existe mais
// (404 ou 410) e ela deve ser apagada.
var ErrInscricaoExpirada = errors.New("inscrição de push expirada")

// Push envia uma mensagem de Web Push para uma inscrição.
type Push interface {
	ChavePublica() string
	Enviar(ctx context.Context, in Inscricao, payload []byte) error
}

// WebPush assina com VAPID e cifra a mensagem (RFC 8291).
type WebPush struct {
	Publica string
	Privada string
	Assunto string // e-mail de contato do remetente (claim "sub" do VAPID)
	HTTP    *http.Client
}

// GerarChavesVAPID cria um par novo (base64 url, sem padding).
func GerarChavesVAPID() (publica, privada string, err error) {
	privada, publica, err = webpush.GenerateVAPIDKeys()
	return publica, privada, err
}

func (w WebPush) ChavePublica() string { return w.Publica }

func (w WebPush) Enviar(ctx context.Context, in Inscricao, payload []byte) error {
	// O endpoint já foi conferido na inscrição; conferir de novo protege
	// contra linhas antigas, já que o worker faz a chamada HTTP.
	if !endpointPermitido(in.Endpoint) {
		return ErrInscricaoExpirada
	}
	cliente := w.HTTP
	if cliente == nil {
		cliente = &http.Client{Timeout: 15 * time.Second}
	}
	resp, err := webpush.SendNotificationWithContext(ctx, payload, &webpush.Subscription{
		Endpoint: in.Endpoint,
		Keys:     webpush.Keys{P256dh: in.Keys.P256dh, Auth: in.Keys.Auth},
	}, &webpush.Options{
		HTTPClient: cliente, Subscriber: w.Assunto, TTL: int((24 * time.Hour).Seconds()),
		Urgency: webpush.UrgencyNormal, VAPIDPublicKey: w.Publica, VAPIDPrivateKey: w.Privada,
	})
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
	switch {
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone:
		return ErrInscricaoExpirada
	case resp.StatusCode >= 300:
		return fmt.Errorf("serviço de push respondeu %d", resp.StatusCode)
	}
	return nil
}

// Serviços de push dos navegadores. O worker faz POST no endpoint que o
// navegador informou; aceitar só estes hosts evita que alguém use o app para
// chamar endereços internos.
var hostsPush = []string{
	"fcm.googleapis.com",
	"android.googleapis.com",
	"push.services.mozilla.com",
	"notify.windows.com",
	"push.apple.com",
}

func endpointPermitido(endpoint string) bool {
	if len(endpoint) > 1000 {
		return false
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" {
		return false
	}
	h := strings.ToLower(u.Hostname())
	for _, p := range hostsPush {
		if h == p || strings.HasSuffix(h, "."+p) {
			return true
		}
	}
	return false
}

// mensagemPush é o JSON que o service worker do front (web/public/push-sw.js)
// mostra como notificação.
func mensagemPush(n notificacoesdb.Notificacao) []byte {
	b, _ := json.Marshal(map[string]string{"titulo": n.Titulo, "corpo": n.Corpo, "url": n.Url, "id": n.ID.String()})
	return b
}
