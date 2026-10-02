-- Notificações: a caixa de entrada de cada usuário em cada workspace, as
-- inscrições de Web Push dos navegadores e a preferência de e-mail.
--
-- A notificação é gravada pelo worker com o escopo do destinatário, então
-- a política exige o workspace E o próprio usuário. `chave` torna a entrega
-- idempotente (ex.: "lista:<id>"), e email_em/push_em evitam reenvio quando
-- o job é repetido.

-- +goose Up
CREATE TABLE notificacoes (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    usuario_id   uuid NOT NULL REFERENCES usuarios (id) ON DELETE CASCADE,
    tipo         text NOT NULL,
    chave        text NOT NULL,
    titulo       text NOT NULL CHECK (length(titulo) <= 200),
    corpo        text NOT NULL DEFAULT '' CHECK (length(corpo) <= 1000),
    url          text NOT NULL DEFAULT '',
    criado_em    timestamptz NOT NULL DEFAULT now(),
    lida_em      timestamptz,
    email_em     timestamptz,
    push_em      timestamptz,
    UNIQUE (workspace_id, usuario_id, chave)
);

CREATE INDEX notificacoes_caixa ON notificacoes (workspace_id, usuario_id, criado_em DESC);

-- Inscrições de Web Push (uma por navegador). São do usuário, valem em
-- qualquer workspace, como a credencial da Shopee.
CREATE TABLE push_inscricoes (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    usuario_id uuid NOT NULL REFERENCES usuarios (id) ON DELETE CASCADE,
    endpoint   text NOT NULL CHECK (length(endpoint) <= 1000),
    p256dh     text NOT NULL CHECK (length(p256dh) <= 200),
    auth       text NOT NULL CHECK (length(auth) <= 100),
    criado_em  timestamptz NOT NULL DEFAULT now(),
    UNIQUE (usuario_id, endpoint)
);

CREATE TABLE preferencias_notificacao (
    usuario_id    uuid PRIMARY KEY REFERENCES usuarios (id) ON DELETE CASCADE,
    email         boolean NOT NULL DEFAULT true,
    atualizado_em timestamptz NOT NULL DEFAULT now()
);

GRANT SELECT, INSERT, UPDATE ON notificacoes TO parceiros_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON push_inscricoes, preferencias_notificacao TO parceiros_app;

ALTER TABLE notificacoes ENABLE ROW LEVEL SECURITY;
ALTER TABLE notificacoes FORCE ROW LEVEL SECURITY;
ALTER TABLE push_inscricoes ENABLE ROW LEVEL SECURITY;
ALTER TABLE push_inscricoes FORCE ROW LEVEL SECURITY;
ALTER TABLE preferencias_notificacao ENABLE ROW LEVEL SECURITY;
ALTER TABLE preferencias_notificacao FORCE ROW LEVEL SECURITY;

CREATE POLICY notificacoes_dono ON notificacoes
    USING (workspace_id = app_workspace_id() AND usuario_id = app_usuario_id())
    WITH CHECK (workspace_id = app_workspace_id() AND usuario_id = app_usuario_id());
CREATE POLICY push_inscricoes_dono ON push_inscricoes
    USING (usuario_id = app_usuario_id())
    WITH CHECK (usuario_id = app_usuario_id());
CREATE POLICY preferencias_notificacao_dono ON preferencias_notificacao
    USING (usuario_id = app_usuario_id())
    WITH CHECK (usuario_id = app_usuario_id());

-- +goose Down
DROP TABLE preferencias_notificacao;
DROP TABLE push_inscricoes;
DROP TABLE notificacoes;
