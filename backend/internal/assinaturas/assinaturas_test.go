package assinaturas_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/rafixcs/app-parceiros/backend/internal/assinaturas"
	"github.com/rafixcs/app-parceiros/backend/internal/contas"
	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/auth"
	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/database/dbtest"
	httpapi "github.com/rafixcs/app-parceiros/backend/internal/infrastructure/http"
	"github.com/rafixcs/app-parceiros/backend/pkg/httputil"
)

type ambiente struct {
	t      *testing.T
	pool   *pgxpool.Pool
	contas *contas.Service
	svc    *assinaturas.Service
	gw     *assinaturas.Mock
	router http.Handler
}

func novoAmbiente(t *testing.T) *ambiente {
	t.Helper()
	pool := dbtest.New(t)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	contasSvc := contas.NewService(pool, auth.Dev{}, "https://app.teste")
	gw := assinaturas.NovoMock()
	svc := assinaturas.NewService(pool, gw, contasSvc, nil, log)
	r := httpapi.NewRouter(log, nil)
	contas.NewHandler(contasSvc, log).Rotas(r, auth.Dev{}, assinaturas.NewHandler(svc, log).Modulo())
	return &ambiente{t: t, pool: pool, contas: contasSvc, svc: svc, gw: gw, router: r}
}

func (a *ambiente) chamar(sub, metodo, caminho string, corpo any, out any) int {
	a.t.Helper()
	var body io.Reader
	if corpo != nil {
		b, err := json.Marshal(corpo)
		if err != nil {
			a.t.Fatal(err)
		}
		body = bytes.NewReader(b)
	}
	req := httptest.NewRequest(metodo, caminho, body)
	if sub != "" {
		req.Header.Set("Authorization", "Bearer dev:"+sub)
	}
	rec := httptest.NewRecorder()
	a.router.ServeHTTP(rec, req)
	if out != nil && rec.Body.Len() > 0 {
		if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
			a.t.Fatalf("%s %s: resposta inválida %q: %v", metodo, caminho, rec.Body.String(), err)
		}
	}
	return rec.Code
}

func (a *ambiente) exigir(sub, metodo, caminho string, corpo any, out any, status int) {
	a.t.Helper()
	var erro httputil.ErrorBody
	if out == nil {
		out = &erro
	}
	if got := a.chamar(sub, metodo, caminho, corpo, out); got != status {
		a.t.Fatalf("%s %s como %q: status %d, quer %d (%+v)", metodo, caminho, sub, got, status, out)
	}
}

func (a *ambiente) exigirErro(sub, metodo, caminho string, corpo any, status int, codigo string) {
	a.t.Helper()
	var erro httputil.ErrorBody
	got := a.chamar(sub, metodo, caminho, corpo, &erro)
	if got != status || erro.Code != codigo {
		a.t.Fatalf("%s %s como %q: %d %q, quer %d %q", metodo, caminho, sub, got, erro.Code, status, codigo)
	}
}

func (a *ambiente) usuario(sub string) contas.Usuario {
	a.t.Helper()
	var u contas.Usuario
	a.exigir(sub, http.MethodGet, "/v1/eu", nil, &u, http.StatusOK)
	return u
}

func (a *ambiente) mentoria(sub, nome string) contas.Workspace {
	a.t.Helper()
	var ws contas.Workspace
	a.exigir(sub, http.MethodPost, "/v1/workspaces", map[string]any{"nome": nome}, &ws, http.StatusCreated)
	return ws
}

func (a *ambiente) pessoal(sub string) contas.Workspace {
	a.t.Helper()
	var ws []contas.Workspace
	a.exigir(sub, http.MethodGet, "/v1/workspaces", nil, &ws, http.StatusOK)
	for _, w := range ws {
		if w.Tipo == contas.TipoPessoal {
			return w
		}
	}
	a.t.Fatalf("usuário %q sem workspace pessoal", sub)
	return contas.Workspace{}
}

func (a *ambiente) entrar(sub, dono string, ws uuid.UUID) contas.Usuario {
	a.t.Helper()
	var c contas.Convite
	a.exigir(dono, http.MethodPost, "/v1/workspaces/"+ws.String()+"/convites", map[string]any{}, &c, http.StatusCreated)
	a.exigir(sub, http.MethodPost, "/v1/convites/"+c.Token+"/aceitar", nil, nil, http.StatusOK)
	return a.usuario(sub)
}

