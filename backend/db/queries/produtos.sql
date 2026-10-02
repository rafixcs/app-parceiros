-- name: GarantirParticaoSnapshots :exec
SELECT garantir_particao_snapshots((@mes::timestamptz)::date);

-- name: UpsertProduto :batchone
INSERT INTO produtos (
    fonte, item_id, loja_id, loja_nome, nome, imagem_url, categoria_id, categorias, url,
    preco_min_centavos, preco_max_centavos, comissao_bp, vendas, nota, coletado_em
) VALUES (
    @fonte, @item_id, @loja_id, @loja_nome, @nome, @imagem_url, @categoria_id, @categorias, @url,
    @preco_min_centavos, @preco_max_centavos, @comissao_bp, @vendas, @nota, @coletado_em
)
ON CONFLICT (fonte, item_id) DO UPDATE SET
    loja_id = excluded.loja_id,
    loja_nome = excluded.loja_nome,
    nome = excluded.nome,
    imagem_url = excluded.imagem_url,
    categoria_id = excluded.categoria_id,
    categorias = excluded.categorias,
    url = excluded.url,
    preco_min_centavos = excluded.preco_min_centavos,
    preco_max_centavos = excluded.preco_max_centavos,
    comissao_bp = excluded.comissao_bp,
    vendas = excluded.vendas,
    nota = excluded.nota,
    coletado_em = excluded.coletado_em
WHERE produtos.coletado_em <= excluded.coletado_em
RETURNING id;

-- name: ProdutoPorItem :one
SELECT id FROM produtos WHERE fonte = @fonte AND item_id = @item_id;

-- name: InserirSnapshot :batchexec
INSERT INTO produto_snapshots (produto_id, coletado_em, preco_min_centavos, preco_max_centavos, comissao_bp, vendas, nota)
VALUES (@produto_id, @coletado_em, @preco_min_centavos, @preco_max_centavos, @comissao_bp, @vendas, @nota)
ON CONFLICT (produto_id, coletado_em) DO NOTHING;

-- name: CategoriasMonitoradas :many
SELECT id FROM categorias WHERE fonte = @fonte AND monitorar ORDER BY id;

-- name: UpsertCategoria :exec
INSERT INTO categorias (fonte, id, nome, monitorar) VALUES (@fonte, @id, @nome, @monitorar)
ON CONFLICT (fonte, id) DO UPDATE SET nome = excluded.nome, monitorar = excluded.monitorar;

-- name: NomesCategorias :many
SELECT id, nome FROM categorias WHERE fonte = @fonte AND id = ANY (@ids::bigint[]);

-- name: ParaTendencias :many
-- Dados atuais de cada produto coletado desde @desde e a base para medir o
-- crescimento: o snapshot mais recente com 7 a 14 dias, ou, se ainda não
-- houver, o mais antigo dos últimos 7 dias.
SELECT
    p.id, p.nome, p.loja_nome, p.imagem_url, p.categoria_id, p.categorias, p.url,
    p.preco_min_centavos, p.preco_max_centavos, p.comissao_bp, p.vendas, p.nota, p.coletado_em,
    COALESCE(antes.vendas, depois.vendas)::bigint AS base_vendas,
    COALESCE(antes.coletado_em, depois.coletado_em)::timestamptz AS base_coletado_em
FROM produtos p
LEFT JOIN LATERAL (
    SELECT s.vendas, s.coletado_em FROM produto_snapshots s
    WHERE s.produto_id = p.id
      AND s.coletado_em BETWEEN p.coletado_em - interval '14 days' AND p.coletado_em - interval '7 days'
    ORDER BY s.coletado_em DESC
    LIMIT 1
) antes ON true
LEFT JOIN LATERAL (
    SELECT s.vendas, s.coletado_em FROM produto_snapshots s
    WHERE s.produto_id = p.id AND s.coletado_em > p.coletado_em - interval '7 days'
    ORDER BY s.coletado_em
    LIMIT 1
) depois ON true
WHERE p.coletado_em >= @desde;

-- name: Produto :one
SELECT * FROM produtos WHERE id = $1;

-- name: Historico :many
SELECT coletado_em, preco_min_centavos, preco_max_centavos, comissao_bp, vendas, nota
FROM produto_snapshots
WHERE produto_id = @produto_id AND coletado_em >= @desde
ORDER BY coletado_em;
