-- Queries of the media module. Every query filters by workspace_id, besides
-- the RLS (which shows the user their videos and the shared ones in the
-- workspace).

-- name: CreateEmbedVideo :one
-- Pasting the same video again returns the existing one (xmax <> 0 on the upsert).
INSERT INTO videos (workspace_id, owner_id, kind, platform, status, title, author, url, embed_id, thumbnail_url, verified_at)
VALUES (@workspace_id, @owner_id, 'embed', @platform, 'ready', @title, @author, @url, @embed_id, @thumbnail_url, now())
ON CONFLICT (workspace_id, owner_id, platform, embed_id) WHERE kind = 'embed'
DO UPDATE SET title = excluded.title, author = excluded.author, url = excluded.url,
    thumbnail_url = excluded.thumbnail_url, status = 'ready', verified_at = now(), updated_at = now()
RETURNING id, (xmax = 0)::boolean AS created;

-- name: CreateUploadVideo :one
INSERT INTO videos (id, workspace_id, owner_id, kind, platform, status, title, storage_key, upload_id,
                    file_name, content_type, size_bytes, usage_rights_at)
VALUES (@id, @workspace_id, @owner_id, 'upload', 'upload', 'uploading', @title, @storage_key, @upload_id,
        @file_name, @content_type, @size_bytes, now())
RETURNING *;

-- name: VideoByID :one
SELECT * FROM videos WHERE id = @id AND workspace_id = @workspace_id;

-- name: LockOwnVideo :one
SELECT * FROM videos WHERE id = @id AND workspace_id = @workspace_id AND owner_id = @owner_id FOR UPDATE;

-- name: WorkspaceVideos :many
-- The videos the user sees in the workspace: theirs and the shared ones.
SELECT * FROM videos
WHERE workspace_id = @workspace_id
  AND (NOT sqlc.arg(only_mine)::boolean OR owner_id = @owner_id)
ORDER BY created_at DESC, id
LIMIT 500;

-- name: VideosForTargets :many
-- The videos linked to any of these targets, with the target of each link.
SELECT sqlc.embed(v), vl.target_id FROM video_links vl
JOIN videos v ON v.id = vl.video_id AND v.workspace_id = vl.workspace_id
WHERE vl.workspace_id = @workspace_id
  AND vl.target_kind = @target_kind
  AND vl.target_id = ANY(@target_ids::uuid[])
ORDER BY vl.created_at, v.id;

-- name: LinksOfVideos :many
SELECT video_id, target_kind, target_id FROM video_links
WHERE workspace_id = @workspace_id AND video_id = ANY(@video_ids::uuid[])
ORDER BY created_at;

-- name: LinkVideo :execrows
INSERT INTO video_links (video_id, workspace_id, owner_id, target_kind, target_id)
VALUES (@video_id, @workspace_id, @owner_id, @target_kind, @target_id)
ON CONFLICT DO NOTHING;

-- name: UnlinkVideo :execrows
DELETE FROM video_links
WHERE workspace_id = @workspace_id AND video_id = @video_id AND target_kind = @target_kind AND target_id = @target_id;

-- name: UnlinkVideoTarget :exec
DELETE FROM video_links WHERE workspace_id = @workspace_id AND target_kind = @target_kind AND target_id = @target_id;

-- name: ShareVideo :execrows
UPDATE videos SET shared = @shared, updated_at = now()
WHERE id = @id AND workspace_id = @workspace_id AND owner_id = @owner_id;

-- name: RenameVideo :execrows
UPDATE videos SET title = @title, updated_at = now()
WHERE id = @id AND workspace_id = @workspace_id AND owner_id = @owner_id;

-- name: MarkVideoUploaded :exec
UPDATE videos SET status = 'processing', upload_id = NULL, size_bytes = @size_bytes, updated_at = now()
WHERE id = @id AND workspace_id = @workspace_id AND owner_id = @owner_id;

-- name: MarkVideoProcessed :exec
UPDATE videos SET status = 'ready', duration_s = @duration_s, width = @width, height = @height, updated_at = now()
WHERE id = @id AND workspace_id = @workspace_id AND owner_id = @owner_id;

-- name: SetVideoStatus :exec
UPDATE videos SET status = @status, updated_at = now()
WHERE id = @id AND workspace_id = @workspace_id AND owner_id = @owner_id;

-- name: MarkEmbedRevalidated :exec
UPDATE videos SET status = @status, title = coalesce(sqlc.narg('title'), title),
    author = coalesce(sqlc.narg('author'), author), thumbnail_url = coalesce(sqlc.narg('thumbnail_url'), thumbnail_url),
    verified_at = now(), updated_at = now()
WHERE id = @id AND workspace_id = @workspace_id AND owner_id = @owner_id;

-- name: DeleteVideo :execrows
DELETE FROM videos WHERE id = @id AND workspace_id = @workspace_id AND owner_id = @owner_id;

-- name: AddVideoUsage :one
-- Adds (or subtracts) bytes to the usage of the workspace and returns the new total.
INSERT INTO video_usage (workspace_id, bytes) VALUES (@workspace_id, greatest(sqlc.arg(delta)::bigint, 0))
ON CONFLICT (workspace_id) DO UPDATE SET bytes = greatest(video_usage.bytes + sqlc.arg(delta)::bigint, 0)
RETURNING bytes;

-- name: VideoUsage :one
SELECT coalesce((SELECT bytes FROM video_usage WHERE workspace_id = @workspace_id), 0)::bigint;
