-- Pluggable authentication and English session settings.
--
-- 1. The RLS session settings and helper functions move to English:
--      app.usuario_id         -> app.user_id                  app_user_id()
--      app.zitadel_sub        -> app.auth_subject             app_auth_subject()
--      app.convite_hash       -> app.invite_hash              app_invite_hash()
--      app.assinatura_externa -> app.external_subscription_id app_external_subscription_id()
--      app_gestor()           -> app_is_manager()
--      app_dono()             -> app_is_owner()
--    Policies reference functions by oid, so renaming keeps them working.
--    Function bodies are text and are rewritten.
--
-- 2. A user belongs to an identity provider: usuarios.zitadel_sub becomes the
--    pair (auth_provider, auth_subject). Existing users came from the OIDC
--    provider (Zitadel).
--
-- 3. Tables of the internal identity provider (AUTH_PROVIDER=internal):
--    accounts with email and password, sessions and one-time tokens (email
--    verification and password reset). Only token hashes are stored.
--
--      app.auth_provider    provider of the identity in the request
--      app.auth_email       account being signed in or signed up
--      app.session_hash     session being checked (hex)
--      app.auth_token_hash  one-time token being used (hex)

-- +goose Up
ALTER FUNCTION app_usuario_id() RENAME TO app_user_id;
CREATE OR REPLACE FUNCTION app_user_id() RETURNS uuid
    LANGUAGE sql STABLE
    AS $$ SELECT nullif(current_setting('app.user_id', true), '')::uuid $$;

ALTER FUNCTION app_zitadel_sub() RENAME TO app_auth_subject;
CREATE OR REPLACE FUNCTION app_auth_subject() RETURNS text
    LANGUAGE sql STABLE
    AS $$ SELECT nullif(current_setting('app.auth_subject', true), '') $$;

ALTER FUNCTION app_convite_hash() RENAME TO app_invite_hash;
CREATE OR REPLACE FUNCTION app_invite_hash() RETURNS bytea
    LANGUAGE sql STABLE
    AS $$ SELECT decode(nullif(current_setting('app.invite_hash', true), ''), 'hex') $$;

ALTER FUNCTION app_assinatura_externa() RENAME TO app_external_subscription_id;
CREATE OR REPLACE FUNCTION app_external_subscription_id() RETURNS text
    LANGUAGE sql STABLE
    AS $$ SELECT nullif(current_setting('app.external_subscription_id', true), '') $$;

ALTER FUNCTION app_gestor() RENAME TO app_is_manager;
CREATE OR REPLACE FUNCTION app_is_manager() RETURNS boolean
    LANGUAGE sql STABLE
    AS $$
        SELECT EXISTS (
            SELECT 1 FROM membros
            WHERE workspace_id = app_workspace_id()
              AND usuario_id = app_user_id()
              AND papel IN ('dono', 'mentor')
        )
    $$;

ALTER FUNCTION app_dono() RENAME TO app_is_owner;
CREATE OR REPLACE FUNCTION app_is_owner() RETURNS boolean
    LANGUAGE sql STABLE
    AS $$
        SELECT EXISTS (
            SELECT 1 FROM membros
            WHERE workspace_id = app_workspace_id()
              AND usuario_id = app_user_id()
              AND papel = 'dono'
        )
    $$;

CREATE FUNCTION app_auth_provider() RETURNS text
    LANGUAGE sql STABLE
    AS $$ SELECT nullif(current_setting('app.auth_provider', true), '') $$;

CREATE FUNCTION app_auth_email() RETURNS text
    LANGUAGE sql STABLE
    AS $$ SELECT nullif(current_setting('app.auth_email', true), '') $$;

CREATE FUNCTION app_session_hash() RETURNS bytea
    LANGUAGE sql STABLE
    AS $$ SELECT decode(nullif(current_setting('app.session_hash', true), ''), 'hex') $$;

CREATE FUNCTION app_auth_token_hash() RETURNS bytea
    LANGUAGE sql STABLE
    AS $$ SELECT decode(nullif(current_setting('app.auth_token_hash', true), ''), 'hex') $$;

-- The internal account of the request, when the identity is internal.
CREATE FUNCTION app_auth_account_id() RETURNS uuid
    LANGUAGE sql STABLE
    AS $$ SELECT CASE WHEN app_auth_provider() = 'internal' THEN app_auth_subject()::uuid END $$;

