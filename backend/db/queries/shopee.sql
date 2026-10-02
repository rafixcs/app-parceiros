-- name: CredencialDoUsuario :one
SELECT * FROM credenciais_shopee WHERE usuario_id = $1;

-- name: SalvarCredencial :one
INSERT INTO credenciais_shopee (usuario_id, app_id, secret_cifrado, dek_cifrada, kek_id, status, verificado_em)
VALUES ($1, $2, $3, $4, $5, 'conectado', $6)
ON CONFLICT (usuario_id) DO UPDATE SET
    app_id = excluded.app_id,
    secret_cifrado = excluded.secret_cifrado,
    dek_cifrada = excluded.dek_cifrada,
    kek_id = excluded.kek_id,
    status = 'conectado',
    verificado_em = excluded.verificado_em,
    atualizado_em = now()
RETURNING *;

-- name: ApagarCredencial :exec
DELETE FROM credenciais_shopee WHERE usuario_id = $1;

-- name: MarcarStatusCredencial :execrows
UPDATE credenciais_shopee
SET status = $2, verificado_em = now(), atualizado_em = now()
WHERE usuario_id = $1;
