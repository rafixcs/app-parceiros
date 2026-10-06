-- Queries of the internal identity provider. Generate the code with `make sqlc`.

-- name: CreateAuthAccount :one
INSERT INTO auth_accounts (email, name, password_hash)
VALUES ($1, $2, $3)
RETURNING *;

-- name: AuthAccountByEmail :one
SELECT * FROM auth_accounts WHERE email = $1;

-- name: DeleteExpiredAuthSessions :exec
DELETE FROM auth_sessions WHERE account_id = $1 AND expires_at <= now();

-- name: CreateAuthSession :exec
INSERT INTO auth_sessions (token_hash, account_id, created_at, last_seen_at, expires_at)
VALUES ($1, $2, $3, $4, $5);

-- name: AuthSessionByHash :one
SELECT sqlc.embed(s), sqlc.embed(a)
FROM auth_sessions s
JOIN auth_accounts a ON a.id = s.account_id
WHERE s.token_hash = $1;

-- name: ExtendAuthSession :exec
UPDATE auth_sessions SET last_seen_at = $2, expires_at = $3 WHERE token_hash = $1;

-- name: DeleteAuthSession :execrows
DELETE FROM auth_sessions WHERE token_hash = $1;

-- name: DeleteAuthSessionsOfAccount :exec
DELETE FROM auth_sessions WHERE account_id = $1;

-- name: CreateAuthToken :exec
INSERT INTO auth_tokens (token_hash, account_id, purpose, expires_at)
VALUES ($1, $2, $3, $4);

-- name: UseAuthToken :one
-- Consumes a valid token of the purpose and returns its account.
UPDATE auth_tokens SET used_at = sqlc.arg(now)::timestamptz
WHERE token_hash = sqlc.arg(token_hash)
  AND purpose = sqlc.arg(purpose)
  AND used_at IS NULL
  AND expires_at > sqlc.arg(now)::timestamptz
RETURNING account_id;

-- name: MarkAuthEmailVerified :one
UPDATE auth_accounts
SET email_verified_at = coalesce(email_verified_at, sqlc.arg(now)::timestamptz), updated_at = sqlc.arg(now)::timestamptz
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: SetAuthPassword :one
UPDATE auth_accounts
SET password_hash = sqlc.arg(password_hash),
    email_verified_at = coalesce(email_verified_at, sqlc.arg(now)::timestamptz),
    updated_at = sqlc.arg(now)::timestamptz
WHERE id = sqlc.arg(id)
RETURNING *;
