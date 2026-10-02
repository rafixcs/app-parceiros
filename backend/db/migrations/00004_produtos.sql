-- Catálogo global de produtos, coletado pelo job snapshot_catalogo com a
-- credencial do app. Não é dado de cliente (não tem workspace_id): a API só
-- lê (SELECT para parceiros_app) e o worker escreve com o papel dono das
-- tabelas.

-- +goose Up
CREATE TYPE fonte AS ENUM ('shopee');

-- Categorias conhecidas. `monitorar` liga o snapshot periódico da categoria.
CREATE TABLE categorias (
    fonte     fonte NOT NULL,
    id        bigint NOT NULL,
    nome      text NOT NULL,
    monitorar boolean NOT NULL DEFAULT false,
    PRIMARY KEY (fonte, id)
);

-- Dados atuais do produto (os da coleta mais recente). O histórico fica em
-- produto_snapshots.
CREATE TABLE produtos (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    fonte              fonte NOT NULL,
    item_id            bigint NOT NULL,
    loja_id            bigint NOT NULL,
    loja_nome          text NOT NULL,
    nome               text NOT NULL,
    imagem_url         text,
    categoria_id       bigint,
    categorias         bigint[] NOT NULL DEFAULT '{}',
    url                text NOT NULL,
    preco_min_centavos bigint NOT NULL,
    preco_max_centavos bigint NOT NULL,
    comissao_bp        integer NOT NULL CHECK (comissao_bp BETWEEN 0 AND 10000),
    vendas             bigint NOT NULL,
    nota               numeric(3, 2),
    coletado_em        timestamptz NOT NULL,
    criado_em          timestamptz NOT NULL DEFAULT now(),
    UNIQUE (fonte, item_id)
);

-- Uma linha por produto por coleta (a hora cheia), particionada por mês.
CREATE TABLE produto_snapshots (
    produto_id         uuid NOT NULL REFERENCES produtos (id) ON DELETE CASCADE,
    coletado_em        timestamptz NOT NULL,
    preco_min_centavos bigint NOT NULL,
    preco_max_centavos bigint NOT NULL,
    comissao_bp        integer NOT NULL,
    vendas             bigint NOT NULL,
    nota               numeric(3, 2),
    PRIMARY KEY (produto_id, coletado_em)
) PARTITION BY RANGE (coletado_em);

-- Rede de segurança: o job cria a partição do mês antes de inserir, então
-- esta deve ficar vazia.
CREATE TABLE produto_snapshots_padrao PARTITION OF produto_snapshots DEFAULT;

-- +goose StatementBegin
CREATE FUNCTION garantir_particao_snapshots(mes date) RETURNS void
    LANGUAGE plpgsql AS $$
DECLARE
    inicio date := date_trunc('month', mes)::date;
    nome text := format('produto_snapshots_%s', to_char(inicio, 'YYYY_MM'));
BEGIN
    IF to_regclass(nome) IS NULL THEN
        EXECUTE format(
            'CREATE TABLE %I PARTITION OF produto_snapshots FOR VALUES FROM (%L) TO (%L)',
            nome, inicio, (inicio + interval '1 month')::date);
    END IF;
END
$$;
-- +goose StatementEnd

SELECT garantir_particao_snapshots(now()::date);
SELECT garantir_particao_snapshots((now() + interval '1 month')::date);

GRANT SELECT ON categorias, produtos, produto_snapshots TO parceiros_app;

-- +goose Down
DROP TABLE produto_snapshots;
DROP FUNCTION garantir_particao_snapshots(date);
DROP TABLE produtos;
DROP TABLE categorias;
DROP TYPE fonte;
