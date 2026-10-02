// Package auth valida os tokens de acesso emitidos pelo Zitadel (OIDC) e põe a
// identidade do chamador no contexto da requisição.
//
// O Zitadel cuida só da identidade. Usuários, workspaces e papéis ficam no
// módulo contas.
package auth

import (
	"context"
	"errors"
	"net/http"
	"strings"
)

// Identidade é quem fez a requisição, segundo o provedor de identidade.
// Email e Nome podem vir vazios: nem todo token de acesso traz esses claims.
type Identidade struct {
	Sub             string
	Email           string
	EmailVerificado bool
	Nome            string
	token           string
}

// Perfil são os dados do usuário no provedor, usados no primeiro acesso.
type Perfil struct {
	Email           string
	EmailVerificado bool
	Nome            string
}

// Verificador valida tokens de acesso e busca o perfil do usuário.
type Verificador interface {
	Verificar(ctx context.Context, token string) (Identidade, error)
	// Perfil devolve e-mail e nome. Usa os claims do token quando existem e,
	// se faltar algum, consulta o endpoint userinfo do provedor.
	Perfil(ctx context.Context, id Identidade) (Perfil, error)
}

var ErrTokenInvalido = errors.New("token inválido")

type ctxKey struct{}

// Middleware exige um token Bearer válido e guarda a identidade no contexto.
func Middleware(v Verificador) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
			if !ok || token == "" {
				naoAutenticado(w)
				return
			}
			id, err := v.Verificar(r.Context(), token)
			if err != nil {
				naoAutenticado(w)
				return
			}
			id.token = token
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, id)))
		})
	}
}

// DoContexto devolve a identidade guardada pelo Middleware.
func DoContexto(ctx context.Context) (Identidade, bool) {
	id, ok := ctx.Value(ctxKey{}).(Identidade)
	return id, ok
}

// ComIdentidade põe uma identidade no contexto. Serve para testes.
func ComIdentidade(ctx context.Context, id Identidade) context.Context {
	return context.WithValue(ctx, ctxKey{}, id)
}

func naoAutenticado(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("WWW-Authenticate", `Bearer`)
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write([]byte(`{"codigo":"nao_autenticado","mensagem":"Faça login para continuar."}` + "\n"))
}
