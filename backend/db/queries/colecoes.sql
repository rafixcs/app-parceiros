-- Toda query filtra por workspace_id e usuario_id, além da RLS.

-- name: CriarItem :one
INSERT INTO itens_colecao (workspace_id, usuario_id, produto_id, titulo, link_status)
VALUES (@workspace_id, @usuario_id, @produto_id, @titulo, @link_status)
ON CONFLICT (workspace_id, usuario_id, produto_id) DO NOTHING
RETURNING *;

-- name: ItemPorProduto :one
SELECT * FROM itens_colecao
WHERE workspace_id = @workspace_id AND usuario_id = @usuario_id AND produto_id = @produto_id;

-- name: Item :one
SELECT * FROM itens_colecao
WHERE id = @id AND workspace_id = @workspace_id AND usuario_id = @usuario_id;

-- name: ListarItens :many
SELECT i.*, count(*) OVER () AS total
FROM itens_colecao i
WHERE i.workspace_id = @workspace_id AND i.usuario_id = @usuario_id
  AND (sqlc.narg('status')::item_status IS NULL OR i.status = sqlc.narg('status'))
  AND (sqlc.narg('tag')::text IS NULL OR sqlc.narg('tag') = ANY (i.tags))
  AND (sqlc.narg('busca')::text IS NULL
       OR i.titulo ILIKE '%' || sqlc.narg('busca') || '%'
       OR i.descricao ILIKE '%' || sqlc.narg('busca') || '%'
       OR i.notas ILIKE '%' || sqlc.narg('busca') || '%')
  AND (sqlc.narg('colecao_id')::uuid IS NULL OR EXISTS (
       SELECT 1 FROM colecao_itens ci
       WHERE ci.item_id = i.id AND ci.colecao_id = sqlc.narg('colecao_id')
         AND ci.workspace_id = @workspace_id AND ci.usuario_id = @usuario_id))
ORDER BY i.criado_em DESC, i.id
LIMIT @limite OFFSET @desloc;

-- name: AtualizarItem :one
UPDATE itens_colecao SET
    titulo = coalesce(sqlc.narg('titulo'), titulo),
    descricao = coalesce(sqlc.narg('descricao'), descricao),
    notas = coalesce(sqlc.narg('notas'), notas),
    tags = coalesce(sqlc.narg('tags')::text[], tags),
    status = coalesce(sqlc.narg('status')::item_status, status),
    atualizado_em = now()
WHERE id = @id AND workspace_id = @workspace_id AND usuario_id = @usuario_id
RETURNING *;

-- name: DefinirLinkManual :one
UPDATE itens_colecao SET
    link_afiliado = @link_afiliado, link_origem = 'manual', link_status = 'pronto', atualizado_em = now()
WHERE id = @id AND workspace_id = @workspace_id AND usuario_id = @usuario_id
RETURNING *;

-- name: PrepararLinkAuto :one
-- Volta ao link automático: apaga o atual e espera o job gerar_link.
UPDATE itens_colecao SET
    link_afiliado = NULL, link_origem = 'auto', link_status = @link_status, atualizado_em = now()
WHERE id = @id AND workspace_id = @workspace_id AND usuario_id = @usuario_id
RETURNING *;

-- name: MarcarLinkStatus :exec
-- Só mexe em links automáticos: um link manual gravado enquanto o job rodava vence.
UPDATE itens_colecao SET link_status = @link_status, atualizado_em = now()
WHERE id = @id AND workspace_id = @workspace_id AND usuario_id = @usuario_id AND link_origem = 'auto';

-- name: ConcluirLink :execrows
UPDATE itens_colecao SET link_afiliado = @link_afiliado, link_status = 'pronto', atualizado_em = now()
WHERE id = @id AND workspace_id = @workspace_id AND usuario_id = @usuario_id AND link_origem = 'auto';

-- name: ItensSemLink :many
-- Links automáticos pendentes (sem credencial) ou que falharam, para gerar de novo.
UPDATE itens_colecao SET link_status = 'gerando', atualizado_em = now()
WHERE workspace_id = @workspace_id AND usuario_id = @usuario_id
  AND link_origem = 'auto' AND link_status IN ('pendente', 'falhou')
RETURNING id;

