-- Internal identity provider (AUTH_PROVIDER=internal): accounts with email and
-- password, sessions and one-time tokens (email verification and password
-- reset). Only token hashes are stored.
--
--   app.auth_email       account being signed in or signed up
--   app.session_hash     session being checked (hex)
--   app.auth_token_hash  one-time token being used (hex)

-- +goose Up
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
DROP FUNCTION app_auth_account_id();
DROP FUNCTION app_auth_token_hash();
DROP FUNCTION app_session_hash();
DROP FUNCTION app_auth_email();
