package contas_test

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
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/rafixcs/app-parceiros/backend/internal/contas"
	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/auth"
	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/database"
	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/database/dbtest"
	httpapi "github.com/rafixcs/app-parceiros/backend/internal/infrastructure/http"
	"github.com/rafixcs/app-parceiros/backend/pkg/httputil"
)

type ambiente struct {
	t      *testing.T
	pool   *pgxpool.Pool
	svc    *contas.Service
	router http.Handler
}

func novoAmbiente(t *testing.T) *ambiente {
	t.Helper()
	pool := dbtest.New(t)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := contas.NewService(pool, auth.Dev{}, "https://app.teste")
	r := httpapi.NewRouter(log, nil)
	contas.NewHandler(svc, log).Rotas(r, auth.Dev{})
	return &ambiente{t: t, pool: pool, svc: svc, router: r}
}

// chamar faz uma requisição como o usuário `sub` (vazio = sem login) e
// decodifica a resposta em out, se não for nil.
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

func (a *ambiente) convite(sub string, ws uuid.UUID, corpo map[string]any) contas.Convite {
	a.t.Helper()
	if corpo == nil {
		corpo = map[string]any{}
	}
	var c contas.Convite
	a.exigir(sub, http.MethodPost, "/v1/workspaces/"+ws.String()+"/convites", corpo, &c, http.StatusCreated)
	return c
}

// admin roda SQL como dono das tabelas, para preparar cenários (ex.: expirar
// um convite). Não passa pelo papel da API.
func (a *ambiente) admin(sql string, args ...any) {
	a.t.Helper()
	if _, err := a.pool.Exec(context.Background(), sql, args...); err != nil {
		a.t.Fatal(err)
	}
}

func TestPrimeiroAcessoCriaWorkspacePessoal(t *testing.T) {
	a := novoAmbiente(t)

	u1 := a.usuario("ana")
	u2 := a.usuario("ana")
	if u1.ID != u2.ID || u1.Email != "ana@dev.local" {
		t.Fatalf("usuário mudou entre acessos: %+v %+v", u1, u2)
	}

	var ws []contas.Workspace
	a.exigir("ana", http.MethodGet, "/v1/workspaces", nil, &ws, http.StatusOK)
	if len(ws) != 1 || ws[0].Tipo != contas.TipoPessoal || ws[0].Papel != contas.PapelDono || ws[0].Plano != "avulso" {
		t.Fatalf("workspaces = %+v, quer um pessoal com papel dono", ws)
	}
}

func TestSemLogin(t *testing.T) {
	a := novoAmbiente(t)
	a.exigirErro("", http.MethodGet, "/v1/eu", nil, http.StatusUnauthorized, "nao_autenticado")
}

