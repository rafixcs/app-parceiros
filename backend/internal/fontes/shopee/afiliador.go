package shopee

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/rafixcs/app-parceiros/backend/internal/fontes"
)

// Afiliador gera links de afiliado com a credencial de cada usuário
// (fontes.Afiliador).
type Afiliador struct {
	Credenciais *Credenciais
	Cliente     *Cliente
}

var _ fontes.Afiliador = Afiliador{}

func (a Afiliador) Conectado(ctx context.Context, usuarioID uuid.UUID) (bool, error) {
	c, err := a.Credenciais.Ver(ctx, usuarioID)
	if err != nil {
		return false, err
	}
	return c.Status == StatusConectado, nil
}

// GerarLink chama o generateShortLink com a credencial do usuário. Se a
// Shopee recusar a credencial, marca a conexão como inválida ou expirada, e o
// erro devolvido é fontes.ErrCredencialInvalida ou fontes.ErrAcessoNegado.
func (a Afiliador) GerarLink(ctx context.Context, usuarioID uuid.UUID, origem string, subIDs []string) (string, error) {
	cred, err := a.Credenciais.DoUsuario(ctx, usuarioID)
	if err != nil {
		return "", err
	}
	link, err := a.Cliente.GerarLink(ctx, cred, origem, subIDs)
	if errors.Is(err, fontes.ErrCredencialInvalida) || errors.Is(err, fontes.ErrAcessoNegado) {
		if errReg := a.Credenciais.RegistrarFalha(ctx, usuarioID, err); errReg != nil {
			return "", errors.Join(err, errReg)
		}
	}
	return link, err
}
