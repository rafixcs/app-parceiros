-- Global product catalog, collected by the snapshot_catalog job with the app
-- credential. It is not customer data (no workspace_id): the API only reads it
-- (SELECT for parceiros_app) and the worker writes with the role that owns the
-- tables.

-- +goose Up
CREATE TYPE source AS ENUM ('shopee');

-- Known categories. `monitored` turns on the periodic snapshot of the category.
CREATE TABLE categories (
    source    source NOT NULL,
    id        bigint NOT NULL,
    name      text NOT NULL,
    monitored boolean NOT NULL DEFAULT false,
    PRIMARY KEY (source, id)
);

-- Current data of the product (from the latest collection). The history is in
-- product_snapshots.
CREATE TABLE products (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    source          source NOT NULL,
    item_id         bigint NOT NULL,
    shop_id         bigint NOT NULL,
    shop_name       text NOT NULL,
    name            text NOT NULL,
    image_url       text,
    category_id     bigint,
    categories      bigint[] NOT NULL DEFAULT '{}',
    url             text NOT NULL,
    min_price_cents bigint NOT NULL,
    max_price_cents bigint NOT NULL,
    commission_bp   integer NOT NULL CHECK (commission_bp BETWEEN 0 AND 10000),
    sales           bigint NOT NULL,
    rating          numeric(3, 2),
    collected_at    timestamptz NOT NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    UNIQUE (source, item_id)
);

-- One row per product per collection (the full hour), partitioned by month.
CREATE TABLE product_snapshots (
    product_id      uuid NOT NULL REFERENCES products (id) ON DELETE CASCADE,
    collected_at    timestamptz NOT NULL,
    min_price_cents bigint NOT NULL,
    max_price_cents bigint NOT NULL,
    commission_bp   integer NOT NULL,
    sales           bigint NOT NULL,
    rating          numeric(3, 2),
    PRIMARY KEY (product_id, collected_at)
) PARTITION BY RANGE (collected_at);

-- Safety net: the job creates the month partition before inserting, so this
-- one should stay empty.
CREATE TABLE product_snapshots_default PARTITION OF product_snapshots DEFAULT;

-- +goose StatementBegin
CREATE FUNCTION ensure_snapshot_partition(month date) RETURNS void
    LANGUAGE plpgsql AS $$
DECLARE
    first_day date := date_trunc('month', month)::date;
    partition_name text := format('product_snapshots_%s', to_char(first_day, 'YYYY_MM'));
BEGIN
    IF to_regclass(partition_name) IS NULL THEN
        EXECUTE format(
            'CREATE TABLE %I PARTITION OF product_snapshots FOR VALUES FROM (%L) TO (%L)',
            partition_name, first_day, (first_day + interval '1 month')::date);
    END IF;
END
$$;
-- +goose StatementEnd

SELECT ensure_snapshot_partition(now()::date);
SELECT ensure_snapshot_partition((now() + interval '1 month')::date);

GRANT SELECT ON categories, products, product_snapshots TO parceiros_app;

-- +goose Down
DROP TABLE product_snapshots;
DROP FUNCTION ensure_snapshot_partition(date);
DROP TABLE products;
DROP TABLE categories;
DROP TYPE source;