// Critério de pronto do M1: o mentor convida e o afiliado entra.
func TestMentorConvidaAfiliadoEntra(t *testing.T) {
	a := novoAmbiente(t)

	ws := a.mentoria("mentor", "Turma de outubro")
	if ws.Tipo != contas.TipoMentoria || ws.Papel != contas.PapelDono || ws.Plano != "mentoria" {
		t.Fatalf("mentoria criada = %+v", ws)
	}

	c := a.convite("mentor", ws.ID, nil)
	if c.Token == "" || c.URL != "https://app.teste/convite/"+c.Token {
		t.Fatalf("convite sem link: %+v", c)
	}
	if d := time.Until(c.ExpiraEm); d < 6*24*time.Hour || d > 8*24*time.Hour {
		t.Fatalf("validade padrão = %v, quer ~7 dias", d)
	}

	// Quem recebe o link vê o convite antes de entrar.
	var pub contas.ConvitePublico
	a.exigir("", http.MethodGet, "/v1/convites/"+c.Token, nil, &pub, http.StatusOK)
	if pub.WorkspaceNome != "Turma de outubro" || pub.Status != contas.ConviteValido {
		t.Fatalf("convite público = %+v", pub)
	}

	var entrou contas.Workspace
	a.exigir("bia", http.MethodPost, "/v1/convites/"+c.Token+"/aceitar", nil, &entrou, http.StatusOK)
	if entrou.ID != ws.ID || entrou.Papel != contas.PapelAfiliado {
		t.Fatalf("aceite = %+v", entrou)
	}

	// O afiliado vê o workspace pessoal e o da mentoria no seletor.
	var dela []contas.Workspace
	a.exigir("bia", http.MethodGet, "/v1/workspaces", nil, &dela, http.StatusOK)
	if len(dela) != 2 || dela[1].ID != ws.ID || dela[1].Papel != contas.PapelAfiliado {
		t.Fatalf("workspaces da afiliada = %+v", dela)
	}

	var membros []contas.MembroDetalhe
	a.exigir("mentor", http.MethodGet, "/v1/workspaces/"+ws.ID.String()+"/membros", nil, &membros, http.StatusOK)
	if len(membros) != 2 || membros[0].Papel != contas.PapelDono || membros[1].Email != "bia@dev.local" {
		t.Fatalf("membros = %+v", membros)
	}

	// Convite usado mostra mensagem clara, para quem tentar de novo.
	a.exigir("", http.MethodGet, "/v1/convites/"+c.Token, nil, &pub, http.StatusOK)
	if pub.Status != contas.ConviteUsado {
		t.Fatalf("status depois do aceite = %q", pub.Status)
	}
	a.exigirErro("caio", http.MethodPost, "/v1/convites/"+c.Token+"/aceitar", nil, http.StatusGone, "convite_usado")
}

func TestAfiliadoNaoGereTurma(t *testing.T) {
	a := novoAmbiente(t)
	ws := a.mentoria("mentor", "Turma")
	c := a.convite("mentor", ws.ID, nil)
	a.exigir("bia", http.MethodPost, "/v1/convites/"+c.Token+"/aceitar", nil, nil, http.StatusOK)

	base := "/v1/workspaces/" + ws.ID.String()
	a.exigirErro("bia", http.MethodGet, base+"/membros", nil, http.StatusForbidden, "sem_permissao")
	a.exigirErro("bia", http.MethodPost, base+"/convites", map[string]any{}, http.StatusForbidden, "sem_permissao")
	a.exigirErro("bia", http.MethodGet, base+"/convites", nil, http.StatusForbidden, "sem_permissao")
	a.exigirErro("bia", http.MethodPatch, base, map[string]any{"nome": "Minha"}, http.StatusForbidden, "sem_permissao")

	var ver contas.Workspace
	a.exigir("bia", http.MethodGet, base, nil, &ver, http.StatusOK)
	if ver.Nome != "Turma" || ver.Papel != contas.PapelAfiliado {
		t.Fatalf("workspace visto pela afiliada = %+v", ver)
	}
}

