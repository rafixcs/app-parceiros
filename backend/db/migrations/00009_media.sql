-- Media: the video library of each user in the workspace.
--
-- There are two kinds of video:
--   embed   reference from another creator (YouTube or TikTok). Only the
--           metadata of the official oEmbed is stored (title, author,
--           thumbnail and URL); the video plays in the official player. The
--           file is never downloaded.
--   upload  the user's own video, sent straight to the bucket by presigned
--           URL, with the statement of usage rights. The worker makes the
--           thumbnail and a 720p preview with ffmpeg.
--
-- The video belongs to its owner. Owner and mentor can share it with the
-- workspace (shared = true); attaching a video to a curated list shares it.
-- The links tie the video to catalog products and to lists.

-- +goose Up
CREATE TYPE video_kind AS ENUM ('embed', 'upload');
-- uploading: upload in progress; processing: queued for process_video;
-- ready: available; failed: the file could not be processed;
-- unavailable: the embed is gone from the platform.
CREATE TYPE video_status AS ENUM ('uploading', 'processing', 'ready', 'failed', 'unavailable');
CREATE TYPE video_target AS ENUM ('product', 'list');

CREATE TABLE videos (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id    uuid NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    owner_id        uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    kind            video_kind NOT NULL,
    platform        text NOT NULL CHECK (platform IN ('youtube', 'tiktok', 'upload')),
    status          video_status NOT NULL,
    title           text NOT NULL DEFAULT '' CHECK (length(title) <= 200),
    author          text NOT NULL DEFAULT '' CHECK (length(author) <= 200),
    shared          boolean NOT NULL DEFAULT false,
    -- embed
    url             text,
    embed_id        text,
    thumbnail_url   text,
    verified_at     timestamptz,
    -- upload
    storage_key     text,
    upload_id       text,
    file_name       text,
    content_type    text,
    size_bytes      bigint NOT NULL DEFAULT 0 CHECK (size_bytes >= 0),
    duration_s      integer,
    width           integer,
    height          integer,
    usage_rights_at timestamptz,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    UNIQUE (id, workspace_id, owner_id),
    CHECK (kind <> 'embed' OR (url IS NOT NULL AND embed_id IS NOT NULL)),
    CHECK (kind <> 'upload' OR (storage_key IS NOT NULL AND usage_rights_at IS NOT NULL))
);

CREATE INDEX videos_workspace ON videos (workspace_id, created_at DESC);
-- Pasting the same reference video again returns the existing one.
CREATE UNIQUE INDEX videos_unique_embed ON videos (workspace_id, owner_id, platform, embed_id) WHERE kind = 'embed';

CREATE TABLE video_links (
    video_id     uuid NOT NULL,
    workspace_id uuid NOT NULL,
    owner_id     uuid NOT NULL,
    target_kind  video_target NOT NULL,
    target_id    uuid NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (video_id, target_kind, target_id),
    FOREIGN KEY (video_id, workspace_id, owner_id)
        REFERENCES videos (id, workspace_id, owner_id) ON DELETE CASCADE
);

CREATE INDEX video_links_target ON video_links (workspace_id, target_kind, target_id);

-- Upload bytes used (or reserved by uploads in progress) per workspace, for
-- the plan quota. Updated in the same transaction as the video.
CREATE TABLE video_usage (
    workspace_id uuid PRIMARY KEY REFERENCES workspaces (id) ON DELETE CASCADE,
    bytes        bigint NOT NULL DEFAULT 0 CHECK (bytes >= 0)
);

-- Upload quota per plan, in bytes. Provisional.
INSERT INTO plan_limits (plan, key, value) VALUES
    ('solo', 'video_bytes', 5368709120),        -- 5 GB
    ('mentorship', 'video_bytes', 53687091200)  -- 50 GB
ON CONFLICT DO NOTHING;

GRANT SELECT, INSERT, UPDATE, DELETE ON videos, video_links TO parceiros_app;
GRANT SELECT, INSERT, UPDATE ON video_usage TO parceiros_app;

ALTER TABLE videos ENABLE ROW LEVEL SECURITY;
ALTER TABLE videos FORCE ROW LEVEL SECURITY;
ALTER TABLE video_links ENABLE ROW LEVEL SECURITY;
ALTER TABLE video_links FORCE ROW LEVEL SECURITY;
ALTER TABLE video_usage ENABLE ROW LEVEL SECURITY;
ALTER TABLE video_usage FORCE ROW LEVEL SECURITY;

-- videos: the owner sees and changes theirs; the workspace sees the shared
-- ones. Only owner and mentor share.
CREATE POLICY videos_select ON videos FOR SELECT
    USING (workspace_id = app_workspace_id() AND (owner_id = app_user_id() OR shared));
CREATE POLICY videos_insert ON videos FOR INSERT
    WITH CHECK (workspace_id = app_workspace_id() AND owner_id = app_user_id() AND NOT shared);
CREATE POLICY videos_update ON videos FOR UPDATE
    USING (workspace_id = app_workspace_id() AND owner_id = app_user_id())
    WITH CHECK (workspace_id = app_workspace_id() AND owner_id = app_user_id()
                AND (NOT shared OR app_is_manager()));
CREATE POLICY videos_delete ON videos FOR DELETE
    USING (workspace_id = app_workspace_id() AND owner_id = app_user_id());

-- links: visible with the video (the subquery goes through the policy of
-- videos). The video owner creates and deletes theirs; owner and mentor also
-- remove videos from lists.
CREATE POLICY video_links_select ON video_links FOR SELECT
    USING (workspace_id = app_workspace_id()
           AND EXISTS (SELECT 1 FROM videos v WHERE v.id = video_id));
CREATE POLICY video_links_insert ON video_links FOR INSERT
    WITH CHECK (workspace_id = app_workspace_id() AND owner_id = app_user_id()
                AND (target_kind <> 'list' OR app_is_manager()));
CREATE POLICY video_links_delete ON video_links FOR DELETE
    USING (workspace_id = app_workspace_id()
           AND (owner_id = app_user_id() OR (target_kind = 'list' AND app_is_manager())));

CREATE POLICY video_usage_workspace ON video_usage
    USING (workspace_id = app_workspace_id())
    WITH CHECK (workspace_id = app_workspace_id());

-- +goose Down
DROP TABLE video_usage;
DROP TABLE video_links;
DROP TABLE videos;
DELETE FROM plan_limits WHERE key = 'video_bytes';
DROP TYPE video_target;
DROP TYPE video_status;
DROP TYPE video_kind;
