-- Accounts: users, workspaces, members and invites, with RLS.
--
-- The API opens every transaction with `SET LOCAL ROLE parceiros_app` and sets
-- the session settings below (see internal/infrastructure/database). The RLS
-- policies are the second barrier against leaks between workspaces; the first
-- is the workspace filter in every query.
--
--   app.user_id        signed-in user
--   app.workspace_id   workspace of the request (only after checking the member)
--   app.auth_provider  identity provider of the request (oidc, internal, dev)
--   app.auth_subject   subject at the provider, used on first access
--   app.invite_hash    hash of an invite token, used to view and accept invites

-- +goose Up
-- +goose StatementBegin
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'parceiros_app') THEN
        CREATE ROLE parceiros_app NOLOGIN;
    END IF;
END
$$;
-- +goose StatementEnd

GRANT parceiros_app TO CURRENT_USER;
GRANT USAGE ON SCHEMA public TO parceiros_app;

CREATE FUNCTION app_user_id() RETURNS uuid
    LANGUAGE sql STABLE
    AS $$ SELECT nullif(current_setting('app.user_id', true), '')::uuid $$;

CREATE FUNCTION app_workspace_id() RETURNS uuid
    LANGUAGE sql STABLE
    AS $$ SELECT nullif(current_setting('app.workspace_id', true), '')::uuid $$;

CREATE FUNCTION app_auth_provider() RETURNS text
    LANGUAGE sql STABLE
    AS $$ SELECT nullif(current_setting('app.auth_provider', true), '') $$;

CREATE FUNCTION app_auth_subject() RETURNS text
    LANGUAGE sql STABLE
    AS $$ SELECT nullif(current_setting('app.auth_subject', true), '') $$;

CREATE FUNCTION app_invite_hash() RETURNS bytea
    LANGUAGE sql STABLE
    AS $$ SELECT decode(nullif(current_setting('app.invite_hash', true), ''), 'hex') $$;

CREATE TYPE workspace_kind AS ENUM ('personal', 'mentorship');
CREATE TYPE member_role AS ENUM ('owner', 'mentor', 'affiliate');

-- A user belongs to one identity provider: (auth_provider, auth_subject).
CREATE TABLE users (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    auth_provider  text NOT NULL,
    auth_subject   text NOT NULL,
    name           text NOT NULL,
    email          text NOT NULL,
    email_verified boolean NOT NULL DEFAULT false,
    created_at     timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT users_auth_identity_key UNIQUE (auth_provider, auth_subject)
);

-- The access of a workspace lasts until access_until: the end of the 7-day
-- trial, or of the paid cycle plus a grace period (see 00011_subscriptions).
CREATE TABLE workspaces (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    kind         workspace_kind NOT NULL,
    name         text NOT NULL CHECK (length(name) BETWEEN 1 AND 80),
    photo_url    text,
    owner_id     uuid NOT NULL REFERENCES users (id),
    plan         text NOT NULL,
    access_until timestamptz NOT NULL DEFAULT now() + interval '7 days',
    -- last confirmed payment; null during the trial
    paid_at      timestamptz,
    -- seats bought by a mentorship; null before the first payment, when the
    -- trial limit applies (plan_limits.trial_seats)
    seats        integer CHECK (seats >= 0),
    created_at   timestamptz NOT NULL DEFAULT now()
);

-- Each user has a single personal workspace.
CREATE UNIQUE INDEX workspaces_one_personal ON workspaces (owner_id) WHERE kind = 'personal';

CREATE TABLE members (
    workspace_id   uuid NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    user_id        uuid NOT NULL REFERENCES users (id),
    role           member_role NOT NULL,
    -- whether the member lets owner and mentors see their aggregated results
    -- (LGPD consent)
    shares_results boolean NOT NULL DEFAULT false,
    joined_at      timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, user_id)
);

CREATE INDEX members_user ON members (user_id);