-- Users: provider + subject.
ALTER TABLE usuarios RENAME COLUMN zitadel_sub TO auth_subject;
ALTER TABLE usuarios ADD COLUMN auth_provider text NOT NULL DEFAULT 'oidc';
ALTER TABLE usuarios ALTER COLUMN auth_provider DROP DEFAULT;
ALTER TABLE usuarios DROP CONSTRAINT usuarios_zitadel_sub_key;
ALTER TABLE usuarios ADD CONSTRAINT usuarios_auth_identity_key UNIQUE (auth_provider, auth_subject);

DROP POLICY usuarios_select ON usuarios;
DROP POLICY usuarios_insert ON usuarios;
DROP POLICY usuarios_update ON usuarios;
CREATE POLICY usuarios_select ON usuarios FOR SELECT
    USING (
        id = app_user_id()
        OR (auth_provider = app_auth_provider() AND auth_subject = app_auth_subject())
        OR id IN (SELECT usuario_id FROM membros WHERE workspace_id = app_workspace_id())
    );
CREATE POLICY usuarios_insert ON usuarios FOR INSERT
    WITH CHECK (auth_provider = app_auth_provider() AND auth_subject = app_auth_subject());
CREATE POLICY usuarios_update ON usuarios FOR UPDATE
    USING (id = app_user_id() OR (auth_provider = app_auth_provider() AND auth_subject = app_auth_subject()));