func TestConvitesInvalidos(t *testing.T) {
	a := novoAmbiente(t)
	ws := a.mentoria("mentor", "Turma")
	base := "/v1/workspaces/" + ws.ID.String()

	t.Run("expirado", func(t *testing.T) {
		c := a.convite("mentor", ws.ID, nil)
		a.admin("UPDATE convites SET expira_em = now() - interval '1 minute' WHERE id = $1", c.ID)
		var pub contas.ConvitePublico
		a.exigir("", http.MethodGet, "/v1/convites/"+c.Token, nil, &pub, http.StatusOK)
		if pub.Status != contas.ConviteExpirado {
			t.Fatalf("status = %q", pub.Status)
		}
		a.exigirErro("bia", http.MethodPost, "/v1/convites/"+c.Token+"/aceitar", nil, http.StatusGone, "convite_expirado")
	})

	t.Run("revogado", func(t *testing.T) {
		c := a.convite("mentor", ws.ID, nil)
		a.exigir("mentor", http.MethodDelete, base+"/convites/"+c.ID.String(), nil, nil, http.StatusNoContent)
		a.exigirErro("bia", http.MethodPost, "/v1/convites/"+c.Token+"/aceitar", nil, http.StatusGone, "convite_revogado")
		a.exigirErro("mentor", http.MethodDelete, base+"/convites/"+c.ID.String(), nil, http.StatusNotFound, "convite_nao_encontrado")
	})

	t.Run("token desconhecido", func(t *testing.T) {
		falso := "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
		a.exigirErro("", http.MethodGet, "/v1/convites/"+falso, nil, http.StatusNotFound, "convite_nao_encontrado")
		a.exigirErro("bia", http.MethodPost, "/v1/convites/lixo/aceitar", nil, http.StatusNotFound, "convite_nao_encontrado")
	})

	t.Run("outro e-mail", func(t *testing.T) {
		c := a.convite("mentor", ws.ID, map[string]any{"email": " Duda@Dev.Local "})
		if c.Email == nil || *c.Email != "duda@dev.local" {
			t.Fatalf("e-mail do convite = %v", c.Email)
		}
		a.exigirErro("caio", http.MethodPost, "/v1/convites/"+c.Token+"/aceitar", nil, http.StatusForbidden, "convite_outro_email")
		a.exigir("duda", http.MethodPost, "/v1/convites/"+c.Token+"/aceitar", nil, nil, http.StatusOK)
	})

	t.Run("já é membro", func(t *testing.T) {
		c := a.convite("mentor", ws.ID, nil)
		a.exigirErro("mentor", http.MethodPost, "/v1/convites/"+c.Token+"/aceitar", nil, http.StatusConflict, "ja_membro")
	})

	t.Run("workspace pessoal", func(t *testing.T) {
		var ws []contas.Workspace
		a.exigir("mentor", http.MethodGet, "/v1/workspaces", nil, &ws, http.StatusOK)
		a.exigirErro("mentor", http.MethodPost, "/v1/workspaces/"+ws[0].ID.String()+"/convites", map[string]any{}, http.StatusConflict, "so_mentoria")
	})

	t.Run("validade fora do limite", func(t *testing.T) {
		a.exigirErro("mentor", http.MethodPost, base+"/convites", map[string]any{"validade_horas": 24 * 31}, http.StatusUnprocessableEntity, "dados_invalidos")
	})
}

func TestLimiteDeAssentos(t *testing.T) {
	a := novoAmbiente(t)
	// No teste vale o limite de teste do plano.
	a.admin("UPDATE limites SET valor = 2 WHERE plano = 'mentoria' AND chave = 'assentos_teste'")
	ws := a.mentoria("mentor", "Turma")
	base := "/v1/workspaces/" + ws.ID.String()

	c1 := a.convite("mentor", ws.ID, nil)
	c2 := a.convite("mentor", ws.ID, nil)
	// Convites pendentes ocupam assento.
	a.exigirErro("mentor", http.MethodPost, base+"/convites", map[string]any{}, http.StatusConflict, "sem_assentos")

	a.exigir("bia", http.MethodPost, "/v1/convites/"+c1.Token+"/aceitar", nil, nil, http.StatusOK)
	a.exigirErro("mentor", http.MethodPost, base+"/convites", map[string]any{}, http.StatusConflict, "sem_assentos")

	// Com assentos contratados, valem eles. Se caírem depois de o convite ter
	// sido criado, o aceite barra.
	a.admin("UPDATE workspaces SET assentos = 1 WHERE id = $1", ws.ID)
	a.exigirErro("caio", http.MethodPost, "/v1/convites/"+c2.Token+"/aceitar", nil, http.StatusConflict, "sem_assentos")
	a.admin("UPDATE workspaces SET assentos = 3 WHERE id = $1", ws.ID)
	a.exigir("caio", http.MethodPost, "/v1/convites/"+c2.Token+"/aceitar", nil, nil, http.StatusOK)
}

