package shopee_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/rafixcs/app-parceiros/backend/internal/contas"
	"github.com/rafixcs/app-parceiros/backend/internal/fontes"
	"github.com/rafixcs/app-parceiros/backend/internal/fontes/shopee"
	"github.com/rafixcs/app-parceiros/backend/internal/platform/auth"
	"github.com/rafixcs/app-parceiros/backend/internal/platform/crypto"
	"github.com/rafixcs/app-parceiros/backend/internal/platform/httpserver"
	"github.com/rafixcs/app-parceiros/backend/internal/platform/postgres"
	"github.com/rafixcs/app-parceiros/backend/internal/platform/postgres/pgtest"
)

const secret = "s3gr3d0-d4-sh0p33"

type ambiente struct {
	t      *testing.T
	router http.Handler
	svc    *shopee.Credenciais
	logs   *bytes.Buffer
	pool   *pgxpool.Pool
}

func novoAmbiente(t *testing.T) (*ambiente, func(sql string) []byte) {
	t.Helper()
	pool := pgtest.New(t)
	logs := &bytes.Buffer{}
	log := slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))

	chave := make([]byte, 32)
	_, _ = rand.Read(chave)
	kek, err := crypto.NovaKEKLocal("teste-1", base64.StdEncoding.EncodeToString(chave))
	if err != nil {
		t.Fatal(err)
	}
	mock := &shopee.Mock{Segredos: map[string]string{"18300001234": secret}}
	svc := shopee.NovasCredenciais(pool, crypto.NovoCofre(kek), shopee.NovoMock(mock, shopee.Config{}))
	contasSvc := contas.NewService(pool, auth.Dev{}, "https://app.teste")

	r := httpserver.NewRouter(log, nil)
	contas.NewHandler(contasSvc, log).Rotas(r, auth.Dev{}, shopee.NewHandler(svc, log).Modulo())

	// bruto lê uma coluna bytea como dono das tabelas (sem RLS).
	bruto := func(sql string) []byte {
		var b []byte
		if err := pool.QueryRow(context.Background(), sql).Scan(&b); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			t.Fatal(err)
		}
		return b
	}
	return &ambiente{t: t, router: r, svc: svc, logs: logs, pool: pool}, bruto
}

func (a *ambiente) chamar(sub, metodo, caminho string, corpo any, out any) (int, string) {
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
			a.t.Fatalf("%s %s: resposta %q: %v", metodo, caminho, rec.Body.String(), err)
		}
	}
	return rec.Code, rec.Body.String()
}

