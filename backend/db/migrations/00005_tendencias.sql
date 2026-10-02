-- Radar: uma linha por produto em alta, recalculada pelo job
-- calcular_tendencias a partir dos snapshots. Guarda uma cópia dos dados de
-- exibição para o radar filtrar e ordenar sem ler as tabelas de produtos.
-- Catálogo global, sem workspace_id: a API só lê.

-- +goose Up
CREATE TABLE tendencias (
    produto_id               uuid PRIMARY KEY REFERENCES produtos (id) ON DELETE CASCADE,
    calculado_em             timestamptz NOT NULL,
    score                    double precision NOT NULL,
    ganho_por_venda_centavos bigint NOT NULL,
    -- Crescimento de vendas estimado para 7 dias; nulo sem um dia de histórico.
    variacao_vendas_7d       bigint,
    nome                     text NOT NULL,
    loja_nome                text NOT NULL,
    imagem_url               text,
    categoria_id             bigint,
    categorias               bigint[] NOT NULL,
    url                      text NOT NULL,
    preco_min_centavos       bigint NOT NULL,
    preco_max_centavos       bigint NOT NULL,
    comissao_bp              integer NOT NULL,
    vendas                   bigint NOT NULL,
    nota                     numeric(3, 2),
    atualizado_em            timestamptz NOT NULL,
    busca                    tsvector GENERATED ALWAYS AS (
        to_tsvector('portuguese', nome || ' ' || loja_nome)) STORED
);

CREATE INDEX tendencias_score ON tendencias (score DESC, vendas DESC);
CREATE INDEX tendencias_categorias ON tendencias USING gin (categorias);
CREATE INDEX tendencias_busca ON tendencias USING gin (busca);
CREATE INDEX tendencias_nome_trgm ON tendencias USING gin (nome gin_trgm_ops);

GRANT SELECT ON tendencias TO parceiros_app;

-- +goose Down
DROP TABLE tendencias;
