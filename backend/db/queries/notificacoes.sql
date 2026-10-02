-- Queries do módulo notificacoes. A caixa filtra por workspace e usuário;
-- inscrições e preferências, pelo usuário. A RLS repete os filtros.

-- name: CriarNotificacao :one
-- Idempotente pela chave: repetir a entrega devolve a mesma notificação.
INSERT INTO notificacoes (workspace_id, usuario_id, tipo, chave, titulo, corpo, url)
VALUES (@workspace_id, @usuario_id, @tipo, @chave, @titulo, @corpo, @url)
ON CONFLICT (workspace_id, usuario_id, chave) DO UPDATE SET chave = excluded.chave
RETURNING *;

-- name: Caixa :many
SELECT * FROM notificacoes
WHERE workspace_id = @workspace_id AND usuario_id = @usuario_id
ORDER BY criado_em DESC, id
LIMIT @limite;

-- name: ContarNaoLidas :one
SELECT count(*) FROM notificacoes
WHERE workspace_id = @workspace_id AND usuario_id = @usuario_id AND lida_em IS NULL;

-- name: MarcarLida :execrows
UPDATE notificacoes SET lida_em = coalesce(lida_em, now())
WHERE id = @id AND workspace_id = @workspace_id AND usuario_id = @usuario_id;

-- name: MarcarTodasLidas :exec
UPDATE notificacoes SET lida_em = now()
WHERE workspace_id = @workspace_id AND usuario_id = @usuario_id AND lida_em IS NULL;

-- name: MarcarEmail :exec
UPDATE notificacoes SET email_em = now()
WHERE id = @id AND workspace_id = @workspace_id AND usuario_id = @usuario_id;

-- name: MarcarPush :exec
UPDATE notificacoes SET push_em = now()
WHERE id = @id AND workspace_id = @workspace_id AND usuario_id = @usuario_id;

-- name: SalvarInscricao :exec
INSERT INTO push_inscricoes (usuario_id, endpoint, p256dh, auth)
VALUES (@usuario_id, @endpoint, @p256dh, @auth)
ON CONFLICT (usuario_id, endpoint) DO UPDATE SET p256dh = excluded.p256dh, auth = excluded.auth;

-- name: ContarInscricoes :one
SELECT count(*) FROM push_inscricoes WHERE usuario_id = @usuario_id;

-- name: Inscricoes :many
SELECT * FROM push_inscricoes WHERE usuario_id = @usuario_id ORDER BY criado_em;

-- name: ApagarInscricao :execrows
DELETE FROM push_inscricoes WHERE usuario_id = @usuario_id AND endpoint = @endpoint;

-- name: Preferencias :one
SELECT * FROM preferencias_notificacao WHERE usuario_id = @usuario_id;

-- name: SalvarPreferencias :one
INSERT INTO preferencias_notificacao (usuario_id, email)
VALUES (@usuario_id, @email)
ON CONFLICT (usuario_id) DO UPDATE SET email = excluded.email, atualizado_em = now()
RETURNING *;