func TestSuspensao(t *testing.T) {
	a := novoAmbiente(t)
	ws := a.mentoria("mentor", "Turma")
	base := "/v1/workspaces/" + ws.ID.String()
	if ws.Status != contas.SituacaoTeste || time.Until(ws.AcessoAte) < 6*24*time.Hour || ws.Assentos != nil {
		t.Fatalf("mentoria nova: %+v", ws)
	}
	c := a.convite("mentor", ws.ID, nil)
	a.exigir("bia", http.MethodPost, "/v1/convites/"+c.Token+"/aceitar", nil, nil, http.StatusOK)
	bia := a.usuario("bia")

	// Fim do teste sem pagamento: suspenso.
	a.admin("UPDATE workspaces SET acesso_ate = now() - interval '1 minute' WHERE id = $1", ws.ID)
	var visto contas.Workspace
	a.exigir("mentor", http.MethodGet, base+"/", nil, &visto, http.StatusOK)
	if visto.Status != contas.SituacaoSuspenso {
		t.Fatalf("status = %q", visto.Status)
	}
	a.exigirErro("mentor", http.MethodGet, base+"/membros", nil, http.StatusPaymentRequired, "workspace_suspenso")
	a.exigirErro("mentor", http.MethodPost, base+"/convites", map[string]any{}, http.StatusPaymentRequired, "workspace_suspenso")
	a.exigirErro("bia", http.MethodPatch, base+"/", map[string]any{"nome": "x"}, http.StatusPaymentRequired, "workspace_suspenso")
	// O afiliado ainda pode sair.
	a.exigir("bia", http.MethodDelete, base+"/membros/"+bia.ID.String(), nil, nil, http.StatusNoContent)

	// Um pagamento reativa, sem nunca encurtar o acesso.
	ctx := context.Background()
	ate := time.Now().Add(30 * 24 * time.Hour)
	assentos := int32(10)
	if err := a.svc.LiberarAcesso(ctx, ws.ID, ate, &assentos); err != nil {
		t.Fatal(err)
	}
	if err := a.svc.LiberarAcesso(ctx, ws.ID, time.Now().Add(time.Hour), nil); err != nil {
		t.Fatal(err)
	}
	a.exigir("mentor", http.MethodGet, base+"/", nil, &visto, http.StatusOK)
	if visto.Status != contas.SituacaoAtivo || visto.AcessoAte.Sub(ate).Abs() > time.Second || visto.Assentos == nil || *visto.Assentos != 10 {
		t.Fatalf("depois do pagamento: %+v", visto)
	}
	var membros []contas.MembroDetalhe
	a.exigir("mentor", http.MethodGet, base+"/membros", nil, &membros, http.StatusOK)

	// Estorno bloqueia na hora.
	if err := a.svc.BloquearAcesso(ctx, ws.ID); err != nil {
		t.Fatal(err)
	}
	a.exigirErro("mentor", http.MethodGet, base+"/membros", nil, http.StatusPaymentRequired, "workspace_suspenso")
}

func TestDefinirAssentos(t *testing.T) {
	a := novoAmbiente(t)
	a.admin("UPDATE limites SET valor = 4 WHERE plano = 'mentoria' AND chave = 'assentos'")
	ws := a.mentoria("mentor", "Turma")
	a.convite("mentor", ws.ID, nil)
	a.convite("mentor", ws.ID, nil)
	ctx := context.Background()
	m, err := a.svc.Membro(ctx, a.usuario("mentor").ID, ws.ID)
	if err != nil {
		t.Fatal(err)
	}
	if n, err := a.svc.AssentosEmUso(ctx, m); err != nil || n != 2 {
		t.Fatalf("em uso = %d, %v", n, err)
	}
	if err := a.svc.DefinirAssentos(ctx, m, 1); !errors.Is(err, contas.ErrAssentosEmUso) {
		t.Fatalf("abaixo do em uso: %v", err)
	}
	if err := a.svc.DefinirAssentos(ctx, m, 5); !errors.Is(err, contas.ErrAssentosAcimaDoPlano) {
		t.Fatalf("acima do plano: %v", err)
	}
	if err := a.svc.DefinirAssentos(ctx, m, 3); err != nil {
		t.Fatal(err)
	}
	ws2, err := a.svc.Workspace(ctx, m)
	if err != nil || ws2.Assentos == nil || *ws2.Assentos != 3 {
		t.Fatalf("assentos = %v, %v", ws2.Assentos, err)
	}
	m.Papel = contas.PapelMentor
	if err := a.svc.DefinirAssentos(ctx, m, 3); !errors.Is(err, contas.ErrSemPermissao) {
		t.Fatalf("mentor: %v", err)
	}
}

