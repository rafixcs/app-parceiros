package notificacoes_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/rafixcs/app-parceiros/backend/internal/contas"
	"github.com/rafixcs/app-parceiros/backend/internal/domain"
	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/auth"
	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/database/dbtest"
	httpapi "github.com/rafixcs/app-parceiros/backend/internal/infrastructure/http"
	"github.com/rafixcs/app-parceiros/backend/internal/notificacoes"
	"github.com/rafixcs/app-parceiros/backend/pkg/httputil"
)

type remetente struct {
	mu     sync.Mutex
	emails []domain.Email
	falhar bool
}

func (r *remetente) Send(_ context.Context, e domain.Email) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.falhar {
		return errors.New("smtp fora do ar")
	}
	r.emails = append(r.emails, e)
	return nil
}

type push struct {
	enviados  []string
	expiradas map[string]bool
}

func (p *push) ChavePublica() string { return "chave" }

func (p *push) Enviar(_ context.Context, in notificacoes.Inscricao, _ []byte) error {
	if p.expiradas[in.Endpoint] {
		return notificacoes.ErrInscricaoExpirada
	}
	p.enviados = append(p.enviados, in.Endpoint)
	return nil
}

type ambiente struct {
	t      *testing.T
	router http.Handler
	svc    *notificacoes.Service
	rem    *remetente
	push   *push
}

func novoAmbiente(t *testing.T, comCanais bool) *ambiente {
	t.Helper()
	pool := dbtest.New(t)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	a := &ambiente{t: t, rem: &remetente{}, push: &push{expiradas: map[string]bool{}}}
	contasSvc := contas.NewService(pool, auth.Dev{}, "https://app.teste")
	var rem domain.Mailer
	var ps notificacoes.Push
	if comCanais {
		rem, ps = a.rem, a.push
	}
	a.svc = notificacoes.NewService(pool, nil, contasSvc, rem, ps, "https://app.teste", log)
	contasSvc.EnviarConvitesCom(a.svc.EnviarConvite)
	r := httpapi.NewRouter(log, nil)
	contas.NewHandler(contasSvc, log).Rotas(r, auth.Dev{}, notificacoes.NewHandler(a.svc, log).Modulo())
	a.router = r
	return a
}

func (a *ambiente) chamar(sub, metodo, caminho string, corpo, out any) int {
	a.t.Helper()
	var body io.Reader
	if corpo != nil {
		b, _ := json.Marshal(corpo)
		body = bytes.NewReader(b)
	}
	req := httptest.NewRequest(metodo, caminho, body)
	req.Header.Set("Authorization", "Bearer dev:"+sub)
	rec := httptest.NewRecorder()
	a.router.ServeHTTP(rec, req)
	if out != nil && rec.Body.Len() > 0 {
		if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
			a.t.Fatalf("%s %s: %q: %v", metodo, caminho, rec.Body.String(), err)
		}
	}
	return rec.Code
}

func (a *ambiente) exigir(sub, metodo, caminho string, corpo, out any, status int) {
	a.t.Helper()
	if got := a.chamar(sub, metodo, caminho, corpo, out); got != status {
		a.t.Fatalf("%s %s: %d, quer %d", metodo, caminho, got, status)
	}
}

func (a *ambiente) exigirErro(sub, metodo, caminho string, corpo any, status int, codigo string) {
	a.t.Helper()
	var e httputil.ErrorBody
	if got := a.chamar(sub, metodo, caminho, corpo, &e); got != status || e.Code != codigo {
		a.t.Fatalf("%s %s: %d %q, quer %d %q", metodo, caminho, got, e.Code, status, codigo)
	}
}

func inscricao(endpoint string) map[string]any {
	return map[string]any{"endpoint": endpoint, "keys": map[string]string{"p256dh": "k", "auth": "a"}}
}

