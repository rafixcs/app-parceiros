-- name: UpsertTrend :batchexec
INSERT INTO trends (
    product_id, computed_at, score, earnings_per_sale_cents, sales_growth_7d,
    name, shop_name, image_url, category_id, categories, url,
    min_price_cents, max_price_cents, commission_bp, sales, rating, updated_at
) VALUES (
    @product_id, @computed_at, @score, @earnings_per_sale_cents, @sales_growth_7d,
    @name, @shop_name, @image_url, @category_id, @categories, @url,
    @min_price_cents, @max_price_cents, @commission_bp, @sales, @rating, @updated_at
)
ON CONFLICT (product_id) DO UPDATE SET
    computed_at = excluded.computed_at,
    score = excluded.score,
    earnings_per_sale_cents = excluded.earnings_per_sale_cents,
    sales_growth_7d = excluded.sales_growth_7d,
    name = excluded.name,
    shop_name = excluded.shop_name,
    image_url = excluded.image_url,
    category_id = excluded.category_id,
    categories = excluded.categories,
    url = excluded.url,
    min_price_cents = excluded.min_price_cents,
    max_price_cents = excluded.max_price_cents,
    commission_bp = excluded.commission_bp,
    sales = excluded.sales,
    rating = excluded.rating,
    updated_at = excluded.updated_at;

-- name: DeleteStaleTrends :execrows
DELETE FROM trends WHERE computed_at < @computed_at;

-- name: Radar :many
SELECT
    product_id, name, image_url, shop_name, url, categories,
    min_price_cents, max_price_cents, commission_bp, earnings_per_sale_cents,
    sales, rating, score, sales_growth_7d, updated_at,
    count(*) OVER () AS total
FROM trends
WHERE (sqlc.narg(category)::bigint IS NULL OR sqlc.narg(category)::bigint = ANY (categories))
  AND (sqlc.narg(min_price)::bigint IS NULL OR min_price_cents >= sqlc.narg(min_price)::bigint)
  AND (sqlc.narg(max_price)::bigint IS NULL OR min_price_cents <= sqlc.narg(max_price)::bigint)
  AND (sqlc.narg(min_commission)::integer IS NULL OR commission_bp >= sqlc.narg(min_commission)::integer)
  AND (sqlc.narg(min_rating)::numeric IS NULL OR rating >= sqlc.narg(min_rating)::numeric)
  AND (
    sqlc.narg(query)::text IS NULL
    OR search @@ websearch_to_tsquery('portuguese', sqlc.narg(query)::text)
    OR name ILIKE sqlc.narg(pattern)::text ESCAPE '\'
    OR shop_name ILIKE sqlc.narg(pattern)::text ESCAPE '\'
  )
ORDER BY
    CASE WHEN @sort::text = 'commission' THEN commission_bp END DESC,
    CASE WHEN @sort::text = 'earnings' THEN earnings_per_sale_cents END DESC,
    CASE WHEN @sort::text = 'sales' THEN sales END DESC,
    score DESC, sales DESC, product_id
LIMIT @row_limit OFFSET @row_offset;

-- name: RadarUpdatedAt :one
SELECT updated_at FROM trends ORDER BY updated_at DESC LIMIT 1;

-- name: RadarItem :one
SELECT
    product_id, name, image_url, shop_name, url, categories,
    min_price_cents, max_price_cents, commission_bp, earnings_per_sale_cents,
    sales, rating, score, sales_growth_7d, updated_at
FROM trends WHERE product_id = $1;

-- name: RadarCategories :many
SELECT category_id::bigint AS id, count(*) AS products
FROM trends
WHERE category_id IS NOT NULL
GROUP BY category_id
ORDER BY products DESC, category_id;