func TestRemoverESair(t *testing.T) {
	a := novoAmbiente(t)
	dono := a.usuario("mentor")
	ws := a.mentoria("mentor", "Turma")
	base := "/v1/workspaces/" + ws.ID.String()

	entrar := func(sub string) contas.Usuario {
		c := a.convite("mentor", ws.ID, nil)
		a.exigir(sub, http.MethodPost, "/v1/convites/"+c.Token+"/aceitar", nil, nil, http.StatusOK)
		return a.usuario(sub)
	}
	bia, caio := entrar("bia"), entrar("caio")

	a.exigirErro("bia", http.MethodDelete, base+"/membros/"+caio.ID.String(), nil, http.StatusForbidden, "sem_permissao")
	a.exigirErro("mentor", http.MethodDelete, base+"/membros/"+dono.ID.String(), nil, http.StatusConflict, "dono_nao_sai")

	// Afiliada sai por conta própria e perde o acesso.
	a.exigir("bia", http.MethodDelete, base+"/membros/"+bia.ID.String(), nil, nil, http.StatusNoContent)
	a.exigirErro("bia", http.MethodGet, base, nil, http.StatusNotFound, "workspace_nao_encontrado")
	var dela []contas.Workspace
	a.exigir("bia", http.MethodGet, "/v1/workspaces", nil, &dela, http.StatusOK)
	if len(dela) != 1 || dela[0].Tipo != contas.TipoPessoal {
		t.Fatalf("depois de sair, workspaces = %+v", dela)
	}

	// O mentor remove um afiliado.
	a.exigir("mentor", http.MethodDelete, base+"/membros/"+caio.ID.String(), nil, nil, http.StatusNoContent)
	a.exigirErro("mentor", http.MethodDelete, base+"/membros/"+caio.ID.String(), nil, http.StatusNotFound, "membro_nao_encontrado")
}

func TestAtualizarWorkspace(t *testing.T) {
	a := novoAmbiente(t)
	ws := a.mentoria("mentor", "Turma")
	base := "/v1/workspaces/" + ws.ID.String()

	var got contas.Workspace
	a.exigir("mentor", http.MethodPatch, base, map[string]any{"nome": "Turma VIP", "foto_url": "https://cdn.teste/f.png"}, &got, http.StatusOK)
	if got.Nome != "Turma VIP" || got.FotoURL == nil || *got.FotoURL != "https://cdn.teste/f.png" {
		t.Fatalf("depois de atualizar = %+v", got)
	}
	a.exigir("mentor", http.MethodPatch, base, map[string]any{"foto_url": ""}, &got, http.StatusOK)
	if got.Nome != "Turma VIP" || got.FotoURL != nil {
		t.Fatalf("depois de remover a foto = %+v", got)
	}
	a.exigirErro("mentor", http.MethodPatch, base, map[string]any{"foto_url": "javascript:alert(1)"}, http.StatusUnprocessableEntity, "dados_invalidos")
	a.exigirErro("mentor", http.MethodPost, "/v1/workspaces", map[string]any{"nome": "  "}, http.StatusUnprocessableEntity, "dados_invalidos")
}

