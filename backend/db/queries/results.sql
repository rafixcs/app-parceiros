-- Queries of the results module. The reads filter by workspace and by the
-- requested users (the affiliate themself, or the group members who
-- consented), on top of RLS. Days count in the Brasília time zone.

-- name: SaveConversion :exec
INSERT INTO conversions (
    user_id, workspace_id, source, conversion_id, order_id, item_id, model_id, product_id,
    item_name, shop_name, sub_id, channel, status, quantity, amount_cents, commission_cents,
    occurred_at, clicked_at
) VALUES (
    @user_id, @workspace_id, @source, @conversion_id, @order_id, @item_id, @model_id, sqlc.narg(product_id),
    @item_name, @shop_name, @sub_id, sqlc.narg(channel), @status, @quantity, @amount_cents, @commission_cents,
    @occurred_at, sqlc.narg(clicked_at)
)
ON CONFLICT (user_id, source, order_id, item_id, model_id) DO UPDATE SET
    workspace_id = excluded.workspace_id,
    conversion_id = excluded.conversion_id,
    product_id = coalesce(excluded.product_id, conversions.product_id),
    item_name = excluded.item_name,
    shop_name = excluded.shop_name,
    sub_id = excluded.sub_id,
    channel = excluded.channel,
    status = excluded.status,
    quantity = excluded.quantity,
    amount_cents = excluded.amount_cents,
    commission_cents = excluded.commission_cents,
    occurred_at = excluded.occurred_at,
    clicked_at = excluded.clicked_at,
    synced_at = now();

-- name: ResultTotals :one
SELECT
    count(DISTINCT order_id) FILTER (WHERE status <> 'cancelled')::bigint AS orders,
    count(DISTINCT order_id) FILTER (WHERE status = 'cancelled')::bigint AS cancelled,
    coalesce(sum(quantity) FILTER (WHERE status <> 'cancelled'), 0)::bigint AS items,
    coalesce(sum(amount_cents) FILTER (WHERE status <> 'cancelled'), 0)::bigint AS sales_cents,
    coalesce(sum(commission_cents) FILTER (WHERE status <> 'cancelled'), 0)::bigint AS estimated_commission_cents,
    coalesce(sum(commission_cents) FILTER (WHERE status = 'completed'), 0)::bigint AS validated_commission_cents,
    count(DISTINCT user_id) FILTER (WHERE status <> 'cancelled')::bigint AS active_users
FROM conversions
WHERE workspace_id = @workspace_id
  AND user_id = ANY(@user_ids::uuid[])
  AND occurred_at >= @from_time AND occurred_at < @to_time;

-- name: ResultsByDay :many
SELECT
    (occurred_at AT TIME ZONE 'America/Sao_Paulo')::date::text AS day,
    count(DISTINCT order_id) FILTER (WHERE status <> 'cancelled')::bigint AS orders,
    coalesce(sum(commission_cents) FILTER (WHERE status <> 'cancelled'), 0)::bigint AS estimated_commission_cents,
    coalesce(sum(commission_cents) FILTER (WHERE status = 'completed'), 0)::bigint AS validated_commission_cents
FROM conversions
WHERE workspace_id = @workspace_id
  AND user_id = ANY(@user_ids::uuid[])
  AND occurred_at >= @from_time AND occurred_at < @to_time
GROUP BY 1
ORDER BY 1;

-- name: ResultsByProduct :many
SELECT
    item_id,
    coalesce(max(product_id::text), '')::text AS product_id,
    max(item_name)::text AS item_name,
    max(shop_name)::text AS shop_name,
    count(DISTINCT order_id) FILTER (WHERE status <> 'cancelled')::bigint AS orders,
    coalesce(sum(quantity) FILTER (WHERE status <> 'cancelled'), 0)::bigint AS items,
    coalesce(sum(amount_cents) FILTER (WHERE status <> 'cancelled'), 0)::bigint AS sales_cents,
    coalesce(sum(commission_cents) FILTER (WHERE status <> 'cancelled'), 0)::bigint AS estimated_commission_cents,
    coalesce(sum(commission_cents) FILTER (WHERE status = 'completed'), 0)::bigint AS validated_commission_cents
