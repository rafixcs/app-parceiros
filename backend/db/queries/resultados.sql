-- Queries do módulo resultados. As leituras filtram por workspace e pelos
-- usuários pedidos (o próprio afiliado, ou a turma que consentiu), além da RLS.
-- Os dias contam no fuso de Brasília.

-- name: SalvarConversao :exec
INSERT INTO conversoes (
    usuario_id, workspace_id, fonte, conversao_id, pedido_id, item_id, modelo_id, produto_id,
    item_nome, loja_nome, sub_id, canal, status, quantidade, valor_centavos, comissao_centavos,
    ocorrido_em, clicado_em
) VALUES (
    @usuario_id, @workspace_id, @fonte, @conversao_id, @pedido_id, @item_id, @modelo_id, @produto_id,
    @item_nome, @loja_nome, @sub_id, sqlc.narg(canal), @status, @quantidade, @valor_centavos, @comissao_centavos,
    @ocorrido_em, @clicado_em
)
ON CONFLICT (usuario_id, fonte, pedido_id, item_id, modelo_id) DO UPDATE SET
    workspace_id = excluded.workspace_id,
    conversao_id = excluded.conversao_id,
    produto_id = coalesce(excluded.produto_id, conversoes.produto_id),
    item_nome = excluded.item_nome,
    loja_nome = excluded.loja_nome,
    sub_id = excluded.sub_id,
    canal = excluded.canal,
    status = excluded.status,
    quantidade = excluded.quantidade,
    valor_centavos = excluded.valor_centavos,
    comissao_centavos = excluded.comissao_centavos,
    ocorrido_em = excluded.ocorrido_em,
    clicado_em = excluded.clicado_em,
    sincronizado_em = now();

-- name: Totais :one
SELECT
    count(DISTINCT pedido_id) FILTER (WHERE status <> 'cancelado')::bigint AS pedidos,
    count(DISTINCT pedido_id) FILTER (WHERE status = 'cancelado')::bigint AS cancelados,
    coalesce(sum(quantidade) FILTER (WHERE status <> 'cancelado'), 0)::bigint AS itens,
    coalesce(sum(valor_centavos) FILTER (WHERE status <> 'cancelado'), 0)::bigint AS vendas_centavos,
    coalesce(sum(comissao_centavos) FILTER (WHERE status <> 'cancelado'), 0)::bigint AS comissao_estimada_centavos,
    coalesce(sum(comissao_centavos) FILTER (WHERE status = 'concluido'), 0)::bigint AS comissao_validada_centavos,
    count(DISTINCT usuario_id) FILTER (WHERE status <> 'cancelado')::bigint AS afiliados
FROM conversoes
WHERE workspace_id = @workspace_id
  AND usuario_id = ANY(@usuarios::uuid[])
  AND ocorrido_em >= @de AND ocorrido_em < @ate;

-- name: PorDia :many
SELECT
    (ocorrido_em AT TIME ZONE 'America/Sao_Paulo')::date AS dia,
    count(DISTINCT pedido_id) FILTER (WHERE status <> 'cancelado')::bigint AS pedidos,
    coalesce(sum(comissao_centavos) FILTER (WHERE status <> 'cancelado'), 0)::bigint AS comissao_estimada_centavos,
    coalesce(sum(comissao_centavos) FILTER (WHERE status = 'concluido'), 0)::bigint AS comissao_validada_centavos
FROM conversoes
WHERE workspace_id = @workspace_id
  AND usuario_id = ANY(@usuarios::uuid[])
  AND ocorrido_em >= @de AND ocorrido_em < @ate
GROUP BY dia
ORDER BY dia;

-- name: PorProduto :many
SELECT
    item_id,
    max(produto_id::text) AS produto_id,
    max(item_nome)::text AS item_nome,
    max(loja_nome)::text AS loja_nome,
    count(DISTINCT pedido_id) FILTER (WHERE status <> 'cancelado')::bigint AS pedidos,
    coalesce(sum(quantidade) FILTER (WHERE status <> 'cancelado'), 0)::bigint AS itens,
    coalesce(sum(valor_centavos) FILTER (WHERE status <> 'cancelado'), 0)::bigint AS vendas_centavos,
    coalesce(sum(comissao_centavos) FILTER (WHERE status <> 'cancelado'), 0)::bigint AS comissao_estimada_centavos,
    coalesce(sum(comissao_centavos) FILTER (WHERE status = 'concluido'), 0)::bigint AS comissao_validada_centavos
