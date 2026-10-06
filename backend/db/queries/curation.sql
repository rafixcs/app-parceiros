-- Curation: every query filters by workspace_id, besides RLS (which hides the
-- drafts from whoever is not owner or mentor).

-- name: CountCuratedLists :one
SELECT count(*) FROM curated_lists WHERE workspace_id = @workspace_id;

-- name: CreateCuratedList :one
INSERT INTO curated_lists (workspace_id, author_id, title, description)
VALUES (@workspace_id, @author_id, @title, @description)
RETURNING id;

-- name: CuratedLists :many
-- Drafts only come with drafts = true (and RLS only shows them to owner and
-- mentor). importers counts the group; imported_by_me, the user.
SELECT l.id, l.title, l.description, l.published_at, l.created_at, l.updated_at,
    (SELECT count(*) FROM curated_list_items li
     WHERE li.list_id = l.id AND li.workspace_id = @workspace_id) AS products,
    (SELECT count(DISTINCT i.user_id) FROM list_imports i
     WHERE i.list_id = l.id AND i.workspace_id = @workspace_id) AS importers,
    EXISTS (SELECT 1 FROM list_imports i
            WHERE i.list_id = l.id AND i.workspace_id = @workspace_id AND i.user_id = @user_id) AS imported_by_me
FROM curated_lists l
WHERE l.workspace_id = @workspace_id
  AND (sqlc.arg(drafts)::boolean OR l.published_at IS NOT NULL)
ORDER BY l.published_at IS NOT NULL, coalesce(l.published_at, l.created_at) DESC, l.id;

-- name: CuratedList :one
SELECT l.id, l.title, l.description, l.published_at, l.created_at, l.updated_at,
    (SELECT count(*) FROM curated_list_items li
     WHERE li.list_id = l.id AND li.workspace_id = @workspace_id) AS products,
    (SELECT count(DISTINCT i.user_id) FROM list_imports i
     WHERE i.list_id = l.id AND i.workspace_id = @workspace_id) AS importers,
    EXISTS (SELECT 1 FROM list_imports i
            WHERE i.list_id = l.id AND i.workspace_id = @workspace_id AND i.user_id = @user_id) AS imported_by_me
FROM curated_lists l
WHERE l.id = @id AND l.workspace_id = @workspace_id
  AND (sqlc.arg(drafts)::boolean OR l.published_at IS NOT NULL);

-- name: LockCuratedList :one
SELECT id FROM curated_lists
WHERE id = @id AND workspace_id = @workspace_id
FOR UPDATE;

-- name: UpdateCuratedList :execrows
UPDATE curated_lists SET
    title = coalesce(sqlc.narg('title'), title),
    description = coalesce(sqlc.narg('description'), description),
    updated_at = now()
WHERE id = @id AND workspace_id = @workspace_id;

-- name: PublishCuratedList :execrows
-- Only the first publication counts: publishing again changes nothing.
UPDATE curated_lists SET published_at = now(), updated_at = now()
WHERE id = @id AND workspace_id = @workspace_id AND published_at IS NULL;

-- name: DeleteCuratedList :execrows
DELETE FROM curated_lists WHERE id = @id AND workspace_id = @workspace_id;

-- name: TouchCuratedList :exec
UPDATE curated_lists SET updated_at = now() WHERE id = @id AND workspace_id = @workspace_id;

-- name: CuratedListItems :many
SELECT product_id, comment, position FROM curated_list_items
WHERE list_id = @list_id AND workspace_id = @workspace_id
ORDER BY position, product_id;

-- name: AddCuratedListItem :execrows
-- Goes to the end of the list; a product already in it is left as is.
INSERT INTO curated_list_items (list_id, workspace_id, product_id, comment, position)
SELECT sqlc.arg(list_id)::uuid, sqlc.arg(workspace_id)::uuid, sqlc.arg(product_id)::uuid,
       sqlc.arg(comment)::text, coalesce(max(li.position), 0) + 1
FROM curated_list_items li
WHERE li.list_id = sqlc.arg(list_id)::uuid AND li.workspace_id = sqlc.arg(workspace_id)::uuid
ON CONFLICT (list_id, product_id) DO NOTHING;

-- name: CommentCuratedListItem :execrows
UPDATE curated_list_items SET comment = @comment
WHERE list_id = @list_id AND workspace_id = @workspace_id AND product_id = @product_id;

-- name: RemoveCuratedListItem :execrows
DELETE FROM curated_list_items
WHERE list_id = @list_id AND workspace_id = @workspace_id AND product_id = @product_id;

-- name: ReorderCuratedListItems :execrows
UPDATE curated_list_items li SET position = o.position
FROM unnest(@product_ids::uuid[]) WITH ORDINALITY AS o(product_id, position)
WHERE li.list_id = @list_id AND li.workspace_id = @workspace_id AND li.product_id = o.product_id;

-- name: RecordListImports :exec
INSERT INTO list_imports (list_id, workspace_id, user_id, product_id)
SELECT sqlc.arg(list_id)::uuid, sqlc.arg(workspace_id)::uuid, sqlc.arg(user_id)::uuid,
       unnest(sqlc.arg(product_ids)::uuid[])
ON CONFLICT DO NOTHING;

-- name: ListImportersByProduct :many
SELECT product_id, count(*) AS importers FROM list_imports
WHERE list_id = @list_id AND workspace_id = @workspace_id
GROUP BY product_id;

-- name: ListImporters :many
SELECT user_id, count(*) AS products, max(imported_at)::timestamptz AS last_imported_at FROM list_imports
WHERE list_id = @list_id AND workspace_id = @workspace_id
GROUP BY user_id
ORDER BY last_imported_at DESC;

-- name: PublishedListImports :many
-- Every import of the published lists, for the results per list.
SELECT i.list_id, i.user_id, i.product_id, i.imported_at
FROM list_imports i
JOIN curated_lists l ON l.id = i.list_id AND l.workspace_id = i.workspace_id
WHERE i.workspace_id = @workspace_id AND l.published_at IS NOT NULL;
