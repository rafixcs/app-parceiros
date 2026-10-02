-- Queries do módulo midia. Toda query filtra por workspace_id, além da RLS
-- (que mostra ao usuário os vídeos dele e os compartilhados no workspace).

-- name: CriarEmbed :one
-- Colar de novo o mesmo vídeo devolve o que já existe (xmax <> 0 no upsert).
INSERT INTO videos (workspace_id, dono_id, tipo, plataforma, status, titulo, autor, url, embed_id, thumb_url, verificado_em)
VALUES (@workspace_id, @dono_id, 'embed', @plataforma, 'pronto', @titulo, @autor, @url, @embed_id, @thumb_url, now())
ON CONFLICT (workspace_id, dono_id, plataforma, embed_id) WHERE tipo = 'embed'
DO UPDATE SET titulo = excluded.titulo, autor = excluded.autor, url = excluded.url,
    thumb_url = excluded.thumb_url, status = 'pronto', verificado_em = now(), atualizado_em = now()
RETURNING id, (xmax = 0)::boolean AS criado;

-- name: CriarUpload :one
INSERT INTO videos (id, workspace_id, dono_id, tipo, plataforma, status, titulo, storage_key, upload_id,
                    nome_arquivo, content_type, tamanho_bytes, direito_uso_em)
VALUES (@id, @workspace_id, @dono_id, 'upload', 'upload', 'enviando', @titulo, @storage_key, @upload_id,
        @nome_arquivo, @content_type, @tamanho_bytes, now())
RETURNING *;

-- name: Video :one
SELECT * FROM videos WHERE id = @id AND workspace_id = @workspace_id;

-- name: TravarVideo :one
SELECT * FROM videos WHERE id = @id AND workspace_id = @workspace_id AND dono_id = @dono_id FOR UPDATE;

-- name: Videos :many
-- Os vídeos que o usuário vê no workspace: os dele e os compartilhados.
SELECT * FROM videos
WHERE workspace_id = @workspace_id
  AND (NOT sqlc.arg(so_meus)::boolean OR dono_id = @dono_id)
ORDER BY criado_em DESC, id
LIMIT 500;

-- name: VideosDosAlvos :many
-- Os vídeos ligados a algum destes alvos, com o alvo de cada ligação.
SELECT sqlc.embed(v), vv.alvo_tipo, vv.alvo_id FROM video_vinculos vv
JOIN videos v ON v.id = vv.video_id AND v.workspace_id = vv.workspace_id
WHERE vv.workspace_id = @workspace_id
  AND vv.alvo_tipo = @alvo_tipo
  AND vv.alvo_id = ANY(@alvo_ids::uuid[])
ORDER BY vv.criado_em, v.id;

-- name: VinculosDosVideos :many
SELECT video_id, alvo_tipo, alvo_id FROM video_vinculos
WHERE workspace_id = @workspace_id AND video_id = ANY(@video_ids::uuid[])
ORDER BY criado_em;

-- name: Vincular :execrows
INSERT INTO video_vinculos (video_id, workspace_id, dono_id, alvo_tipo, alvo_id)
VALUES (@video_id, @workspace_id, @dono_id, @alvo_tipo, @alvo_id)
ON CONFLICT DO NOTHING;

-- name: Desvincular :execrows
DELETE FROM video_vinculos
WHERE workspace_id = @workspace_id AND video_id = @video_id AND alvo_tipo = @alvo_tipo AND alvo_id = @alvo_id;

-- name: DesvincularAlvo :exec
DELETE FROM video_vinculos WHERE workspace_id = @workspace_id AND alvo_tipo = @alvo_tipo AND alvo_id = @alvo_id;

-- name: Compartilhar :execrows
UPDATE videos SET compartilhado = @compartilhado, atualizado_em = now()
WHERE id = @id AND workspace_id = @workspace_id AND dono_id = @dono_id;

-- name: Renomear :execrows
UPDATE videos SET titulo = @titulo, atualizado_em = now()
WHERE id = @id AND workspace_id = @workspace_id AND dono_id = @dono_id;

-- name: ConcluirUpload :exec
UPDATE videos SET status = 'processando', upload_id = NULL, tamanho_bytes = @tamanho_bytes, atualizado_em = now()
WHERE id = @id AND workspace_id = @workspace_id AND dono_id = @dono_id;

-- name: Processado :exec
UPDATE videos SET status = 'pronto', duracao_s = @duracao_s, largura = @largura, altura = @altura, atualizado_em = now()
WHERE id = @id AND workspace_id = @workspace_id AND dono_id = @dono_id;

-- name: MarcarStatus :exec
UPDATE videos SET status = @status, atualizado_em = now()
WHERE id = @id AND workspace_id = @workspace_id AND dono_id = @dono_id;

-- name: Revalidado :exec
UPDATE videos SET status = @status, titulo = coalesce(sqlc.narg('titulo'), titulo),
    autor = coalesce(sqlc.narg('autor'), autor), thumb_url = coalesce(sqlc.narg('thumb_url'), thumb_url),
    verificado_em = now(), atualizado_em = now()
WHERE id = @id AND workspace_id = @workspace_id AND dono_id = @dono_id;

-- name: ApagarVideo :execrows
DELETE FROM videos WHERE id = @id AND workspace_id = @workspace_id AND dono_id = @dono_id;

-- name: SomarUso :one
-- Soma (ou subtrai) bytes do uso do workspace e devolve o novo total.
INSERT INTO uso_videos (workspace_id, bytes) VALUES (@workspace_id, greatest(sqlc.arg(delta)::bigint, 0))
ON CONFLICT (workspace_id) DO UPDATE SET bytes = greatest(uso_videos.bytes + sqlc.arg(delta)::bigint, 0)
RETURNING bytes;

-- name: Uso :one
SELECT coalesce((SELECT bytes FROM uso_videos WHERE workspace_id = @workspace_id), 0)::bigint;
