-- Credencial da Open API de afiliados da Shopee. Pertence ao usuário, não ao
-- workspace, porque a comissão é dele. O Secret fica cifrado com envelope
-- encryption (internal/platform/crypto): `secret_cifrado` com a DEK, e a DEK
-- cifrada pela chave mestra `kek_id`.

-- +goose Up
CREATE TYPE credencial_status AS ENUM ('conectado', 'invalido', 'expirado');

CREATE TABLE credenciais_shopee (
    usuario_id     uuid PRIMARY KEY REFERENCES usuarios (id) ON DELETE CASCADE,
    app_id         text NOT NULL,
    secret_cifrado bytea NOT NULL,
    dek_cifrada    bytea NOT NULL,
    kek_id         text NOT NULL,
    status         credencial_status NOT NULL,
    verificado_em  timestamptz NOT NULL,
    criado_em      timestamptz NOT NULL DEFAULT now(),
    atualizado_em  timestamptz NOT NULL DEFAULT now()
);

GRANT SELECT, INSERT, UPDATE, DELETE ON credenciais_shopee TO parceiros_app;

ALTER TABLE credenciais_shopee ENABLE ROW LEVEL SECURITY;
ALTER TABLE credenciais_shopee FORCE ROW LEVEL SECURITY;

-- Só o próprio usuário enxerga e altera a sua credencial, em qualquer workspace.
CREATE POLICY credenciais_shopee_dono ON credenciais_shopee
    USING (usuario_id = app_usuario_id())
    WITH CHECK (usuario_id = app_usuario_id());

-- +goose Down
DROP TABLE credenciais_shopee;
DROP TYPE credencial_status;