func (a *ambiente) admin(sql string, args ...any) {
	a.t.Helper()
	if _, err := a.pool.Exec(context.Background(), sql, args...); err != nil {
		a.t.Fatal(err)
	}
}

// aviso manda um evento de cobrança pelo webhook, como o gateway faria.
func (a *ambiente) aviso(id, tipo, externoID string, vencimento time.Time, status int) {
	a.t.Helper()
	corpo := map[string]any{"id": id, "tipo": tipo, "assinatura": externoID}
	if !vencimento.IsZero() {
		corpo["vencimento"] = vencimento.Format(time.DateOnly)
	}
	a.exigir("", http.MethodPost, "/v1/webhooks/cobranca", corpo, nil, status)
}

func (a *ambiente) membro(sub string, ws uuid.UUID) contas.Membro {
	a.t.Helper()
	m, err := a.contas.Membro(context.Background(), a.usuario(sub).ID, ws)
	if err != nil {
		a.t.Fatal(err)
	}
	return m
}

func (a *ambiente) externoID() string {
	a.t.Helper()
	for id := range a.gw.Assinaturas {
		return id
	}
	a.t.Fatal("nenhuma assinatura criada no gateway")
	return ""
}

func TestCheckoutEPagamento(t *testing.T) {
	a := novoAmbiente(t)
	ws := a.mentoria("mentor", "Turma de outubro")
	base := "/v1/workspaces/" + ws.ID.String()

	var v assinaturas.Assinatura
	a.exigir("mentor", http.MethodGet, base+"/assinatura", nil, &v, http.StatusOK)
	if v.Status != assinaturas.StatusSemAssinatura || v.Situacao != contas.SituacaoTeste || v.PrecoCentavos != 1490 {
		t.Fatalf("no teste: %+v", v)
	}
	if v.AssentosMaximo != 200 || v.AssentosEmUso != 0 || !v.Simulavel {
		t.Fatalf("limites: %+v", v)
	}

	a.exigirErro("mentor", http.MethodPost, base+"/assinatura",
		map[string]any{"assentos": 10, "cpf_cnpj": "123"}, http.StatusUnprocessableEntity, "dados_invalidos")
	a.exigirErro("mentor", http.MethodPost, base+"/assinatura",
		map[string]any{"assentos": 0, "cpf_cnpj": "390.533.447-05"}, http.StatusUnprocessableEntity, "dados_invalidos")

	a.exigir("mentor", http.MethodPost, base+"/assinatura",
		map[string]any{"assentos": 10, "cpf_cnpj": "390.533.447-05"}, &v, http.StatusCreated)
	if v.Status != assinaturas.StatusAguardando || v.ValorCentavos != 10*1490 || v.Assentos != 10 {
		t.Fatalf("depois de assinar: %+v", v)
	}
	// Assinar não paga: o workspace segue no teste e sem assentos contratados.
	if v.Situacao != contas.SituacaoTeste {
		t.Fatalf("situação = %q", v.Situacao)
	}
	externo := a.externoID()
	if n := a.gw.Assinaturas[externo]; n.ValorCentavos != 10*1490 || n.CPFCNPJ != "39053344705" {
		t.Fatalf("pedido no gateway: %+v", n)
	}
	a.exigirErro("mentor", http.MethodPost, base+"/assinatura",
		map[string]any{"assentos": 10, "cpf_cnpj": "390.533.447-05"}, http.StatusConflict, "ja_assinada")

	// A cobrança criada traz o link da fatura.
	vencimento := time.Now().Add(72 * time.Hour).Truncate(24 * time.Hour)
	a.aviso("evt-cobranca", "cobranca", externo, vencimento, http.StatusNoContent)
	a.exigir("mentor", http.MethodGet, base+"/assinatura", nil, &v, http.StatusOK)
	if v.ProximoCiclo == nil || *v.ProximoCiclo != vencimento.Format(time.DateOnly) {
		t.Fatalf("próximo ciclo = %v", v.ProximoCiclo)
	}

	// Pagamento confirmado: workspace ativo até o fim do ciclo, com os
	// assentos contratados.
	a.aviso("evt-pago", "pago", externo, vencimento, http.StatusNoContent)
	a.exigir("mentor", http.MethodGet, base+"/assinatura", nil, &v, http.StatusOK)
	fim := vencimento.Add(assinaturas.Ciclo + assinaturas.Tolerancia)
	if v.Status != assinaturas.StatusAtiva || v.Situacao != contas.SituacaoAtivo {
		t.Fatalf("depois do pagamento: %+v", v)
	}
	if v.AcessoAte.Sub(fim).Abs() > time.Minute || v.Assentos != 10 {
		t.Fatalf("acesso até %s (quer %s), assentos %d", v.AcessoAte, fim, v.Assentos)
	}

	// Reenvio do mesmo aviso não muda nada.
	antes := v
	a.aviso("evt-pago", "pago", externo, vencimento.Add(30*24*time.Hour), http.StatusNoContent)
	a.exigir("mentor", http.MethodGet, base+"/assinatura", nil, &v, http.StatusOK)
	if v.AcessoAte.Sub(antes.AcessoAte).Abs() > time.Second {
		t.Fatalf("reenvio mudou o acesso: %s -> %s", antes.AcessoAte, v.AcessoAte)
	}

	// Atraso não corta o acesso na hora (ele cai pela data); estorno corta.
	a.aviso("evt-atraso", "atrasado", externo, vencimento, http.StatusNoContent)
	a.exigir("mentor", http.MethodGet, base+"/assinatura", nil, &v, http.StatusOK)
	if v.Status != assinaturas.StatusAtrasada || v.Situacao != contas.SituacaoAtivo {
		t.Fatalf("no atraso: %+v", v)
	}
	a.aviso("evt-estorno", "estornado", externo, time.Time{}, http.StatusNoContent)
	a.exigir("mentor", http.MethodGet, base+"/assinatura", nil, &v, http.StatusOK)
	if v.Situacao != contas.SituacaoSuspenso {
		t.Fatalf("depois do estorno: %+v", v)
	}
}