// Vazamento entre workspaces, pela API: quem não é membro não enxerga nem
// altera nada, e o workspace parece não existir.
func TestVazamentoEntreWorkspacesAPI(t *testing.T) {
	a := novoAmbiente(t)
	ws := a.mentoria("mentor", "Turma")
	c := a.convite("mentor", ws.ID, nil)
	a.exigir("bia", http.MethodPost, "/v1/convites/"+c.Token+"/aceitar", nil, nil, http.StatusOK)
	bia := a.usuario("bia")
	a.usuario("intruso")

	base := "/v1/workspaces/" + ws.ID.String()
	for _, tc := range []struct{ metodo, caminho string }{
		{http.MethodGet, base},
		{http.MethodPatch, base},
		{http.MethodGet, base + "/membros"},
		{http.MethodDelete, base + "/membros/" + bia.ID.String()},
		{http.MethodGet, base + "/convites"},
		{http.MethodPost, base + "/convites"},
		{http.MethodDelete, base + "/convites/" + c.ID.String()},
		{http.MethodGet, "/v1/workspaces/" + uuid.NewString()},
	} {
		a.exigirErro("intruso", tc.metodo, tc.caminho, map[string]any{}, http.StatusNotFound, "workspace_nao_encontrado")
	}

	var dele []contas.Workspace
	a.exigir("intruso", http.MethodGet, "/v1/workspaces", nil, &dele, http.StatusOK)
	if len(dele) != 1 || dele[0].Tipo != contas.TipoPessoal {
		t.Fatalf("intruso vê workspaces de outros: %+v", dele)
	}
}

