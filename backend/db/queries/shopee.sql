-- name: ShopeeCredentialByUser :one
SELECT * FROM shopee_credentials WHERE user_id = $1;

-- name: SaveShopeeCredential :one
INSERT INTO shopee_credentials (user_id, app_id, encrypted_secret, encrypted_dek, kek_id, status, verified_at)
VALUES ($1, $2, $3, $4, $5, 'connected', $6)
ON CONFLICT (user_id) DO UPDATE SET
    app_id = excluded.app_id,
    encrypted_secret = excluded.encrypted_secret,
    encrypted_dek = excluded.encrypted_dek,
    kek_id = excluded.kek_id,
    status = 'connected',
    verified_at = excluded.verified_at,
    updated_at = now()
RETURNING *;

-- name: DeleteShopeeCredential :exec
DELETE FROM shopee_credentials WHERE user_id = $1;

-- name: SetShopeeCredentialStatus :execrows
UPDATE shopee_credentials
SET status = $2, verified_at = now(), updated_at = now()
WHERE user_id = $1;

-- name: ConnectedShopeeUsers :many
-- Only the worker (owner role of the tables) uses it, to schedule the sync.
SELECT user_id FROM shopee_credentials WHERE status = 'connected' ORDER BY user_id;