func TestPreferenciasEPush(t *testing.T) {
	a := novoAmbiente(t, true)
	ctx := context.Background()
	var eu contas.Usuario
	a.exigir("ana", http.MethodGet, "/v1/eu", nil, &eu, 200)
	var ws []contas.Workspace
	a.exigir("ana", http.MethodGet, "/v1/workspaces", nil, &ws, 200)

	var p notificacoes.Preferencias
	a.exigir("ana", http.MethodGet, "/v1/eu/notificacoes", nil, &p, 200)
	if !p.Email || p.PushChavePublica == nil || *p.PushChavePublica != "chave" || p.Inscricoes != 0 {
		t.Fatalf("preferências iniciais: %+v", p)
	}
	a.exigirErro("ana", http.MethodPost, "/v1/eu/push", inscricao("https://interno.local/x"), 422, "inscricao_invalida")
	a.exigirErro("ana", http.MethodPost, "/v1/eu/push", map[string]any{"endpoint": "https://fcm.googleapis.com/x"}, 422, "inscricao_invalida")
	vivo, morto := "https://fcm.googleapis.com/fcm/send/vivo", "https://web.push.apple.com/morto"
	a.exigir("ana", http.MethodPost, "/v1/eu/push", inscricao(vivo), nil, http.StatusNoContent)
	a.exigir("ana", http.MethodPost, "/v1/eu/push", inscricao(vivo), nil, http.StatusNoContent) // repetir não duplica
	a.exigir("ana", http.MethodPost, "/v1/eu/push", inscricao(morto), nil, http.StatusNoContent)
	a.exigir("ana", http.MethodPut, "/v1/eu/notificacoes", map[string]any{"email": false}, &p, 200)
	if p.Email || p.Inscricoes != 2 {
		t.Fatalf("depois de desligar o e-mail: %+v", p)
	}
	a.exigirErro("ana", http.MethodPut, "/v1/eu/notificacoes", map[string]any{}, 422, "dados_invalidos")

	// Com o e-mail desligado, só vai push; a inscrição expirada é apagada.
	a.push.expiradas[morto] = true
	args := notificacoes.EntregarArgs{WorkspaceID: ws[0].ID, UsuarioID: eu.ID, Tipo: "teste", Chave: "k1", Titulo: "Oi", URL: "/"}
	if err := a.svc.Entregar(ctx, args); err != nil {
		t.Fatal(err)
	}
	if len(a.rem.emails) != 0 || len(a.push.enviados) != 1 || a.push.enviados[0] != vivo {
		t.Fatalf("entrega: e-mails %+v, push %+v", a.rem.emails, a.push.enviados)
	}
	a.exigir("ana", http.MethodGet, "/v1/eu/notificacoes", nil, &p, 200)
	if p.Inscricoes != 1 {
		t.Fatalf("a inscrição expirada continua: %+v", p)
	}

	// Com o e-mail ligado, uma nova notificação vai por e-mail. Se o SMTP
	// falha, o job falha (e é refeito) sem repetir o push.
	a.exigir("ana", http.MethodPut, "/v1/eu/notificacoes", map[string]any{"email": true}, &p, 200)
	a.rem.falhar = true
	args.Chave = "k2"
	if err := a.svc.Entregar(ctx, args); err == nil {
		t.Fatal("entrega com SMTP fora do ar não falhou")
	}
	a.rem.falhar = false
	if err := a.svc.Entregar(ctx, args); err != nil {
		t.Fatal(err)
	}
	if len(a.rem.emails) != 1 || a.rem.emails[0].To != "ana@dev.local" || len(a.push.enviados) != 2 {
		t.Fatalf("segunda entrega: e-mails %+v, push %+v", a.rem.emails, a.push.enviados)
	}

	a.exigir("ana", http.MethodDelete, "/v1/eu/push", map[string]any{"endpoint": vivo}, nil, http.StatusNoContent)
	a.exigir("ana", http.MethodGet, "/v1/eu/notificacoes", nil, &p, 200)
	if p.Inscricoes != 0 {
		t.Fatalf("depois de desinscrever: %+v", p)
	}
	a.exigirErro("ana", http.MethodPost, "/v1/workspaces/"+ws[0].ID.String()+"/notificacoes/"+uuid.NewString()+"/lida", nil, 404, "notificacao_nao_encontrada")
}

func TestConvitePorEmail(t *testing.T) {
	a := novoAmbiente(t, true)
	var ws contas.Workspace
	a.exigir("mestre", http.MethodPost, "/v1/workspaces", map[string]any{"nome": "Turma Top"}, &ws, 201)
	var c contas.Convite
	a.exigir("mestre", http.MethodPost, "/v1/workspaces/"+ws.ID.String()+"/convites", map[string]any{"email": "nova@exemplo.com"}, &c, 201)
	if c.EmailEnviado == nil || !*c.EmailEnviado || len(a.rem.emails) != 1 {
		t.Fatalf("convite: %+v, e-mails %+v", c, a.rem.emails)
	}
	e := a.rem.emails[0]
	if e.To != "nova@exemplo.com" || !strings.Contains(e.Subject, "Turma Top") || !strings.Contains(e.Text, c.URL) {
		t.Fatalf("e-mail do convite: %+v", e)
	}
	// Sem e-mail no convite, nada é enviado e o campo não aparece.
	c = contas.Convite{}
	a.exigir("mestre", http.MethodPost, "/v1/workspaces/"+ws.ID.String()+"/convites", map[string]any{}, &c, 201)
	if c.EmailEnviado != nil || len(a.rem.emails) != 1 {
		t.Fatalf("convite sem e-mail: %+v", c)
	}

	// Sem SMTP, o convite sai com o link e email_enviado = false.
	b := novoAmbiente(t, false)
	b.exigir("mestre", http.MethodPost, "/v1/workspaces", map[string]any{"nome": "Turma"}, &ws, 201)
	c = contas.Convite{}
	b.exigir("mestre", http.MethodPost, "/v1/workspaces/"+ws.ID.String()+"/convites", map[string]any{"email": "x@exemplo.com"}, &c, 201)
	if c.EmailEnviado == nil || *c.EmailEnviado || c.URL == "" {
		t.Fatalf("convite sem SMTP: %+v", c)
	}
	var p notificacoes.Preferencias
	b.exigir("ana", http.MethodGet, "/v1/eu/notificacoes", nil, &p, 200)
	if p.PushChavePublica != nil {
		t.Fatalf("push sem chaves: %+v", p)
	}
	b.exigirErro("ana", http.MethodPost, "/v1/eu/push", inscricao("https://fcm.googleapis.com/x"), 503, "push_indisponivel")
}
