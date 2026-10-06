-- Credential of the Shopee affiliate Open API. It belongs to the user, not to
-- the workspace, because the commission is theirs. The secret is stored with
-- envelope encryption (internal/infrastructure/crypto): `encrypted_secret`
-- with the DEK, and the DEK encrypted by the master key `kek_id`.

-- +goose Up
CREATE TYPE credential_status AS ENUM ('connected', 'invalid', 'expired');

CREATE TABLE shopee_credentials (
    user_id          uuid PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    app_id           text NOT NULL,
    encrypted_secret bytea NOT NULL,
    encrypted_dek    bytea NOT NULL,
    kek_id           text NOT NULL,
    status           credential_status NOT NULL,
    verified_at      timestamptz NOT NULL,
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now()
);

GRANT SELECT, INSERT, UPDATE, DELETE ON shopee_credentials TO parceiros_app;

ALTER TABLE shopee_credentials ENABLE ROW LEVEL SECURITY;
ALTER TABLE shopee_credentials FORCE ROW LEVEL SECURITY;

-- Only the user sees and changes their credential, in any workspace.
CREATE POLICY shopee_credentials_owner ON shopee_credentials
    USING (user_id = app_user_id())
    WITH CHECK (user_id = app_user_id());

-- +goose Down
DROP TABLE shopee_credentials;
DROP TYPE credential_status;
