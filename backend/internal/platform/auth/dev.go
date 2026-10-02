package auth

import (
	"context"
	"strings"
)

// Dev aceita tokens no formato `dev:<sub>` sem assinatura, para o ambiente
// local sem Zitadel. O e-mail vira `<sub>@dev.local`. Só pode ser ligado com
// APP_ENV=dev (veja Novo).
type Dev struct{}

func (Dev) Verificar(_ context.Context, token string) (Identidade, error) {
	sub, ok := strings.CutPrefix(token, "dev:")
	if !ok || sub == "" || strings.ContainsAny(sub, " \t") {
		return Identidade{}, ErrTokenInvalido
	}
	return Identidade{Sub: sub, Email: sub + "@dev.local", EmailVerificado: true, Nome: sub}, nil
}

func (Dev) Perfil(_ context.Context, id Identidade) (Perfil, error) {
	return Perfil{Email: id.Email, EmailVerificado: id.EmailVerificado, Nome: id.Nome}, nil
}