func TestConexaoShopee(t *testing.T) {
	a, bruto := novoAmbiente(t)
	ctx := context.Background()

	var c shopee.Conexao
	if st, _ := a.chamar("ana", http.MethodGet, "/v1/eu/shopee", nil, &c); st != 200 || c.Status != shopee.StatusDesconectado {
		t.Fatalf("antes de conectar: %d %+v", st, c)
	}

	t.Run("dados inválidos", func(t *testing.T) {
		var e httpserver.Erro
		st, _ := a.chamar("ana", http.MethodPut, "/v1/eu/shopee", map[string]string{"app_id": "abc", "secret": "x"}, &e)
		if st != 422 || e.Codigo != "dados_invalidos" {
			t.Fatalf("%d %+v", st, e)
		}
	})

	t.Run("a Shopee recusa", func(t *testing.T) {
		casos := map[string]struct {
			app, secret string
			status      int
			codigo      string
		}{
			"secret errado": {"18300001234", "outro", 422, "credencial_invalida"},
			"assinatura":    {shopee.MockAppIDInvalido, secret, 422, "credencial_invalida"},
			"acesso negado": {shopee.MockAppIDNegado, secret, 422, "credencial_invalida"},
			"limite":        {shopee.MockAppIDLimite, secret, 429, "shopee_limite"},
		}
		for nome, c := range casos {
			var e httpserver.Erro
			st, corpo := a.chamar("ana", http.MethodPut, "/v1/eu/shopee", map[string]string{"app_id": c.app, "secret": c.secret}, &e)
			if st != c.status || e.Codigo != c.codigo {
				t.Errorf("%s: %d %+v", nome, st, e)
			}
			if strings.Contains(corpo, c.secret) {
				t.Errorf("%s: secret na resposta", nome)
			}
		}
		var c shopee.Conexao
		a.chamar("ana", http.MethodGet, "/v1/eu/shopee", nil, &c)
		if c.Status != shopee.StatusDesconectado {
			t.Fatalf("credencial recusada foi salva: %+v", c)
		}
	})

	st, corpo := a.chamar("ana", http.MethodPut, "/v1/eu/shopee", map[string]string{"app_id": "18300001234", "secret": secret}, &c)
	if st != 200 || c.Status != shopee.StatusConectado || c.AppID == nil || *c.AppID != "••••1234" || c.VerificadoEm == nil {
		t.Fatalf("conectar: %d %s", st, corpo)
	}
	if strings.Contains(corpo, secret) {
		t.Fatal("secret na resposta")
	}

	t.Run("cifrada no banco", func(t *testing.T) {
		cifrado := bruto("SELECT secret_cifrado || dek_cifrada FROM credenciais_shopee")
		if len(cifrado) == 0 || bytes.Contains(cifrado, []byte(secret)) {
			t.Fatal("secret em claro no banco")
		}
	})

	ana := a.usuario("ana")
	bia := a.usuario("bia")

	t.Run("credencial decifrada só para o dono", func(t *testing.T) {
		cred, err := a.svc.DoUsuario(ctx, ana)
		if err != nil || cred.Secret != secret || cred.AppID != "18300001234" {
			t.Fatalf("%v %v", cred, err)
		}
		if _, err := a.svc.DoUsuario(ctx, bia); !errors.Is(err, shopee.ErrSemCredencial) {
			t.Fatalf("bia: %v", err)
		}
	})

	t.Run("sem vazamento entre usuários", func(t *testing.T) {
		var c shopee.Conexao
		a.chamar("bia", http.MethodGet, "/v1/eu/shopee", nil, &c)
		if c.Status != shopee.StatusDesconectado {
			t.Fatalf("bia vê a conexão da ana: %+v", c)
		}
		// Mesmo uma query sem filtro, no escopo da bia, não enxerga a linha da ana.
		var n int
		err := postgres.InTx(ctx, a.pool, postgres.Escopo{UsuarioID: bia.String()}, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, "SELECT count(*) FROM credenciais_shopee").Scan(&n)
		})
		if err != nil || n != 0 {
			t.Fatalf("RLS: %d linhas, %v", n, err)
		}
		// Desconectar como bia não apaga a da ana.
		a.chamar("bia", http.MethodDelete, "/v1/eu/shopee", nil, nil)
		if _, err := a.svc.DoUsuario(ctx, ana); err != nil {
			t.Fatalf("a credencial da ana sumiu: %v", err)
		}
	})

	t.Run("falha marca o status", func(t *testing.T) {
		if err := a.svc.RegistrarFalha(ctx, ana, &shopee.ErroAPI{Codigo: 10020}); err != nil {
			t.Fatal(err)
		}
		var c shopee.Conexao
		a.chamar("ana", http.MethodGet, "/v1/eu/shopee", nil, &c)
		if c.Status != shopee.StatusInvalido {
			t.Fatalf("%+v", c)
		}
		if _, err := a.svc.DoUsuario(ctx, ana); !errors.Is(err, shopee.ErrSemCredencial) {
			t.Fatalf("credencial inválida usada: %v", err)
		}
		_ = a.svc.RegistrarFalha(ctx, ana, fontes.ErrAcessoNegado)
		a.chamar("ana", http.MethodGet, "/v1/eu/shopee", nil, &c)
		if c.Status != shopee.StatusExpirado {
			t.Fatalf("%+v", c)
		}
	})

	if st, _ := a.chamar("ana", http.MethodDelete, "/v1/eu/shopee", nil, nil); st != 204 {
		t.Fatalf("desconectar: %d", st)
	}
	a.chamar("ana", http.MethodGet, "/v1/eu/shopee", nil, &c)
	if c.Status != shopee.StatusDesconectado || len(bruto("SELECT secret_cifrado FROM credenciais_shopee")) != 0 {
		t.Fatalf("depois de desconectar: %+v", c)
	}

	if strings.Contains(a.logs.String(), secret) {
		t.Fatal("secret no log")
	}
}

func (a *ambiente) usuario(sub string) uuid.UUID {
	a.t.Helper()
	var u contas.Usuario
	if st, corpo := a.chamar(sub, http.MethodGet, "/v1/eu", nil, &u); st != 200 {
		a.t.Fatalf("eu: %d %s", st, corpo)
	}
	return u.ID
}