// Vazamento entre workspaces, no banco: mesmo uma query sem filtro só devolve
// linhas do escopo da transação, e escritas em outro workspace são barradas
// pelo RLS.
func TestVazamentoEntreWorkspacesRLS(t *testing.T) {
	a := novoAmbiente(t)
	ctx := context.Background()
	ws := a.mentoria("mentor", "Turma")
	a.convite("mentor", ws.ID, nil)
	intruso := a.usuario("intruso")

	var pessoal []contas.Workspace
	a.exigir("intruso", http.MethodGet, "/v1/workspaces", nil, &pessoal, http.StatusOK)
	escopo := database.Scope{UserID: intruso.ID.String(), WorkspaceID: pessoal[0].ID.String()}

	contar := func(e database.Scope, tabela string) int {
		t.Helper()
		var n int
		err := database.InTx(ctx, a.pool, e, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, "SELECT count(*) FROM "+tabela).Scan(&n)
		})
		if err != nil {
			t.Fatal(err)
		}
		return n
	}

	for tabela, quer := range map[string]int{"usuarios": 1, "workspaces": 1, "membros": 1, "convites": 0} {
		if n := contar(escopo, tabela); n != quer {
			t.Errorf("%s visíveis para o intruso = %d, quer %d", tabela, n, quer)
		}
		if n := contar(database.Scope{}, tabela); n != 0 {
			t.Errorf("%s visíveis sem escopo = %d, quer 0", tabela, n)
		}
	}

	escritas := map[string]func(pgx.Tx) error{
		"entrar como membro": func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, "INSERT INTO membros (workspace_id, usuario_id, papel) VALUES ($1, $2, 'mentor')", ws.ID, intruso.ID)
			return err
		},
		"criar convite": func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, "INSERT INTO convites (workspace_id, token_hash, expira_em, criado_por) VALUES ($1, '\\x00', now() + interval '1 day', $2)", ws.ID, intruso.ID)
			return err
		},
		"criar workspace em nome de outro": func(tx pgx.Tx) error {
			var dono uuid.UUID
			if err := a.pool.QueryRow(ctx, "SELECT dono_id FROM workspaces WHERE id = $1", ws.ID).Scan(&dono); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, "INSERT INTO workspaces (tipo, nome, dono_id, plano) VALUES ('mentoria', 'x', $1, 'mentoria')", dono)
			return err
		},
	}
	for nome, escrever := range escritas {
		err := database.InTx(ctx, a.pool, escopo, escrever)
		if err == nil || !contemRLS(err) {
			t.Errorf("%s: err = %v, quer violação de RLS", nome, err)
		}
	}

	// Alterações e remoções em outro workspace não afetam nenhuma linha.
	err := database.InTx(ctx, a.pool, escopo, func(tx pgx.Tx) error {
		for _, sql := range []string{
			"UPDATE workspaces SET nome = 'invadido' WHERE id = $1",
			"DELETE FROM membros WHERE workspace_id = $1",
			"UPDATE convites SET revogado_em = now() WHERE workspace_id = $1",
		} {
			tag, err := tx.Exec(ctx, sql, ws.ID)
			if err != nil {
				return err
			}
			if tag.RowsAffected() != 0 {
				return errors.New(sql + ": afetou linhas de outro workspace")
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func contemRLS(err error) bool {
	return err != nil && bytes.Contains([]byte(err.Error()), []byte("row-level security"))
}

// TestPessoalGratuitoParaAluno: o workspace pessoal de quem é aluno de uma
// mentoria em dia não é cobrado (decisão provisória, docs/mvp.md §8).
func TestPessoalGratuitoParaAluno(t *testing.T) {
	a := novoAmbiente(t)
	ws := a.mentoria("mentor", "Turma")
	c := a.convite("mentor", ws.ID, nil)
	a.exigir("bia", http.MethodPost, "/v1/convites/"+c.Token+"/aceitar", nil, nil, http.StatusOK)
	bia := a.usuario("bia")

	pessoal := func() contas.Workspace {
		var ws []contas.Workspace
		a.exigir("bia", http.MethodGet, "/v1/workspaces", nil, &ws, http.StatusOK)
		for _, w := range ws {
			if w.Tipo == contas.TipoPessoal {
				return w
			}
		}
		t.Fatal("sem workspace pessoal")
		return contas.Workspace{}
	}
	p := pessoal()
	if p.Status != contas.SituacaoGratuito {
		t.Fatalf("pessoal do aluno = %q", p.Status)
	}
	// Mesmo com o teste do pessoal vencido, o aluno segue usando.
	a.admin("UPDATE workspaces SET acesso_ate = now() - interval '1 day' WHERE id = $1", p.ID)
	var membros []contas.MembroDetalhe
	if got := a.chamar("bia", http.MethodGet, "/v1/workspaces/"+p.ID.String()+"/membros", nil, &membros); got == http.StatusPaymentRequired {
		t.Fatal("pessoal do aluno ficou suspenso")
	}
	// A mentoria do mentor não ganha nada com isso: o pessoal dele segue cobrado.
	var dele []contas.Workspace
	a.exigir("mentor", http.MethodGet, "/v1/workspaces", nil, &dele, http.StatusOK)
	for _, w := range dele {
		if w.Status == contas.SituacaoGratuito {
			t.Fatalf("workspace do mentor gratuito: %+v", w)
		}
	}

	// Mentoria suspensa: o pessoal do aluno volta a depender do próprio acesso.
	a.admin("UPDATE workspaces SET acesso_ate = now() - interval '1 minute' WHERE id = $1", ws.ID)
	if p := pessoal(); p.Status != contas.SituacaoSuspenso {
		t.Fatalf("com a mentoria suspensa, pessoal = %q", p.Status)
	}
	// E quem sai da mentoria também.
	a.admin("UPDATE workspaces SET acesso_ate = now() + interval '1 day' WHERE id = $1", ws.ID)
	a.exigir("bia", http.MethodDelete, "/v1/workspaces/"+ws.ID.String()+"/membros/"+bia.ID.String(), nil, nil, http.StatusNoContent)
	if p := pessoal(); p.Status != contas.SituacaoSuspenso {
		t.Fatalf("fora da mentoria, pessoal = %q", p.Status)
	}
}
