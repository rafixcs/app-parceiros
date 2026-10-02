-- name: UpsertTendencia :batchexec
INSERT INTO tendencias (
    produto_id, calculado_em, score, ganho_por_venda_centavos, variacao_vendas_7d,
    nome, loja_nome, imagem_url, categoria_id, categorias, url,
    preco_min_centavos, preco_max_centavos, comissao_bp, vendas, nota, atualizado_em
) VALUES (
    @produto_id, @calculado_em, @score, @ganho_por_venda_centavos, @variacao_vendas_7d,
    @nome, @loja_nome, @imagem_url, @categoria_id, @categorias, @url,
    @preco_min_centavos, @preco_max_centavos, @comissao_bp, @vendas, @nota, @atualizado_em
)
ON CONFLICT (produto_id) DO UPDATE SET
    calculado_em = excluded.calculado_em,
    score = excluded.score,
    ganho_por_venda_centavos = excluded.ganho_por_venda_centavos,
    variacao_vendas_7d = excluded.variacao_vendas_7d,
    nome = excluded.nome,
    loja_nome = excluded.loja_nome,
    imagem_url = excluded.imagem_url,
    categoria_id = excluded.categoria_id,
    categorias = excluded.categorias,
    url = excluded.url,
    preco_min_centavos = excluded.preco_min_centavos,
    preco_max_centavos = excluded.preco_max_centavos,
    comissao_bp = excluded.comissao_bp,
    vendas = excluded.vendas,
    nota = excluded.nota,
    atualizado_em = excluded.atualizado_em;

-- name: RemoverTendenciasAntigas :execrows
DELETE FROM tendencias WHERE calculado_em < @calculado_em;

-- name: Radar :many
SELECT
    produto_id, nome, imagem_url, loja_nome, url, categorias,
    preco_min_centavos, preco_max_centavos, comissao_bp, ganho_por_venda_centavos,
    vendas, nota, score, variacao_vendas_7d, atualizado_em,
    count(*) OVER () AS total
FROM tendencias
WHERE (sqlc.narg(categoria)::bigint IS NULL OR sqlc.narg(categoria)::bigint = ANY (categorias))
  AND (sqlc.narg(preco_min)::bigint IS NULL OR preco_min_centavos >= sqlc.narg(preco_min)::bigint)
  AND (sqlc.narg(preco_max)::bigint IS NULL OR preco_min_centavos <= sqlc.narg(preco_max)::bigint)
  AND (sqlc.narg(comissao_min)::integer IS NULL OR comissao_bp >= sqlc.narg(comissao_min)::integer)
  AND (sqlc.narg(nota_min)::numeric IS NULL OR nota >= sqlc.narg(nota_min)::numeric)
  AND (
    sqlc.narg(busca)::text IS NULL
    OR busca @@ websearch_to_tsquery('portuguese', sqlc.narg(busca)::text)
    OR nome ILIKE sqlc.narg(padrao)::text ESCAPE '\'
    OR loja_nome ILIKE sqlc.narg(padrao)::text ESCAPE '\'
  )
ORDER BY
    CASE WHEN @ordem::text = 'comissao' THEN comissao_bp END DESC,
    CASE WHEN @ordem::text = 'ganho' THEN ganho_por_venda_centavos END DESC,
    CASE WHEN @ordem::text = 'vendas' THEN vendas END DESC,
    score DESC, vendas DESC, produto_id
LIMIT @limite OFFSET @deslocamento;

-- name: RadarAtualizadoEm :one
SELECT atualizado_em FROM tendencias ORDER BY atualizado_em DESC LIMIT 1;

-- name: RadarItem :one
SELECT
    produto_id, nome, imagem_url, loja_nome, url, categorias,
    preco_min_centavos, preco_max_centavos, comissao_bp, ganho_por_venda_centavos,
    vendas, nota, score, variacao_vendas_7d, atualizado_em
FROM tendencias WHERE produto_id = $1;

-- name: CategoriasDoRadar :many
SELECT categoria_id::bigint AS id, count(*) AS produtos
FROM tendencias
WHERE categoria_id IS NOT NULL
GROUP BY categoria_id
ORDER BY produtos DESC, categoria_id;