-- name: RemoverItem :execrows
DELETE FROM itens_colecao WHERE id = @id AND workspace_id = @workspace_id AND usuario_id = @usuario_id;

-- name: ProdutosSalvos :many
SELECT produto_id FROM itens_colecao
WHERE workspace_id = @workspace_id AND usuario_id = @usuario_id
ORDER BY criado_em DESC;

-- name: ApagarLinksCanal :exec
DELETE FROM links_canal WHERE item_id = @item_id AND workspace_id = @workspace_id AND usuario_id = @usuario_id;

-- name: SalvarLinkCanal :exec
INSERT INTO links_canal (item_id, workspace_id, usuario_id, canal, sub_id, url)
VALUES (@item_id, @workspace_id, @usuario_id, @canal, @sub_id, @url)
ON CONFLICT (item_id, canal) DO UPDATE SET sub_id = excluded.sub_id, url = excluded.url, gerado_em = now();

-- name: LinksDosItens :many
SELECT item_id, canal, sub_id, url FROM links_canal
WHERE workspace_id = @workspace_id AND usuario_id = @usuario_id AND item_id = ANY (@ids::uuid[])
ORDER BY item_id, canal;

-- name: ColecoesDosItens :many
SELECT item_id, colecao_id FROM colecao_itens
WHERE workspace_id = @workspace_id AND usuario_id = @usuario_id AND item_id = ANY (@ids::uuid[])
ORDER BY item_id, adicionado_em;

-- name: ListarColecoes :many
SELECT c.id, c.nome, c.criado_em,
    (SELECT count(*) FROM colecao_itens ci
     WHERE ci.colecao_id = c.id AND ci.workspace_id = @workspace_id AND ci.usuario_id = @usuario_id) AS itens
FROM colecoes c
WHERE c.workspace_id = @workspace_id AND c.usuario_id = @usuario_id
ORDER BY lower(c.nome);

-- name: Colecao :one
SELECT c.id, c.nome, c.criado_em,
    (SELECT count(*) FROM colecao_itens ci
     WHERE ci.colecao_id = c.id AND ci.workspace_id = @workspace_id AND ci.usuario_id = @usuario_id) AS itens
FROM colecoes c
WHERE c.id = @id AND c.workspace_id = @workspace_id AND c.usuario_id = @usuario_id;

-- name: CriarColecao :one
INSERT INTO colecoes (workspace_id, usuario_id, nome) VALUES (@workspace_id, @usuario_id, @nome)
RETURNING id;

-- name: RenomearColecao :execrows
UPDATE colecoes SET nome = @nome
WHERE id = @id AND workspace_id = @workspace_id AND usuario_id = @usuario_id;

-- name: ApagarColecao :execrows
DELETE FROM colecoes WHERE id = @id AND workspace_id = @workspace_id AND usuario_id = @usuario_id;

-- name: ContarColecoes :one
SELECT count(*) FROM colecoes
WHERE workspace_id = @workspace_id AND usuario_id = @usuario_id AND id = ANY (@ids::uuid[]);

-- name: LimparColecoesDoItem :exec
DELETE FROM colecao_itens WHERE item_id = @item_id AND workspace_id = @workspace_id AND usuario_id = @usuario_id;

-- name: AdicionarColecaoItem :exec
INSERT INTO colecao_itens (colecao_id, item_id, workspace_id, usuario_id)
VALUES (@colecao_id, @item_id, @workspace_id, @usuario_id)
ON CONFLICT DO NOTHING;

-- name: CriarItemImportado :one
-- Item vindo de uma lista da curadoria, com o comentário do mentor nas notas.
INSERT INTO itens_colecao (workspace_id, usuario_id, produto_id, titulo, notas, link_status)
VALUES (@workspace_id, @usuario_id, @produto_id, @titulo, @notas, @link_status)
ON CONFLICT (workspace_id, usuario_id, produto_id) DO NOTHING
RETURNING *;

-- name: ItensDosProdutos :many
SELECT * FROM itens_colecao
WHERE workspace_id = @workspace_id AND usuario_id = @usuario_id AND produto_id = ANY (@produto_ids::uuid[]);

-- name: ColecaoPorNome :one
SELECT id FROM colecoes
WHERE workspace_id = @workspace_id AND usuario_id = @usuario_id AND lower(nome) = lower(@nome);