-- Internal identity provider.
CREATE TABLE auth_accounts (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    email             text NOT NULL UNIQUE CHECK (email = lower(email)),
    name              text NOT NULL CHECK (length(name) BETWEEN 1 AND 80),
    password_hash     text NOT NULL,
    email_verified_at timestamptz,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE auth_sessions (
    token_hash   bytea PRIMARY KEY,
    account_id   uuid NOT NULL REFERENCES auth_accounts (id) ON DELETE CASCADE,
    created_at   timestamptz NOT NULL DEFAULT now(),
    last_seen_at timestamptz NOT NULL DEFAULT now(),
    expires_at   timestamptz NOT NULL
);

CREATE INDEX auth_sessions_account ON auth_sessions (account_id);

CREATE TABLE auth_tokens (
    token_hash bytea PRIMARY KEY,
    account_id uuid NOT NULL REFERENCES auth_accounts (id) ON DELETE CASCADE,
    purpose    text NOT NULL CHECK (purpose IN ('verify_email', 'reset_password')),
    expires_at timestamptz NOT NULL,
    used_at    timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX auth_tokens_account ON auth_tokens (account_id);

GRANT SELECT, INSERT, UPDATE ON auth_accounts TO parceiros_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON auth_sessions TO parceiros_app;
GRANT SELECT, INSERT, UPDATE ON auth_tokens TO parceiros_app;

ALTER TABLE auth_accounts ENABLE ROW LEVEL SECURITY;
ALTER TABLE auth_accounts FORCE ROW LEVEL SECURITY;
ALTER TABLE auth_sessions ENABLE ROW LEVEL SECURITY;
ALTER TABLE auth_sessions FORCE ROW LEVEL SECURITY;
ALTER TABLE auth_tokens ENABLE ROW LEVEL SECURITY;
ALTER TABLE auth_tokens FORCE ROW LEVEL SECURITY;

-- accounts: the one being signed in (by email), the signed-in one, and the
-- account of the session or one-time token in hand.
CREATE POLICY auth_accounts_select ON auth_accounts FOR SELECT
    USING (
        email = app_auth_email()
        OR id = app_auth_account_id()
        OR id IN (SELECT account_id FROM auth_sessions WHERE token_hash = app_session_hash())
        OR id IN (SELECT account_id FROM auth_tokens WHERE token_hash = app_auth_token_hash())
    );
CREATE POLICY auth_accounts_insert ON auth_accounts FOR INSERT
    WITH CHECK (email = app_auth_email());
CREATE POLICY auth_accounts_update ON auth_accounts FOR UPDATE
    USING (id = app_auth_account_id());

-- sessions: the one in hand, or every session of the signed-in account.
CREATE POLICY auth_sessions_select ON auth_sessions FOR SELECT
    USING (token_hash = app_session_hash() OR account_id = app_auth_account_id());
CREATE POLICY auth_sessions_insert ON auth_sessions FOR INSERT
    WITH CHECK (account_id = app_auth_account_id());
CREATE POLICY auth_sessions_update ON auth_sessions FOR UPDATE
    USING (token_hash = app_session_hash());
CREATE POLICY auth_sessions_delete ON auth_sessions FOR DELETE
    USING (token_hash = app_session_hash() OR account_id = app_auth_account_id());

-- one-time tokens: created for the account, then used by whoever has them.
CREATE POLICY auth_tokens_select ON auth_tokens FOR SELECT
    USING (token_hash = app_auth_token_hash() OR account_id = app_auth_account_id());
CREATE POLICY auth_tokens_insert ON auth_tokens FOR INSERT
    WITH CHECK (account_id = app_auth_account_id());
CREATE POLICY auth_tokens_update ON auth_tokens FOR UPDATE
    USING (token_hash = app_auth_token_hash());

-- +goose Down
DROP POLICY auth_accounts_select ON auth_accounts;
DROP TABLE auth_tokens;
DROP TABLE auth_sessions;
DROP TABLE auth_accounts;

DROP POLICY usuarios_select ON usuarios;
DROP POLICY usuarios_insert ON usuarios;
DROP POLICY usuarios_update ON usuarios;
ALTER TABLE usuarios DROP CONSTRAINT usuarios_auth_identity_key;
ALTER TABLE usuarios DROP COLUMN auth_provider;
ALTER TABLE usuarios RENAME COLUMN auth_subject TO zitadel_sub;
ALTER TABLE usuarios ADD CONSTRAINT usuarios_zitadel_sub_key UNIQUE (zitadel_sub);

DROP FUNCTION app_auth_account_id();
DROP FUNCTION app_auth_token_hash();
DROP FUNCTION app_session_hash();
DROP FUNCTION app_auth_email();
DROP FUNCTION app_auth_provider();

ALTER FUNCTION app_is_owner() RENAME TO app_dono;
ALTER FUNCTION app_is_manager() RENAME TO app_gestor;
ALTER FUNCTION app_external_subscription_id() RENAME TO app_assinatura_externa;
CREATE OR REPLACE FUNCTION app_assinatura_externa() RETURNS text
    LANGUAGE sql STABLE
    AS $$ SELECT nullif(current_setting('app.assinatura_externa', true), '') $$;
ALTER FUNCTION app_invite_hash() RENAME TO app_convite_hash;
CREATE OR REPLACE FUNCTION app_convite_hash() RETURNS bytea
    LANGUAGE sql STABLE
    AS $$ SELECT decode(nullif(current_setting('app.convite_hash', true), ''), 'hex') $$;
ALTER FUNCTION app_auth_subject() RENAME TO app_zitadel_sub;
ALTER FUNCTION app_user_id() RENAME TO app_usuario_id;
CREATE OR REPLACE FUNCTION app_usuario_id() RETURNS uuid
    LANGUAGE sql STABLE
    AS $$ SELECT nullif(current_setting('app.usuario_id', true), '')::uuid $$;
-- The recreated policies use the new functions; recreate them as in 00002.
CREATE POLICY usuarios_select ON usuarios FOR SELECT
    USING (
        id = app_usuario_id()
        OR zitadel_sub = app_zitadel_sub()
        OR id IN (SELECT usuario_id FROM membros WHERE workspace_id = app_workspace_id())
    );
CREATE POLICY usuarios_insert ON usuarios FOR INSERT
    WITH CHECK (zitadel_sub = app_zitadel_sub());
CREATE POLICY usuarios_update ON usuarios FOR UPDATE
    USING (id = app_usuario_id() OR zitadel_sub = app_zitadel_sub());
CREATE OR REPLACE FUNCTION app_zitadel_sub() RETURNS text
    LANGUAGE sql STABLE
    AS $$ SELECT nullif(current_setting('app.zitadel_sub', true), '') $$;
CREATE OR REPLACE FUNCTION app_gestor() RETURNS boolean
    LANGUAGE sql STABLE
    AS $$
        SELECT EXISTS (
            SELECT 1 FROM membros
            WHERE workspace_id = app_workspace_id()
              AND usuario_id = app_usuario_id()
              AND papel IN ('dono', 'mentor')
        )
    $$;
CREATE OR REPLACE FUNCTION app_dono() RETURNS boolean
    LANGUAGE sql STABLE
    AS $$
        SELECT EXISTS (
            SELECT 1 FROM membros
            WHERE workspace_id = app_workspace_id()
              AND usuario_id = app_usuario_id()
              AND papel = 'dono'
        )
    $$;