func TestPlanoAvulso(t *testing.T) {
	a := novoAmbiente(t)
	ws := a.pessoal("ana")
	base := "/v1/workspaces/" + ws.ID.String()

	var v assinaturas.Assinatura
	a.exigir("ana", http.MethodGet, base+"/assinatura", nil, &v, http.StatusOK)
	if v.Plano != "avulso" || v.PrecoCentavos != 2990 || v.AssentosMaximo != 1 {
		t.Fatalf("plano avulso: %+v", v)
	}
	// Assentos pedidos no avulso são ignorados: é um workspace de uma pessoa.
	a.exigir("ana", http.MethodPost, base+"/assinatura",
		map[string]any{"assentos": 7, "cpf_cnpj": "39053344705"}, &v, http.StatusCreated)
	if v.ValorCentavos != 2990 || v.Assentos != 1 {
		t.Fatalf("assinatura avulsa: %+v", v)
	}
	// Simular o pagamento (só com o gateway mock) ativa o workspace.
	a.exigir("ana", http.MethodPost, base+"/assinatura/simular-pagamento", nil, &v, http.StatusOK)
	if v.Status != assinaturas.StatusAtiva || v.Situacao != contas.SituacaoAtivo {
		t.Fatalf("depois de simular: %+v", v)
	}
}

func TestSuspensoPagaPelaAssinatura(t *testing.T) {
	a := novoAmbiente(t)
	ws := a.mentoria("mentor", "Turma")
	base := "/v1/workspaces/" + ws.ID.String()
	a.admin("UPDATE workspaces SET acesso_ate = now() - interval '1 day' WHERE id = $1", ws.ID)

	// Com o workspace suspenso, a assinatura continua acessível: é por ela que
	// ele volta.
	a.exigirErro("mentor", http.MethodGet, base+"/membros", nil, http.StatusPaymentRequired, "workspace_suspenso")
	var v assinaturas.Assinatura
	a.exigir("mentor", http.MethodGet, base+"/assinatura", nil, &v, http.StatusOK)
	if v.Situacao != contas.SituacaoSuspenso {
		t.Fatalf("situação = %q", v.Situacao)
	}
	a.exigir("mentor", http.MethodPost, base+"/assinatura",
		map[string]any{"assentos": 1, "cpf_cnpj": "39053344705"}, &v, http.StatusCreated)
	a.aviso("evt-1", "pago", a.externoID(), time.Now().Truncate(24*time.Hour), http.StatusNoContent)
	var membros []contas.MembroDetalhe
	a.exigir("mentor", http.MethodGet, base+"/membros", nil, &membros, http.StatusOK)
}

