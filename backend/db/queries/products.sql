-- name: EnsureSnapshotPartition :exec
SELECT ensure_snapshot_partition((@month::timestamptz)::date);

-- name: UpsertProduct :batchone
INSERT INTO products (
    source, item_id, shop_id, shop_name, name, image_url, category_id, categories, url,
    min_price_cents, max_price_cents, commission_bp, sales, rating, collected_at
) VALUES (
    @source, @item_id, @shop_id, @shop_name, @name, @image_url, @category_id, @categories, @url,
    @min_price_cents, @max_price_cents, @commission_bp, @sales, @rating, @collected_at
)
ON CONFLICT (source, item_id) DO UPDATE SET
    shop_id = excluded.shop_id,
    shop_name = excluded.shop_name,
    name = excluded.name,
    image_url = excluded.image_url,
    category_id = excluded.category_id,
    categories = excluded.categories,
    url = excluded.url,
    min_price_cents = excluded.min_price_cents,
    max_price_cents = excluded.max_price_cents,
    commission_bp = excluded.commission_bp,
    sales = excluded.sales,
    rating = excluded.rating,
    collected_at = excluded.collected_at
WHERE products.collected_at <= excluded.collected_at
RETURNING id;

-- name: ProductIDByItem :one
SELECT id FROM products WHERE source = @source AND item_id = @item_id;

-- name: InsertSnapshot :batchexec
INSERT INTO product_snapshots (product_id, collected_at, min_price_cents, max_price_cents, commission_bp, sales, rating)
VALUES (@product_id, @collected_at, @min_price_cents, @max_price_cents, @commission_bp, @sales, @rating)
ON CONFLICT (product_id, collected_at) DO NOTHING;

-- name: MonitoredCategories :many
SELECT id FROM categories WHERE source = @source AND monitored ORDER BY id;

-- name: UpsertCategory :exec
INSERT INTO categories (source, id, name, monitored) VALUES (@source, @id, @name, @monitored)
ON CONFLICT (source, id) DO UPDATE SET name = excluded.name, monitored = excluded.monitored;

-- name: CategoryNames :many
SELECT id, name FROM categories WHERE source = @source AND id = ANY (@ids::bigint[]);

-- name: ProductsForTrends :many
-- Current data of each product collected since @since and the baseline to
-- measure its growth: the latest snapshot 7 to 14 days old or, while there is
-- none, the oldest of the last 7 days.
SELECT
    p.id, p.source, p.item_id, p.name, p.shop_name, p.image_url, p.category_id, p.categories, p.url,
    p.min_price_cents, p.max_price_cents, p.commission_bp, p.sales, p.rating, p.collected_at,
    COALESCE(before.sales, after.sales)::bigint AS base_sales,
    COALESCE(before.collected_at, after.collected_at)::timestamptz AS base_collected_at
FROM products p
LEFT JOIN LATERAL (
    SELECT s.sales, s.collected_at FROM product_snapshots s
    WHERE s.product_id = p.id
      AND s.collected_at BETWEEN p.collected_at - interval '14 days' AND p.collected_at - interval '7 days'
    ORDER BY s.collected_at DESC
    LIMIT 1
) before ON true
LEFT JOIN LATERAL (
    SELECT s.sales, s.collected_at FROM product_snapshots s
    WHERE s.product_id = p.id AND s.collected_at > p.collected_at - interval '7 days'
    ORDER BY s.collected_at
    LIMIT 1
) after ON true
WHERE p.collected_at >= @since;

-- name: ProductByID :one
SELECT * FROM products WHERE id = $1;

-- name: ProductHistory :many
SELECT collected_at, min_price_cents, max_price_cents, commission_bp, sales, rating
FROM product_snapshots
WHERE product_id = @product_id AND collected_at >= @since
ORDER BY collected_at;

-- name: ProductsByIDs :many
SELECT * FROM products WHERE id = ANY (@ids::uuid[]);

-- name: ProductByItem :one
SELECT * FROM products WHERE source = @source AND item_id = @item_id;