FROM conversoes
WHERE workspace_id = @workspace_id
  AND usuario_id = ANY(@usuarios::uuid[])
  AND ocorrido_em >= @de AND ocorrido_em < @ate
GROUP BY item_id
HAVING count(*) FILTER (WHERE status <> 'cancelado') > 0
ORDER BY comissao_estimada_centavos DESC, pedidos DESC, item_id
LIMIT @limite;

-- name: PorCanal :many
SELECT
    coalesce(canal::text, '')::text AS canal,
    count(DISTINCT pedido_id) FILTER (WHERE status <> 'cancelado')::bigint AS pedidos,
    coalesce(sum(comissao_centavos) FILTER (WHERE status <> 'cancelado'), 0)::bigint AS comissao_estimada_centavos,
    coalesce(sum(comissao_centavos) FILTER (WHERE status = 'concluido'), 0)::bigint AS comissao_validada_centavos
FROM conversoes
WHERE workspace_id = @workspace_id
  AND usuario_id = ANY(@usuarios::uuid[])
  AND ocorrido_em >= @de AND ocorrido_em < @ate
GROUP BY 1
HAVING count(*) FILTER (WHERE status <> 'cancelado') > 0
ORDER BY comissao_estimada_centavos DESC, 1;

-- name: PorGrupo :many
-- Resultados por grupo de importações (as de uma lista da curadoria): cada
-- posição dos arrays é um produto importado por um afiliado, a partir de
-- quando. Conta só as vendas desse produto, desse afiliado, depois da
-- importação.
WITH imp AS (
    -- Os unnest no SELECT andam juntos, posição a posição.
    SELECT unnest(@grupos::int[]) AS grupo,
           unnest(@usuarios::uuid[]) AS usuario_id,
           unnest(@produtos::uuid[]) AS produto_id,
           unnest(@desde::timestamptz[]) AS desde
)
SELECT
    imp.grupo::int AS grupo,
    count(DISTINCT c.pedido_id) FILTER (WHERE c.status <> 'cancelado')::bigint AS pedidos,
    coalesce(sum(c.valor_centavos) FILTER (WHERE c.status <> 'cancelado'), 0)::bigint AS vendas_centavos,
    coalesce(sum(c.comissao_centavos) FILTER (WHERE c.status <> 'cancelado'), 0)::bigint AS comissao_estimada_centavos,
    coalesce(sum(c.comissao_centavos) FILTER (WHERE c.status = 'concluido'), 0)::bigint AS comissao_validada_centavos
FROM imp
JOIN conversoes c
  ON c.usuario_id = imp.usuario_id
 AND c.produto_id = imp.produto_id
 AND c.ocorrido_em >= imp.desde
WHERE c.workspace_id = @workspace_id
  AND c.ocorrido_em >= @de AND c.ocorrido_em < @ate
GROUP BY imp.grupo;

-- name: Sincronizacao :one
SELECT * FROM sincronizacoes WHERE usuario_id = @usuario_id;

-- name: IniciarSincronizacao :one
INSERT INTO sincronizacoes (usuario_id, status, pedida_em)
VALUES (@usuario_id, 'sincronizando', @agora)
ON CONFLICT (usuario_id) DO UPDATE SET status = 'sincronizando', pedida_em = excluded.pedida_em, erro = NULL
RETURNING *;

-- name: ConcluirSincronizacao :exec
-- concluida_em e conversoes são da última sincronização que deu certo.
INSERT INTO sincronizacoes (usuario_id, status, concluida_em, conversoes, erro)
VALUES (@usuario_id, @status, CASE WHEN @status = 'ok' THEN @agora::timestamptz END, @conversoes, sqlc.narg(erro))
ON CONFLICT (usuario_id) DO UPDATE SET
    status = excluded.status,
    concluida_em = coalesce(excluded.concluida_em, sincronizacoes.concluida_em),
    conversoes = CASE WHEN excluded.status = 'ok' THEN excluded.conversoes ELSE sincronizacoes.conversoes END,
    erro = excluded.erro;
