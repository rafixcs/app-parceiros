package repository

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/rafixcs/app-parceiros/backend/internal/domain"
	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/database/dbgen"
)

// PostgresShopeeCredential is the domain.ShopeeCredentialRepository on
// Postgres. Every call but ConnectedShopeeUsers runs in the user's scope: the
// RLS shows each user only their own row.
type PostgresShopeeCredential struct {
	pool *pgxpool.Pool
}

var _ domain.ShopeeCredentialRepository = (*PostgresShopeeCredential)(nil)

func NewPostgresShopeeCredential(pool *pgxpool.Pool) *PostgresShopeeCredential {
	return &PostgresShopeeCredential{pool: pool}
}

func (r *PostgresShopeeCredential) ShopeeCredential(ctx context.Context, userID uuid.UUID) (domain.StoredShopeeCredential, error) {
	var c dbgen.ShopeeCredential
	err := run(ctx, r.pool, userScope(userID), func(q *dbgen.Queries, _ pgx.Tx) error {
		var err error
		c, err = q.ShopeeCredentialByUser(ctx, userID)
		return err
	})
	if err != nil {
		return domain.StoredShopeeCredential{}, notFound(err)
	}
	return shopeeCredentialOf(c), nil
}

func (r *PostgresShopeeCredential) SaveShopeeCredential(ctx context.Context, sc domain.StoredShopeeCredential) (domain.StoredShopeeCredential, error) {
	var c dbgen.ShopeeCredential
	err := run(ctx, r.pool, userScope(sc.UserID), func(q *dbgen.Queries, _ pgx.Tx) error {
		var err error
		c, err = q.SaveShopeeCredential(ctx, dbgen.SaveShopeeCredentialParams{
			UserID:          sc.UserID,
			AppID:           sc.AppID,
			EncryptedSecret: sc.Secret.Ciphertext,
			EncryptedDek:    sc.Secret.EncryptedDEK,
			KekID:           sc.Secret.KEKID,
			VerifiedAt:      sc.VerifiedAt,
		})
		return err
	})
	if err != nil {
		return domain.StoredShopeeCredential{}, err
	}
	return shopeeCredentialOf(c), nil
}

func (r *PostgresShopeeCredential) DeleteShopeeCredential(ctx context.Context, userID uuid.UUID) error {
	return run(ctx, r.pool, userScope(userID), func(q *dbgen.Queries, _ pgx.Tx) error {
		return q.DeleteShopeeCredential(ctx, userID)
	})
}

func (r *PostgresShopeeCredential) SetShopeeCredentialStatus(ctx context.Context, userID uuid.UUID, status domain.ShopeeStatus) error {
	return run(ctx, r.pool, userScope(userID), func(q *dbgen.Queries, _ pgx.Tx) error {
		_, err := q.SetShopeeCredentialStatus(ctx, dbgen.SetShopeeCredentialStatusParams{
			UserID: userID, Status: dbgen.CredentialStatus(status),
		})
		return err
	})
}

// ConnectedShopeeUsers reads with the owner role of the tables (worker only)
// and returns only ids.
func (r *PostgresShopeeCredential) ConnectedShopeeUsers(ctx context.Context) ([]uuid.UUID, error) {
	return dbgen.New(r.pool).ConnectedShopeeUsers(ctx)
}

func shopeeCredentialOf(c dbgen.ShopeeCredential) domain.StoredShopeeCredential {
	return domain.StoredShopeeCredential{
		UserID: c.UserID,
		AppID:  c.AppID,
		Secret: domain.SealedSecret{
			Ciphertext: c.EncryptedSecret, EncryptedDEK: c.EncryptedDek, KEKID: c.KekID,
		},
		Status:     domain.ShopeeStatus(c.Status),
		VerifiedAt: c.VerifiedAt,
	}
}
