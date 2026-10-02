package auth

import (
	"context"
	"fmt"
)

// Novo escolhe o verificador pelo modo: "oidc" (padrão) ou "dev". O modo dev
// é recusado fora de APP_ENV=dev.
func Novo(ctx context.Context, modo, env string, c ConfigOIDC) (Verificador, error) {
	switch modo {
	case "", "oidc":
		return NovoOIDC(ctx, c)
	case "dev":
		if env != "dev" {
			return nil, fmt.Errorf("AUTH_MODE=dev só é permitido com APP_ENV=dev")
		}
		return Dev{}, nil
	default:
		return nil, fmt.Errorf("AUTH_MODE desconhecido %q", modo)
	}
}
