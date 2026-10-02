package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// AppRole é o papel sem privilégios com que a API acessa os dados de cliente.
// Ele não ignora RLS, mesmo quando a conexão é de um superusuário.
const AppRole = "parceiros_app"

// Escopo são as variáveis de sessão lidas pelas políticas de RLS. Campos
// vazios ficam sem valor, e as políticas que dependem deles não liberam nada.
type Escopo struct {
	UsuarioID   string // app.usuario_id
	WorkspaceID string // app.workspace_id
	ZitadelSub  string // app.zitadel_sub
	ConviteHash string // app.convite_hash (hex)
}

// InTx roda fn numa transação com o papel AppRole e as variáveis do escopo.
// Toda leitura e escrita de dados de cliente passa por aqui.
func InTx(ctx context.Context, pool *pgxpool.Pool, e Escopo, fn func(pgx.Tx) error) error {
	return pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SET LOCAL ROLE "+AppRole); err != nil {
			return fmt.Errorf("assumindo papel %s: %w", AppRole, err)
		}
		if err := SetEscopo(ctx, tx, e); err != nil {
			return err
		}
		return fn(tx)
	})
}

// SetEscopo troca as variáveis de sessão dentro de uma transação já aberta,
// por exemplo depois de descobrir o workspace de um convite.
func SetEscopo(ctx context.Context, tx pgx.Tx, e Escopo) error {
	_, err := tx.Exec(ctx, `SELECT
		set_config('app.usuario_id', $1, true),
		set_config('app.workspace_id', $2, true),
		set_config('app.zitadel_sub', $3, true),
		set_config('app.convite_hash', $4, true)`,
		e.UsuarioID, e.WorkspaceID, e.ZitadelSub, e.ConviteHash)
	if err != nil {
		return fmt.Errorf("definindo escopo da transação: %w", err)
	}
	return nil
}
