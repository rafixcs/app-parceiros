-- Curation: lists of products a mentor builds, with a comment per product, and
-- publishes to the group. The affiliate imports the list (all of it or part)
-- into their own saved items, and each import is recorded for the mentor's
-- dashboard.
--
-- Drafts only show to owner and mentor; once published, the list shows to the
-- whole workspace. Who manages the group is decided by app_is_manager().

-- +goose Up
CREATE TABLE curated_lists (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    author_id    uuid NOT NULL REFERENCES users (id),
    title        text NOT NULL CHECK (length(title) BETWEEN 1 AND 120),
    description  text NOT NULL DEFAULT '' CHECK (length(description) <= 2000),
    published_at timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    UNIQUE (id, workspace_id)
);

CREATE INDEX curated_lists_workspace ON curated_lists (workspace_id, created_at DESC);

CREATE TABLE curated_list_items (
    list_id      uuid NOT NULL,
    workspace_id uuid NOT NULL,
    product_id   uuid NOT NULL REFERENCES products (id),
    comment      text NOT NULL DEFAULT '' CHECK (length(comment) <= 1000),
    position     integer NOT NULL,
    PRIMARY KEY (list_id, product_id),
    FOREIGN KEY (list_id, workspace_id)
        REFERENCES curated_lists (id, workspace_id) ON DELETE CASCADE
);

-- One row per imported product: who imported each list and each product (the
-- mentor's dashboard and the results per list).
CREATE TABLE list_imports (
    list_id      uuid NOT NULL,
    workspace_id uuid NOT NULL,
    user_id      uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    product_id   uuid NOT NULL REFERENCES products (id),
    imported_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (list_id, user_id, product_id),
    FOREIGN KEY (list_id, workspace_id)
        REFERENCES curated_lists (id, workspace_id) ON DELETE CASCADE
);

CREATE INDEX list_imports_user ON list_imports (workspace_id, user_id);

INSERT INTO plan_limits (plan, key, value) VALUES
    ('solo', 'lists', 0),
    ('mentorship', 'lists', 200)
ON CONFLICT DO NOTHING;

GRANT SELECT, INSERT, UPDATE, DELETE ON curated_lists, curated_list_items TO parceiros_app;
GRANT SELECT, INSERT ON list_imports TO parceiros_app;

ALTER TABLE curated_lists ENABLE ROW LEVEL SECURITY;
ALTER TABLE curated_lists FORCE ROW LEVEL SECURITY;
ALTER TABLE curated_list_items ENABLE ROW LEVEL SECURITY;
ALTER TABLE curated_list_items FORCE ROW LEVEL SECURITY;
ALTER TABLE list_imports ENABLE ROW LEVEL SECURITY;
ALTER TABLE list_imports FORCE ROW LEVEL SECURITY;

-- lists: the workspace sees the published ones; owner and mentor also see the
-- drafts and are the only ones who write.
CREATE POLICY curated_lists_select ON curated_lists FOR SELECT
    USING (workspace_id = app_workspace_id() AND (published_at IS NOT NULL OR app_is_manager()));
CREATE POLICY curated_lists_insert ON curated_lists FOR INSERT
    WITH CHECK (workspace_id = app_workspace_id() AND author_id = app_user_id() AND app_is_manager());
CREATE POLICY curated_lists_update ON curated_lists FOR UPDATE
    USING (workspace_id = app_workspace_id() AND app_is_manager());
CREATE POLICY curated_lists_delete ON curated_lists FOR DELETE
    USING (workspace_id = app_workspace_id() AND app_is_manager());

-- list items: visible with the list (the subquery goes through the policy of
-- curated_lists).
CREATE POLICY curated_list_items_select ON curated_list_items FOR SELECT
    USING (workspace_id = app_workspace_id()
           AND EXISTS (SELECT 1 FROM curated_lists l WHERE l.id = list_id));
CREATE POLICY curated_list_items_insert ON curated_list_items FOR INSERT
    WITH CHECK (workspace_id = app_workspace_id() AND app_is_manager());
CREATE POLICY curated_list_items_update ON curated_list_items FOR UPDATE
    USING (workspace_id = app_workspace_id() AND app_is_manager());
CREATE POLICY curated_list_items_delete ON curated_list_items FOR DELETE
    USING (workspace_id = app_workspace_id() AND app_is_manager());

-- imports: the affiliate sees and writes their own; owner and mentor see the
-- group's.
CREATE POLICY list_imports_select ON list_imports FOR SELECT
    USING (workspace_id = app_workspace_id() AND (user_id = app_user_id() OR app_is_manager()));
CREATE POLICY list_imports_insert ON list_imports FOR INSERT
    WITH CHECK (workspace_id = app_workspace_id() AND user_id = app_user_id()
                AND EXISTS (SELECT 1 FROM curated_lists l WHERE l.id = list_id AND l.published_at IS NOT NULL));

-- +goose Down
DROP TABLE list_imports;
DROP TABLE curated_list_items;
DROP TABLE curated_lists;
DELETE FROM plan_limits WHERE key = 'lists';
