-- Radar: one row per trending product, recomputed by the compute_trends job
-- from the snapshots. It keeps a copy of the display data so the radar filters
-- and sorts without reading the product tables. Global catalog, no
-- workspace_id: the API only reads it.

-- +goose Up
CREATE TABLE trends (
    product_id              uuid PRIMARY KEY REFERENCES products (id) ON DELETE CASCADE,
    computed_at             timestamptz NOT NULL,
    score                   double precision NOT NULL,
    earnings_per_sale_cents bigint NOT NULL,
    -- estimated sales growth over 7 days; null without a day of history
    sales_growth_7d         bigint,
    name                    text NOT NULL,
    shop_name               text NOT NULL,
    image_url               text,
    category_id             bigint,
    categories              bigint[] NOT NULL,
    url                     text NOT NULL,
    min_price_cents         bigint NOT NULL,
    max_price_cents         bigint NOT NULL,
    commission_bp           integer NOT NULL,
    sales                   bigint NOT NULL,
    rating                  numeric(3, 2),
    updated_at              timestamptz NOT NULL,
    -- product names are in Portuguese, so the text search uses that language
    search                  tsvector GENERATED ALWAYS AS (
        to_tsvector('portuguese', name || ' ' || shop_name)) STORED
);

CREATE INDEX trends_score ON trends (score DESC, sales DESC);
CREATE INDEX trends_categories ON trends USING gin (categories);
CREATE INDEX trends_search ON trends USING gin (search);
CREATE INDEX trends_name_trgm ON trends USING gin (name gin_trgm_ops);

GRANT SELECT ON trends TO parceiros_app;

-- +goose Down
DROP TABLE trends;