func TestMudarAssentosECancelar(t *testing.T) {
	a := novoAmbiente(t)
	ws := a.mentoria("mentor", "Turma")
	base := "/v1/workspaces/" + ws.ID.String()
	a.admin("UPDATE limites SET valor = 5 WHERE plano = 'mentoria' AND chave = 'assentos'")

	var v assinaturas.Assinatura
	a.exigir("mentor", http.MethodPost, base+"/assinatura",
		map[string]any{"assentos": 4, "cpf_cnpj": "39053344705"}, &v, http.StatusCreated)
	externo := a.externoID()
	a.aviso("evt-1", "pago", externo, time.Now().Truncate(24*time.Hour), http.StatusNoContent)

	a.entrar("bia", "mentor", ws.ID)
	a.entrar("caio", "mentor", ws.ID)

	a.exigirErro("mentor", http.MethodPatch, base+"/assinatura",
		map[string]any{"assentos": 9}, http.StatusUnprocessableEntity, "dados_invalidos")
	a.exigirErro("mentor", http.MethodPatch, base+"/assinatura",
		map[string]any{"assentos": 1}, http.StatusConflict, "assentos_em_uso")

	// Menos assentos valem na hora.
	a.exigir("mentor", http.MethodPatch, base+"/assinatura", map[string]any{"assentos": 2}, &v, http.StatusOK)
	if v.Assentos != 2 || v.ValorCentavos != 2*1490 || a.gw.Valores[externo] != 2*1490 {
		t.Fatalf("depois de diminuir: %+v (gateway %d)", v, a.gw.Valores[externo])
	}
	a.exigirErro("mentor", http.MethodPost, base+"/convites", map[string]any{}, http.StatusConflict, "sem_assentos")

	// Mais assentos valem no próximo pagamento.
	a.exigir("mentor", http.MethodPatch, base+"/assinatura", map[string]any{"assentos": 5}, &v, http.StatusOK)
	a.exigirErro("mentor", http.MethodPost, base+"/convites", map[string]any{}, http.StatusConflict, "sem_assentos")
	a.aviso("evt-2", "pago", externo, time.Now().Truncate(24*time.Hour), http.StatusNoContent)
	var c contas.Convite
	a.exigir("mentor", http.MethodPost, base+"/convites", map[string]any{}, &c, http.StatusCreated)

	// Cancelar encerra no gateway e mantém o acesso já pago.
	a.exigir("mentor", http.MethodDelete, base+"/assinatura", nil, &v, http.StatusOK)
	if v.Status != assinaturas.StatusCancelada || !a.gw.Canceladas[externo] || v.Situacao != contas.SituacaoAtivo {
		t.Fatalf("depois de cancelar: %+v", v)
	}
	a.exigirErro("mentor", http.MethodDelete, base+"/assinatura", nil, http.StatusNotFound, "sem_assinatura")
	// Depois de cancelar dá para assinar de novo.
	a.exigir("mentor", http.MethodPost, base+"/assinatura",
		map[string]any{"assentos": 3, "cpf_cnpj": "39053344705"}, &v, http.StatusCreated)
	if v.Status != assinaturas.StatusAguardando || v.Assentos != 3 {
		t.Fatalf("nova assinatura: %+v", v)
	}
}

func TestPermissoes(t *testing.T) {
	a := novoAmbiente(t)
	ws := a.mentoria("mentor", "Turma")
	base := "/v1/workspaces/" + ws.ID.String()
	a.entrar("bia", "mentor", ws.ID)
	var v assinaturas.Assinatura
	a.exigir("mentor", http.MethodPost, base+"/assinatura",
		map[string]any{"assentos": 2, "cpf_cnpj": "39053344705"}, &v, http.StatusCreated)

	// O afiliado não vê nem mexe na assinatura.
	a.exigirErro("bia", http.MethodGet, base+"/assinatura", nil, http.StatusForbidden, "sem_permissao")
	a.exigirErro("bia", http.MethodPost, base+"/assinatura",
		map[string]any{"assentos": 2, "cpf_cnpj": "39053344705"}, http.StatusForbidden, "sem_permissao")
	a.exigirErro("bia", http.MethodDelete, base+"/assinatura", nil, http.StatusForbidden, "sem_permissao")

	// Quem está fora do workspace nem chega ao módulo.
	a.exigirErro("intruso", http.MethodGet, base+"/assinatura", nil, http.StatusNotFound, "workspace_nao_encontrado")
	a.exigirErro("intruso", http.MethodDelete, base+"/assinatura", nil, http.StatusNotFound, "workspace_nao_encontrado")
}