-- The invite token is not stored, only its SHA-256.
CREATE TABLE invites (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    email        text,
    token_hash   bytea NOT NULL UNIQUE,
    expires_at   timestamptz NOT NULL,
    used_by      uuid REFERENCES users (id),
    used_at      timestamptz,
    revoked_at   timestamptz,
    created_by   uuid NOT NULL REFERENCES users (id),
    created_at   timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX invites_workspace ON invites (workspace_id);

-- Limits and prices per plan (seats, video quota, lists). Changing a value
-- here needs no deploy. The values are provisional.
CREATE TABLE plan_limits (
    plan  text NOT NULL,
    key   text NOT NULL,
    value bigint NOT NULL,
    PRIMARY KEY (plan, key)
);

INSERT INTO plan_limits (plan, key, value) VALUES
    ('solo', 'seats', 0),
    ('mentorship', 'seats', 200);

-- Whether the user of the request manages the workspace of the request
-- (owner or mentor), and whether they own it.
CREATE FUNCTION app_is_manager() RETURNS boolean
    LANGUAGE sql STABLE
    AS $$
        SELECT EXISTS (
            SELECT 1 FROM members
            WHERE workspace_id = app_workspace_id()
              AND user_id = app_user_id()
              AND role IN ('owner', 'mentor')
        )
    $$;

CREATE FUNCTION app_is_owner() RETURNS boolean
    LANGUAGE sql STABLE
    AS $$
        SELECT EXISTS (
            SELECT 1 FROM members
            WHERE workspace_id = app_workspace_id()
              AND user_id = app_user_id()
              AND role = 'owner'
        )
    $$;

GRANT SELECT, INSERT, UPDATE ON users TO parceiros_app;
GRANT SELECT, INSERT, UPDATE ON workspaces TO parceiros_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON members TO parceiros_app;
GRANT SELECT, INSERT, UPDATE ON invites TO parceiros_app;
GRANT SELECT ON plan_limits TO parceiros_app;

ALTER TABLE users ENABLE ROW LEVEL SECURITY;
ALTER TABLE users FORCE ROW LEVEL SECURITY;
ALTER TABLE workspaces ENABLE ROW LEVEL SECURITY;
ALTER TABLE workspaces FORCE ROW LEVEL SECURITY;
ALTER TABLE members ENABLE ROW LEVEL SECURITY;
ALTER TABLE members FORCE ROW LEVEL SECURITY;
ALTER TABLE invites ENABLE ROW LEVEL SECURITY;
ALTER TABLE invites FORCE ROW LEVEL SECURITY;

-- members: those of the current workspace and the user's own memberships (for
-- the workspace switcher and to leave a workspace).
CREATE POLICY members_select ON members FOR SELECT
    USING (workspace_id = app_workspace_id() OR user_id = app_user_id());
CREATE POLICY members_insert ON members FOR INSERT
    WITH CHECK (workspace_id = app_workspace_id());
CREATE POLICY members_update ON members FOR UPDATE
    USING (workspace_id = app_workspace_id());
CREATE POLICY members_delete ON members FOR DELETE
    USING (workspace_id = app_workspace_id());

-- invites: those of the current workspace, or the one whose token the user has.
CREATE POLICY invites_select ON invites FOR SELECT
    USING (workspace_id = app_workspace_id() OR token_hash = app_invite_hash());
CREATE POLICY invites_insert ON invites FOR INSERT
    WITH CHECK (workspace_id = app_workspace_id());
CREATE POLICY invites_update ON invites FOR UPDATE
    USING (workspace_id = app_workspace_id());

-- workspaces: the current one, those the user created or belongs to, and the
-- one of an invite in hand (to show its name before accepting).
CREATE POLICY workspaces_select ON workspaces FOR SELECT
    USING (
        id = app_workspace_id()
        OR owner_id = app_user_id()
        OR id IN (SELECT workspace_id FROM members WHERE user_id = app_user_id())
        OR id IN (SELECT workspace_id FROM invites WHERE token_hash = app_invite_hash())
    );
CREATE POLICY workspaces_insert ON workspaces FOR INSERT
    WITH CHECK (owner_id = app_user_id());
CREATE POLICY workspaces_update ON workspaces FOR UPDATE
    USING (id = app_workspace_id());

-- users: the user themself (by id or by identity on first access) and the
-- members of the current workspace.
CREATE POLICY users_select ON users FOR SELECT
    USING (
        id = app_user_id()
        OR (auth_provider = app_auth_provider() AND auth_subject = app_auth_subject())
        OR id IN (SELECT user_id FROM members WHERE workspace_id = app_workspace_id())
    );
CREATE POLICY users_insert ON users FOR INSERT
    WITH CHECK (auth_provider = app_auth_provider() AND auth_subject = app_auth_subject());
CREATE POLICY users_update ON users FOR UPDATE
    USING (id = app_user_id() OR (auth_provider = app_auth_provider() AND auth_subject = app_auth_subject()));

-- +goose Down
-- These policies query other tables and must go before them.
DROP POLICY workspaces_select ON workspaces;
DROP POLICY users_select ON users;
DROP FUNCTION app_is_owner();
DROP FUNCTION app_is_manager();
DROP TABLE plan_limits;
DROP TABLE invites;
DROP TABLE members;
DROP TABLE workspaces;
DROP TABLE users;
DROP TYPE member_role;
DROP TYPE workspace_kind;
DROP FUNCTION app_invite_hash();
DROP FUNCTION app_auth_subject();
DROP FUNCTION app_auth_provider();
DROP FUNCTION app_workspace_id();
DROP FUNCTION app_user_id();
REVOKE USAGE ON SCHEMA public FROM parceiros_app;
