package database

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// AppRole is the unprivileged role the application uses to touch customer
// data. It does not bypass RLS, even when the connection is a superuser's.
const AppRole = "parceiros_app"

// Scope holds the session settings read by the RLS policies. Empty fields stay
// unset, and the policies that depend on them grant nothing.
type Scope struct {
	UserID      string // app.user_id
	WorkspaceID string // app.workspace_id
	// AuthProvider and AuthSubject identify the caller at the identity
	// provider, used on first access (app.auth_provider, app.auth_subject).
	AuthProvider string
	AuthSubject  string
	InviteHash   string // app.invite_hash (hex)
	// ExternalSubscriptionID is the subscription id at the payment gateway,
	// used by the webhook to find its workspace (app.external_subscription_id).
	ExternalSubscriptionID string
	// AuthEmail, SessionHash and AuthTokenHash scope the internal identity
	// provider: the account being signed in, the session being checked and
	// the one-time token being used (app.auth_email, app.session_hash and
	// app.auth_token_hash, hashes in hex).
	AuthEmail     string
	SessionHash   string
	AuthTokenHash string
}

// InTx runs fn in a transaction with the AppRole role and the scope settings.
// Every read and write of customer data goes through here.
func InTx(ctx context.Context, pool *pgxpool.Pool, s Scope, fn func(pgx.Tx) error) error {
	return pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SET LOCAL ROLE "+AppRole); err != nil {
			return fmt.Errorf("assuming role %s: %w", AppRole, err)
		}
		if err := SetScope(ctx, tx, s); err != nil {
			return err
		}
		return fn(tx)
	})
}

// SetScope replaces the session settings inside an open transaction, for
// instance after finding the workspace of an invite.
func SetScope(ctx context.Context, tx pgx.Tx, s Scope) error {
	_, err := tx.Exec(ctx, `SELECT
		set_config('app.user_id', $1, true),
		set_config('app.workspace_id', $2, true),
		set_config('app.auth_provider', $3, true),
		set_config('app.auth_subject', $4, true),
		set_config('app.invite_hash', $5, true),
		set_config('app.external_subscription_id', $6, true),
		set_config('app.auth_email', $7, true),
		set_config('app.session_hash', $8, true),
		set_config('app.auth_token_hash', $9, true)`,
		s.UserID, s.WorkspaceID, s.AuthProvider, s.AuthSubject, s.InviteHash,
		s.ExternalSubscriptionID, s.AuthEmail, s.SessionHash, s.AuthTokenHash)
	if err != nil {
		return fmt.Errorf("setting transaction scope: %w", err)
	}
	return nil
}
