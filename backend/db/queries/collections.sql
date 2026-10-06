-- Collections: every query filters by workspace_id and user_id, besides RLS.

-- name: CreateSavedItem :one
INSERT INTO saved_items (workspace_id, user_id, product_id, title, notes, link_status)
VALUES (@workspace_id, @user_id, @product_id, @title, @notes, @link_status)
ON CONFLICT (workspace_id, user_id, product_id) DO NOTHING
RETURNING *;

-- name: SavedItemByProduct :one
SELECT * FROM saved_items
WHERE workspace_id = @workspace_id AND user_id = @user_id AND product_id = @product_id;

-- name: SavedItemByID :one
SELECT * FROM saved_items
WHERE id = @id AND workspace_id = @workspace_id AND user_id = @user_id;

-- name: ListSavedItems :many
SELECT i.*, count(*) OVER () AS total
FROM saved_items i
WHERE i.workspace_id = @workspace_id AND i.user_id = @user_id
  AND (sqlc.narg('status')::item_status IS NULL OR i.status = sqlc.narg('status'))
  AND (sqlc.narg('tag')::text IS NULL OR sqlc.narg('tag') = ANY (i.tags))
  AND (sqlc.narg('query')::text IS NULL
       OR i.title ILIKE '%' || sqlc.narg('query') || '%'
       OR i.description ILIKE '%' || sqlc.narg('query') || '%'
       OR i.notes ILIKE '%' || sqlc.narg('query') || '%')
  AND (sqlc.narg('collection_id')::uuid IS NULL OR EXISTS (
       SELECT 1 FROM collection_items ci
       WHERE ci.item_id = i.id AND ci.collection_id = sqlc.narg('collection_id')
         AND ci.workspace_id = @workspace_id AND ci.user_id = @user_id))
ORDER BY i.created_at DESC, i.id
LIMIT @row_limit OFFSET @row_offset;

-- name: UpdateSavedItem :one
UPDATE saved_items SET
    title = coalesce(sqlc.narg('title'), title),
    description = coalesce(sqlc.narg('description'), description),
    notes = coalesce(sqlc.narg('notes'), notes),
    tags = coalesce(sqlc.narg('tags')::text[], tags),
    status = coalesce(sqlc.narg('status')::item_status, status),
    updated_at = now()
WHERE id = @id AND workspace_id = @workspace_id AND user_id = @user_id
RETURNING *;

-- name: SetManualLink :one
UPDATE saved_items SET
    affiliate_link = @affiliate_link, link_origin = 'manual', link_status = 'ready', updated_at = now()
WHERE id = @id AND workspace_id = @workspace_id AND user_id = @user_id
RETURNING *;

-- name: ResetAutoLink :one
-- Back to the automatic link: clears the current one and waits for the
-- generate_affiliate_link job.
UPDATE saved_items SET
    affiliate_link = NULL, link_origin = 'auto', link_status = @link_status, updated_at = now()
WHERE id = @id AND workspace_id = @workspace_id AND user_id = @user_id
RETURNING *;

-- name: MarkLinkStatus :exec
-- Only touches automatic links: a manual link saved while the job ran wins.
UPDATE saved_items SET link_status = @link_status, updated_at = now()
WHERE id = @id AND workspace_id = @workspace_id AND user_id = @user_id AND link_origin = 'auto';

-- name: CompleteAutoLink :execrows
UPDATE saved_items SET affiliate_link = @affiliate_link, link_status = 'ready', updated_at = now()
WHERE id = @id AND workspace_id = @workspace_id AND user_id = @user_id AND link_origin = 'auto';

-- name: ClaimPendingLinks :many
-- Automatic links pending (no credential) or failed, to generate again.
UPDATE saved_items SET link_status = 'generating', updated_at = now()
WHERE workspace_id = @workspace_id AND user_id = @user_id
  AND link_origin = 'auto' AND link_status IN ('pending', 'failed')
RETURNING id;

-- name: DeleteSavedItem :execrows
DELETE FROM saved_items WHERE id = @id AND workspace_id = @workspace_id AND user_id = @user_id;

-- name: SavedProductIDs :many
SELECT product_id FROM saved_items
WHERE workspace_id = @workspace_id AND user_id = @user_id
ORDER BY created_at DESC;

-- name: DeleteChannelLinks :exec
DELETE FROM channel_links WHERE item_id = @item_id AND workspace_id = @workspace_id AND user_id = @user_id;

-- name: SaveChannelLink :exec
INSERT INTO channel_links (item_id, workspace_id, user_id, channel, sub_id, url)
VALUES (@item_id, @workspace_id, @user_id, @channel, @sub_id, @url)
ON CONFLICT (item_id, channel) DO UPDATE SET sub_id = excluded.sub_id, url = excluded.url, generated_at = now();

-- name: ChannelLinksOfItems :many
SELECT item_id, channel, sub_id, url FROM channel_links
WHERE workspace_id = @workspace_id AND user_id = @user_id AND item_id = ANY (@ids::uuid[])
ORDER BY item_id, channel;

-- name: CollectionsOfItems :many
SELECT item_id, collection_id FROM collection_items
WHERE workspace_id = @workspace_id AND user_id = @user_id AND item_id = ANY (@ids::uuid[])
ORDER BY item_id, added_at;

-- name: ListCollections :many
SELECT c.id, c.name, c.created_at,
    (SELECT count(*) FROM collection_items ci
     WHERE ci.collection_id = c.id AND ci.workspace_id = @workspace_id AND ci.user_id = @user_id) AS items
FROM collections c
WHERE c.workspace_id = @workspace_id AND c.user_id = @user_id
ORDER BY lower(c.name);

-- name: CollectionByID :one
SELECT c.id, c.name, c.created_at,
    (SELECT count(*) FROM collection_items ci
     WHERE ci.collection_id = c.id AND ci.workspace_id = @workspace_id AND ci.user_id = @user_id) AS items
FROM collections c
WHERE c.id = @id AND c.workspace_id = @workspace_id AND c.user_id = @user_id;

-- name: CreateCollection :one
INSERT INTO collections (workspace_id, user_id, name) VALUES (@workspace_id, @user_id, @name)
RETURNING id;

-- name: RenameCollection :execrows
UPDATE collections SET name = @name
WHERE id = @id AND workspace_id = @workspace_id AND user_id = @user_id;

-- name: DeleteCollection :execrows
DELETE FROM collections WHERE id = @id AND workspace_id = @workspace_id AND user_id = @user_id;

-- name: CountCollections :one
SELECT count(*) FROM collections
WHERE workspace_id = @workspace_id AND user_id = @user_id AND id = ANY (@ids::uuid[]);

-- name: ClearItemCollections :exec
DELETE FROM collection_items WHERE item_id = @item_id AND workspace_id = @workspace_id AND user_id = @user_id;

-- name: AddCollectionItem :exec
INSERT INTO collection_items (collection_id, item_id, workspace_id, user_id)
VALUES (@collection_id, @item_id, @workspace_id, @user_id)
ON CONFLICT DO NOTHING;

-- name: SavedItemsByProducts :many
SELECT * FROM saved_items
WHERE workspace_id = @workspace_id AND user_id = @user_id AND product_id = ANY (@product_ids::uuid[]);

-- name: CollectionByName :one
SELECT id FROM collections
WHERE workspace_id = @workspace_id AND user_id = @user_id AND lower(name) = lower(@name);
