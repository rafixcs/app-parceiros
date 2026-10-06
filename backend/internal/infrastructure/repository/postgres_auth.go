package repository

import (
	"context"
	"encoding/hex"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/rafixcs/app-parceiros/backend/internal/domain"
	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/database"
	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/database/dbgen"
)

// PostgresAuth is the domain.AuthRepository on Postgres.
type PostgresAuth struct {
	pool *pgxpool.Pool
}

var _ domain.AuthRepository = (*PostgresAuth)(nil)

func NewPostgresAuth(pool *pgxpool.Pool) *PostgresAuth { return &PostgresAuth{pool: pool} }

func (r *PostgresAuth) tx(ctx context.Context, s database.Scope, fn func(*dbgen.Queries, pgx.Tx) error) error {
	return run(ctx, r.pool, s, fn)
}

// accountScope is the scope of a signed-in internal account.
func accountScope(id uuid.UUID) database.Scope {
	return database.Scope{AuthProvider: domain.AuthProviderInternal, AuthSubject: id.String()}
}

func (r *PostgresAuth) CreateAccount(ctx context.Context, a domain.AuthAccount) (domain.AuthAccount, error) {
	var row dbgen.AuthAccount
	err := r.tx(ctx, database.Scope{AuthEmail: a.Email}, func(q *dbgen.Queries, _ pgx.Tx) error {
		var err error
		row, err = q.CreateAuthAccount(ctx, dbgen.CreateAuthAccountParams{
			Email: a.Email, Name: a.Name, PasswordHash: a.PasswordHash,
		})
		return err
	})
	if isUniqueViolation(err) {
		return domain.AuthAccount{}, domain.ErrEmailTaken
	}
	return authAccountOf(row), err
}

func (r *PostgresAuth) AccountByEmail(ctx context.Context, email string) (domain.AuthAccount, error) {
	var row dbgen.AuthAccount
	err := r.tx(ctx, database.Scope{AuthEmail: email}, func(q *dbgen.Queries, _ pgx.Tx) error {
		var err error
		row, err = q.AuthAccountByEmail(ctx, email)
		return err
	})
	return authAccountOf(row), notFound(err)
}

func (r *PostgresAuth) CreateSession(ctx context.Context, s domain.AuthSession) error {
	return r.tx(ctx, accountScope(s.AccountID), func(q *dbgen.Queries, _ pgx.Tx) error {
		if err := q.DeleteExpiredAuthSessions(ctx, s.AccountID); err != nil {
			return err
		}
		return q.CreateAuthSession(ctx, dbgen.CreateAuthSessionParams{
			TokenHash: s.TokenHash, AccountID: s.AccountID,
			CreatedAt: s.CreatedAt, LastSeenAt: s.LastSeenAt, ExpiresAt: s.ExpiresAt,
		})
	})
}

func (r *PostgresAuth) SessionByHash(ctx context.Context, hash []byte) (domain.AuthSession, domain.AuthAccount, error) {
	var row dbgen.AuthSessionByHashRow
	err := r.tx(ctx, database.Scope{SessionHash: hex.EncodeToString(hash)}, func(q *dbgen.Queries, _ pgx.Tx) error {
		var err error
		row, err = q.AuthSessionByHash(ctx, hash)
		return err
	})
	if err != nil {
		return domain.AuthSession{}, domain.AuthAccount{}, notFound(err)
	}
	s := row.AuthSession
	return domain.AuthSession{
		TokenHash: s.TokenHash, AccountID: s.AccountID,
		CreatedAt: s.CreatedAt, LastSeenAt: s.LastSeenAt, ExpiresAt: s.ExpiresAt,
	}, authAccountOf(row.AuthAccount), nil
}

func (r *PostgresAuth) ExtendSession(ctx context.Context, hash []byte, lastSeen, expires time.Time) error {
	return r.tx(ctx, database.Scope{SessionHash: hex.EncodeToString(hash)}, func(q *dbgen.Queries, _ pgx.Tx) error {
		return q.ExtendAuthSession(ctx, dbgen.ExtendAuthSessionParams{TokenHash: hash, LastSeenAt: lastSeen, ExpiresAt: expires})
	})
}

func (r *PostgresAuth) DeleteSession(ctx context.Context, hash []byte) error {
	return r.tx(ctx, database.Scope{SessionHash: hex.EncodeToString(hash)}, func(q *dbgen.Queries, _ pgx.Tx) error {
		n, err := q.DeleteAuthSession(ctx, hash)
		if err == nil && n == 0 {
			return domain.ErrNotFound
		}
		return err
	})
}

func (r *PostgresAuth) CreateToken(ctx context.Context, t domain.AuthToken) error {
	return r.tx(ctx, accountScope(t.AccountID), func(q *dbgen.Queries, _ pgx.Tx) error {
		return q.CreateAuthToken(ctx, dbgen.CreateAuthTokenParams{
			TokenHash: t.TokenHash, AccountID: t.AccountID, Purpose: string(t.Purpose), ExpiresAt: t.ExpiresAt,
		})
	})
}

func (r *PostgresAuth) VerifyEmail(ctx context.Context, tokenHash []byte, now time.Time) (domain.AuthAccount, error) {
	var row dbgen.AuthAccount
	err := r.useToken(ctx, tokenHash, domain.TokenVerifyEmail, now, func(q *dbgen.Queries, accountID uuid.UUID) error {
		var err error
		row, err = q.MarkAuthEmailVerified(ctx, dbgen.MarkAuthEmailVerifiedParams{ID: accountID, Now: now})
		return err
	})
	return authAccountOf(row), err
}

func (r *PostgresAuth) ResetPassword(ctx context.Context, tokenHash []byte, passwordHash string, now time.Time) (domain.AuthAccount, error) {
	var row dbgen.AuthAccount
	err := r.useToken(ctx, tokenHash, domain.TokenResetPassword, now, func(q *dbgen.Queries, accountID uuid.UUID) error {
		var err error
		row, err = q.SetAuthPassword(ctx, dbgen.SetAuthPasswordParams{ID: accountID, PasswordHash: passwordHash, Now: now})
		if err != nil {
			return err
		}
		return q.DeleteAuthSessionsOfAccount(ctx, accountID)
	})
	return authAccountOf(row), err
}

// useToken consumes a one-time token and runs fn in the same transaction,
// scoped to the token's account.
func (r *PostgresAuth) useToken(ctx context.Context, hash []byte, purpose domain.AuthTokenPurpose, now time.Time,
	fn func(q *dbgen.Queries, accountID uuid.UUID) error,
) error {
	err := r.tx(ctx, database.Scope{AuthTokenHash: hex.EncodeToString(hash)}, func(q *dbgen.Queries, tx pgx.Tx) error {
		accountID, err := q.UseAuthToken(ctx, dbgen.UseAuthTokenParams{TokenHash: hash, Purpose: string(purpose), Now: now})
		if err != nil {
			return err
		}
		if err := database.SetScope(ctx, tx, accountScope(accountID)); err != nil {
			return err
		}
		return fn(q, accountID)
	})
	return notFound(err)
}

func authAccountOf(a dbgen.AuthAccount) domain.AuthAccount {
	return domain.AuthAccount{
		ID: a.ID, Email: a.Email, Name: a.Name, PasswordHash: a.PasswordHash,
		EmailVerifiedAt: a.EmailVerifiedAt, CreatedAt: a.CreatedAt,
	}
}

// notFound turns pgx.ErrNoRows into domain.ErrNotFound.
