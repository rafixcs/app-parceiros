-- Contas: usuários, workspaces, membros e convites, com RLS.
--
-- A API abre cada transação com `SET LOCAL ROLE parceiros_app` e define as
-- variáveis de sessão abaixo (veja internal/platform/postgres). As políticas
-- de RLS são a segunda barreira contra vazamento entre workspaces; a primeira
-- é o filtro por workspace em toda query.
--
--   app.usuario_id    usuário autenticado
--   app.workspace_id  workspace da requisição (só depois de checar o membro)
--   app.zitadel_sub   sub do token, usado no primeiro acesso
--   app.convite_hash  hash do token de convite, usado para ver e aceitar convites

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

CREATE FUNCTION app_usuario_id() RETURNS uuid
    LANGUAGE sql STABLE
    AS $$ SELECT nullif(current_setting('app.usuario_id', true), '')::uuid $$;

CREATE FUNCTION app_workspace_id() RETURNS uuid
    LANGUAGE sql STABLE
    AS $$ SELECT nullif(current_setting('app.workspace_id', true), '')::uuid $$;

CREATE FUNCTION app_zitadel_sub() RETURNS text
    LANGUAGE sql STABLE
    AS $$ SELECT nullif(current_setting('app.zitadel_sub', true), '') $$;

CREATE FUNCTION app_convite_hash() RETURNS bytea
    LANGUAGE sql STABLE
    AS $$ SELECT decode(nullif(current_setting('app.convite_hash', true), ''), 'hex') $$;

CREATE TYPE workspace_tipo AS ENUM ('pessoal', 'mentoria');
CREATE TYPE membro_papel AS ENUM ('dono', 'mentor', 'afiliado');

CREATE TABLE usuarios (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    zitadel_sub      text NOT NULL UNIQUE,
    nome             text NOT NULL,
    email            text NOT NULL,
    email_verificado boolean NOT NULL DEFAULT false,
    criado_em        timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE workspaces (
    id        uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tipo      workspace_tipo NOT NULL,
    nome      text NOT NULL CHECK (length(nome) BETWEEN 1 AND 80),
    foto_url  text,
    dono_id   uuid NOT NULL REFERENCES usuarios (id),
    plano     text NOT NULL,
    status    text NOT NULL DEFAULT 'ativo',
    criado_em timestamptz NOT NULL DEFAULT now()
);

-- Cada usuário tem um único workspace pessoal.
CREATE UNIQUE INDEX workspaces_pessoal_unico ON workspaces (dono_id) WHERE tipo = 'pessoal';

CREATE TABLE membros (
    workspace_id        uuid NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    usuario_id          uuid NOT NULL REFERENCES usuarios (id),
    papel               membro_papel NOT NULL,
    consente_resultados boolean NOT NULL DEFAULT false,
    entrou_em           timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, usuario_id)
);

CREATE INDEX membros_usuario ON membros (usuario_id);

-- O token do convite não é guardado: só o seu SHA-256.
CREATE TABLE convites (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    email        text,
    token_hash   bytea NOT NULL UNIQUE,
    expira_em    timestamptz NOT NULL,
    usado_por    uuid REFERENCES usuarios (id),
    usado_em     timestamptz,
    revogado_em  timestamptz,
    criado_por   uuid NOT NULL REFERENCES usuarios (id),
    criado_em    timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX convites_workspace ON convites (workspace_id);

-- Limites por plano (assentos, cota de vídeo, listas). Mudar um valor aqui não
-- exige deploy. Os valores iniciais são provisórios até o M7 (assinatura).
CREATE TABLE limites (
    plano text NOT NULL,
    chave text NOT NULL,
    valor bigint NOT NULL,
    PRIMARY KEY (plano, chave)
);

INSERT INTO limites (plano, chave, valor) VALUES
    ('avulso', 'assentos', 0),
    ('mentoria', 'assentos', 30);

GRANT SELECT, INSERT, UPDATE ON usuarios TO parceiros_app;
GRANT SELECT, INSERT, UPDATE ON workspaces TO parceiros_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON membros TO parceiros_app;
GRANT SELECT, INSERT, UPDATE ON convites TO parceiros_app;
GRANT SELECT ON limites TO parceiros_app;

ALTER TABLE usuarios ENABLE ROW LEVEL SECURITY;
ALTER TABLE usuarios FORCE ROW LEVEL SECURITY;
ALTER TABLE workspaces ENABLE ROW LEVEL SECURITY;
ALTER TABLE workspaces FORCE ROW LEVEL SECURITY;
ALTER TABLE membros ENABLE ROW LEVEL SECURITY;
ALTER TABLE membros FORCE ROW LEVEL SECURITY;
ALTER TABLE convites ENABLE ROW LEVEL SECURITY;
ALTER TABLE convites FORCE ROW LEVEL SECURITY;

-- membros: os do workspace atual e as participações do próprio usuário (para o
-- seletor de workspaces e para sair de um workspace).
CREATE POLICY membros_select ON membros FOR SELECT
    USING (workspace_id = app_workspace_id() OR usuario_id = app_usuario_id());
CREATE POLICY membros_insert ON membros FOR INSERT
    WITH CHECK (workspace_id = app_workspace_id());
CREATE POLICY membros_update ON membros FOR UPDATE
    USING (workspace_id = app_workspace_id());
CREATE POLICY membros_delete ON membros FOR DELETE
    USING (workspace_id = app_workspace_id());

-- convites: os do workspace atual, ou o convite cujo token o usuário tem em mãos.
CREATE POLICY convites_select ON convites FOR SELECT
    USING (workspace_id = app_workspace_id() OR token_hash = app_convite_hash());
CREATE POLICY convites_insert ON convites FOR INSERT
    WITH CHECK (workspace_id = app_workspace_id());
CREATE POLICY convites_update ON convites FOR UPDATE
    USING (workspace_id = app_workspace_id());

-- workspaces: o atual, os que o usuário criou ou participa, e o de um convite
-- que ele tem em mãos (para mostrar o nome antes de aceitar).
CREATE POLICY workspaces_select ON workspaces FOR SELECT
    USING (
        id = app_workspace_id()
        OR dono_id = app_usuario_id()
        OR id IN (SELECT workspace_id FROM membros WHERE usuario_id = app_usuario_id())
        OR id IN (SELECT workspace_id FROM convites WHERE token_hash = app_convite_hash())
    );
CREATE POLICY workspaces_insert ON workspaces FOR INSERT
    WITH CHECK (dono_id = app_usuario_id());
CREATE POLICY workspaces_update ON workspaces FOR UPDATE
    USING (id = app_workspace_id());

-- usuarios: o próprio usuário e os membros do workspace atual.
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

-- +goose Down
-- Estas políticas consultam outras tabelas e precisam sair antes delas.
DROP POLICY workspaces_select ON workspaces;
DROP POLICY usuarios_select ON usuarios;
DROP TABLE limites;
DROP TABLE convites;
DROP TABLE membros;
DROP TABLE workspaces;
DROP TABLE usuarios;
DROP TYPE membro_papel;
DROP TYPE workspace_tipo;
DROP FUNCTION app_convite_hash();
DROP FUNCTION app_zitadel_sub();
DROP FUNCTION app_workspace_id();
DROP FUNCTION app_usuario_id();
REVOKE USAGE ON SCHEMA public FROM parceiros_app;