FROM conversions
WHERE workspace_id = @workspace_id
  AND user_id = ANY(@user_ids::uuid[])
  AND occurred_at >= @from_time AND occurred_at < @to_time
GROUP BY item_id
HAVING count(*) FILTER (WHERE status <> 'cancelled') > 0
ORDER BY estimated_commission_cents DESC, orders DESC, item_id
LIMIT @row_limit;

-- name: ResultsByChannel :many
SELECT
    coalesce(channel::text, '')::text AS channel,
    count(DISTINCT order_id) FILTER (WHERE status <> 'cancelled')::bigint AS orders,
    coalesce(sum(commission_cents) FILTER (WHERE status <> 'cancelled'), 0)::bigint AS estimated_commission_cents,
    coalesce(sum(commission_cents) FILTER (WHERE status = 'completed'), 0)::bigint AS validated_commission_cents
FROM conversions
WHERE workspace_id = @workspace_id
  AND user_id = ANY(@user_ids::uuid[])
  AND occurred_at >= @from_time AND occurred_at < @to_time
GROUP BY 1
HAVING count(*) FILTER (WHERE status <> 'cancelled') > 0
ORDER BY estimated_commission_cents DESC, 1;

-- name: ResultsByImportGroup :many
-- Results per group of imports (those of one curated list): each position of
-- the arrays is a product imported by an affiliate, since when. It counts only
-- the sales of that product, by that affiliate, after the import.
WITH imp AS (
    -- The unnest calls in the SELECT walk together, position by position.
    SELECT unnest(@group_ids::int[]) AS group_id,
           unnest(@import_users::uuid[]) AS user_id,
           unnest(@import_products::uuid[]) AS product_id,
           unnest(@imported_at::timestamptz[]) AS since
)
SELECT
    imp.group_id::int AS group_id,
    count(DISTINCT c.order_id) FILTER (WHERE c.status <> 'cancelled')::bigint AS orders,
    coalesce(sum(c.amount_cents) FILTER (WHERE c.status <> 'cancelled'), 0)::bigint AS sales_cents,
    coalesce(sum(c.commission_cents) FILTER (WHERE c.status <> 'cancelled'), 0)::bigint AS estimated_commission_cents,
    coalesce(sum(c.commission_cents) FILTER (WHERE c.status = 'completed'), 0)::bigint AS validated_commission_cents
FROM imp
JOIN conversions c
  ON c.user_id = imp.user_id
 AND c.product_id = imp.product_id
 AND c.occurred_at >= imp.since
WHERE c.workspace_id = @workspace_id
  AND c.occurred_at >= @from_time AND c.occurred_at < @to_time
GROUP BY imp.group_id;

-- name: ConversionSyncByUser :one
SELECT * FROM conversion_syncs WHERE user_id = @user_id;

-- name: StartConversionSync :one
INSERT INTO conversion_syncs (user_id, status, requested_at)
VALUES (@user_id, 'syncing', @now)
ON CONFLICT (user_id) DO UPDATE SET status = 'syncing', requested_at = excluded.requested_at, error = NULL
RETURNING *;

-- name: FinishConversionSync :exec
-- finished_at and conversions are those of the last successful sync.
INSERT INTO conversion_syncs (user_id, status, finished_at, conversions, error)
VALUES (@user_id, @status::text, CASE WHEN @status::text = 'ok' THEN @now::timestamptz END, @conversions, sqlc.narg(error))
ON CONFLICT (user_id) DO UPDATE SET
    status = excluded.status,
    finished_at = coalesce(excluded.finished_at, conversion_syncs.finished_at),
    conversions = CASE WHEN excluded.status = 'ok' THEN excluded.conversions ELSE conversion_syncs.conversions END,
    error = excluded.error;
