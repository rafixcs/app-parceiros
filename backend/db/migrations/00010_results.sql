-- Results: the conversions Shopee attributes to each affiliate's links, read
-- from the conversionReport with their credential (sync_conversions job).
--
-- A conversion belongs to the user, because the commission is theirs, and
-- lands in the workspace marked in the link's subId (see
-- domain.WorkspaceMark). Without a known mark, it goes to the personal
-- workspace. One row per order item.
--
-- Who sees them:
--   - the user: in the workspace of the request, or in all of them when the
--     transaction has no workspace (the sync job, which is who writes);
--   - owner and mentor of the workspace: those of members who consented
--     (members.shares_results). The API only returns them aggregates.

-- +goose Up
CREATE TYPE order_status AS ENUM ('unpaid', 'pending', 'completed', 'cancelled');

CREATE TABLE conversions (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id          uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    workspace_id     uuid NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    source           text NOT NULL,
    conversion_id    bigint NOT NULL,
    order_id         text NOT NULL,
    item_id          bigint NOT NULL,
    model_id         bigint NOT NULL DEFAULT 0,
    -- catalog product, when the item is in it
    product_id       uuid REFERENCES products (id),
    item_name        text NOT NULL,
    shop_name        text NOT NULL,
    sub_id           text NOT NULL DEFAULT '',
    -- channel of the subId; null when the link did not come from the app
    channel          channel,
    status           order_status NOT NULL,
    quantity         integer NOT NULL CHECK (quantity >= 0),
    -- price × quantity
    amount_cents     bigint NOT NULL,
    commission_cents bigint NOT NULL,
    occurred_at      timestamptz NOT NULL,
    clicked_at       timestamptz,
    synced_at        timestamptz NOT NULL DEFAULT now(),
    UNIQUE (user_id, source, order_id, item_id, model_id)
);

CREATE INDEX conversions_workspace ON conversions (workspace_id, occurred_at);
CREATE INDEX conversions_user ON conversions (user_id, workspace_id, occurred_at);

-- State of each user's synchronization.
CREATE TABLE conversion_syncs (
    user_id      uuid PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    status       text NOT NULL CHECK (status IN ('syncing', 'ok', 'error', 'no_credential')),
    requested_at timestamptz NOT NULL DEFAULT now(),
    finished_at  timestamptz,
    conversions  integer NOT NULL DEFAULT 0,
    error        text
);

GRANT SELECT, INSERT, UPDATE, DELETE ON conversions, conversion_syncs TO parceiros_app;

ALTER TABLE conversions ENABLE ROW LEVEL SECURITY;
ALTER TABLE conversions FORCE ROW LEVEL SECURITY;
ALTER TABLE conversion_syncs ENABLE ROW LEVEL SECURITY;
ALTER TABLE conversion_syncs FORCE ROW LEVEL SECURITY;

CREATE POLICY conversions_select ON conversions FOR SELECT
    USING (
        (user_id = app_user_id()
         AND (app_workspace_id() IS NULL OR workspace_id = app_workspace_id()))
        OR (workspace_id = app_workspace_id()
            AND app_is_manager()
            AND EXISTS (SELECT 1 FROM members m
                        WHERE m.workspace_id = conversions.workspace_id
                          AND m.user_id = conversions.user_id
                          AND m.shares_results))
    );
-- Only the sync (a transaction without workspace) writes.
CREATE POLICY conversions_insert ON conversions FOR INSERT
    WITH CHECK (user_id = app_user_id() AND app_workspace_id() IS NULL);
CREATE POLICY conversions_update ON conversions FOR UPDATE
    USING (user_id = app_user_id() AND app_workspace_id() IS NULL)
    WITH CHECK (user_id = app_user_id() AND app_workspace_id() IS NULL);
CREATE POLICY conversions_delete ON conversions FOR DELETE
    USING (user_id = app_user_id() AND app_workspace_id() IS NULL);

CREATE POLICY conversion_syncs_owner ON conversion_syncs
    USING (user_id = app_user_id())
    WITH CHECK (user_id = app_user_id());

-- +goose Down
DROP POLICY conversions_select ON conversions;
DROP TABLE conversion_syncs;
DROP TABLE conversions;
DROP TYPE order_status;