// TestVazamentoEntreWorkspaces confere que a RLS esconde a assinatura de um
// workspace de quem não é gestor dele, mesmo se o módulo for chamado com um
// membro forjado.
func TestVazamentoEntreWorkspaces(t *testing.T) {
	a := novoAmbiente(t)
	ws := a.mentoria("mentor", "Turma")
	base := "/v1/workspaces/" + ws.ID.String()
	var v assinaturas.Assinatura
	a.exigir("mentor", http.MethodPost, base+"/assinatura",
		map[string]any{"assentos": 3, "cpf_cnpj": "39053344705"}, &v, http.StatusCreated)

	ctx := context.Background()
	intrusa := a.usuario("intrusa")
	forjado := contas.Membro{
		WorkspaceID: ws.ID, UsuarioID: intrusa.ID, Papel: contas.PapelDono, TipoWorkspace: contas.TipoMentoria,
	}
	vista, err := a.svc.Ver(ctx, forjado)
	if err != nil {
		t.Fatal(err)
	}
	if vista.Status != assinaturas.StatusSemAssinatura || vista.ValorCentavos != 0 || vista.URLPagamento != nil {
		t.Fatalf("assinatura de outro workspace apareceu: %+v", vista)
	}
	if _, err := a.svc.Cancelar(ctx, forjado); !errors.Is(err, assinaturas.ErrSemAssinatura) {
		t.Fatalf("cancelar de fora: %v", err)
	}
	// A assinatura continua lá, intacta.
	a.exigir("mentor", http.MethodGet, base+"/assinatura", nil, &v, http.StatusOK)
	if v.Status != assinaturas.StatusAguardando || v.Assentos != 3 {
		t.Fatalf("assinatura mexida de fora: %+v", v)
	}
}

func TestWebhookDesconhecidoEInvalido(t *testing.T) {
	a := novoAmbiente(t)
	// Assinatura que não é nossa: nada a fazer, e o gateway não precisa
	// reenviar.
	a.aviso("evt-x", "pago", "mock_sub_999", time.Now(), http.StatusNoContent)
	// Aviso sem assinatura, ou de um tipo que não interessa.
	a.exigirErro("", http.MethodPost, "/v1/webhooks/cobranca", map[string]any{"id": "evt-y", "tipo": "pago"},
		http.StatusUnauthorized, "nao_autenticado")
	a.aviso("evt-z", "outra_coisa", "mock_sub_1", time.Time{}, http.StatusNoContent)
}

func TestFalhaDoGateway(t *testing.T) {
	a := novoAmbiente(t)
	ws := a.mentoria("mentor", "Turma")
	base := "/v1/workspaces/" + ws.ID.String()
	a.gw.Falhar = errors.New("gateway fora do ar")
	a.exigirErro("mentor", http.MethodPost, base+"/assinatura",
		map[string]any{"assentos": 2, "cpf_cnpj": "39053344705"}, http.StatusBadGateway, "cobranca_indisponivel")
	// Nada foi gravado: o checkout pode ser repetido.
	a.gw.Falhar = nil
	var v assinaturas.Assinatura
	a.exigir("mentor", http.MethodPost, base+"/assinatura",
		map[string]any{"assentos": 2, "cpf_cnpj": "39053344705"}, &v, http.StatusCreated)
	if v.Status != assinaturas.StatusAguardando {
		t.Fatalf("%+v", v)
	}
}

func TestSemSimulacaoForaDoMock(t *testing.T) {
	a := novoAmbiente(t)
	ws := a.mentoria("mentor", "Turma")
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := assinaturas.NewService(a.pool, assinaturas.Asaas{Chave: "x"}, a.contas, nil, log)
	if _, err := svc.SimularPagamento(context.Background(), a.membro("mentor", ws.ID)); !errors.Is(err, assinaturas.ErrSemSimulacao) {
		t.Fatalf("simulação com gateway de verdade: %v", err)
	}
}
