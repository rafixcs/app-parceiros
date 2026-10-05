-- Queries do módulo curadoria. Toda query filtra por workspace_id, além da RLS
-- (que esconde os rascunhos de quem não é dono nem mentor).

-- name: ContarListas :one
SELECT count(*) FROM listas_curadoria WHERE workspace_id = @workspace_id;

-- name: CriarLista :one
INSERT INTO listas_curadoria (workspace_id, autor_id, titulo, descricao)
VALUES (@workspace_id, @autor_id, @titulo, @descricao)
RETURNING id;

-- name: Listas :many
-- Os rascunhos só voltam com rascunhos = true (e a RLS só os mostra a
-- dono e mentor). importadores conta a turma; importei, o próprio usuário.
SELECT l.id, l.titulo, l.descricao, l.publicada_em, l.criado_em, l.atualizado_em,
    (SELECT count(*) FROM lista_itens li
     WHERE li.lista_id = l.id AND li.workspace_id = @workspace_id) AS produtos,
    (SELECT count(DISTINCT i.usuario_id) FROM importacoes i
     WHERE i.lista_id = l.id AND i.workspace_id = @workspace_id) AS importadores,
    EXISTS (SELECT 1 FROM importacoes i
            WHERE i.lista_id = l.id AND i.workspace_id = @workspace_id AND i.usuario_id = @usuario_id) AS importei
FROM listas_curadoria l
WHERE l.workspace_id = @workspace_id
  AND (sqlc.arg(rascunhos)::boolean OR l.publicada_em IS NOT NULL)
ORDER BY l.publicada_em IS NOT NULL, coalesce(l.publicada_em, l.criado_em) DESC, l.id;

-- name: Lista :one
SELECT l.id, l.titulo, l.descricao, l.publicada_em, l.criado_em, l.atualizado_em,
    (SELECT count(*) FROM lista_itens li
     WHERE li.lista_id = l.id AND li.workspace_id = @workspace_id) AS produtos,
    (SELECT count(DISTINCT i.usuario_id) FROM importacoes i
     WHERE i.lista_id = l.id AND i.workspace_id = @workspace_id) AS importadores,
    EXISTS (SELECT 1 FROM importacoes i
            WHERE i.lista_id = l.id AND i.workspace_id = @workspace_id AND i.usuario_id = @usuario_id) AS importei
FROM listas_curadoria l
WHERE l.id = @id AND l.workspace_id = @workspace_id
  AND (sqlc.arg(rascunhos)::boolean OR l.publicada_em IS NOT NULL);

-- name: TravarLista :one
SELECT id, publicada_em FROM listas_curadoria
WHERE id = @id AND workspace_id = @workspace_id
FOR UPDATE;

-- name: AtualizarLista :execrows
UPDATE listas_curadoria SET
    titulo = coalesce(sqlc.narg('titulo'), titulo),
    descricao = coalesce(sqlc.narg('descricao'), descricao),
    atualizado_em = now()
WHERE id = @id AND workspace_id = @workspace_id;

-- name: PublicarLista :execrows
-- Só a primeira publicação conta: publicar de novo não muda nada.
UPDATE listas_curadoria SET publicada_em = now(), atualizado_em = now()
WHERE id = @id AND workspace_id = @workspace_id AND publicada_em IS NULL;

-- name: ApagarLista :execrows
DELETE FROM listas_curadoria WHERE id = @id AND workspace_id = @workspace_id;

-- name: ItensDaLista :many
SELECT produto_id, comentario, ordem FROM lista_itens
WHERE lista_id = @lista_id AND workspace_id = @workspace_id
ORDER BY ordem, produto_id;

-- name: AdicionarItemLista :execrows
INSERT INTO lista_itens (lista_id, workspace_id, produto_id, comentario, ordem)
SELECT @lista_id, @workspace_id, @produto_id, @comentario, coalesce(max(ordem), 0) + 1
FROM lista_itens WHERE lista_id = @lista_id AND workspace_id = @workspace_id
ON CONFLICT (lista_id, produto_id) DO NOTHING;

-- name: ComentarItemLista :execrows
UPDATE lista_itens SET comentario = @comentario
WHERE lista_id = @lista_id AND workspace_id = @workspace_id AND produto_id = @produto_id;

-- name: RemoverItemLista :execrows
DELETE FROM lista_itens
WHERE lista_id = @lista_id AND workspace_id = @workspace_id AND produto_id = @produto_id;

-- name: OrdenarItensLista :execrows
UPDATE lista_itens li SET ordem = o.ordem
FROM unnest(@produto_ids::uuid[]) WITH ORDINALITY AS o(produto_id, ordem)
WHERE li.lista_id = @lista_id AND li.workspace_id = @workspace_id AND li.produto_id = o.produto_id;

-- name: TocarLista :exec
UPDATE listas_curadoria SET atualizado_em = now() WHERE id = @id AND workspace_id = @workspace_id;

-- name: RegistrarImportacao :exec
INSERT INTO importacoes (lista_id, workspace_id, usuario_id, produto_id)
SELECT @lista_id, @workspace_id, @usuario_id, unnest(@produto_ids::uuid[])
ON CONFLICT DO NOTHING;

-- name: MinhasImportacoes :many
SELECT produto_id FROM importacoes
WHERE lista_id = @lista_id AND workspace_id = @workspace_id AND usuario_id = @usuario_id;

-- name: ImportacoesPorProduto :many
SELECT produto_id, count(*) AS importadores FROM importacoes
WHERE lista_id = @lista_id AND workspace_id = @workspace_id
GROUP BY produto_id;

-- name: Importadores :many
SELECT usuario_id, count(*) AS produtos, max(importado_em)::timestamptz AS ultima_em FROM importacoes
WHERE lista_id = @lista_id AND workspace_id = @workspace_id
GROUP BY usuario_id
ORDER BY ultima_em DESC;

-- name: ImportacoesPublicadas :many
-- Todas as importações das listas publicadas, para os resultados por lista.
SELECT i.lista_id, i.usuario_id, i.produto_id, i.importado_em
FROM importacoes i
JOIN listas_curadoria l ON l.id = i.lista_id AND l.workspace_id = i.workspace_id
WHERE i.workspace_id = @workspace_id AND l.publicada_em IS NOT NULL;
