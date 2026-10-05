-- Resultados: as conversões que a Shopee atribui aos links de cada afiliado,
-- lidas do conversionReport com a credencial dele (job sync_conversoes).
--
-- A conversão é do usuário, porque a comissão é dele, e fica no workspace
-- marcado no subId do link (veja colecoes.SubIDs). Sem marca reconhecida, vai
-- para o workspace pessoal. Uma linha por item de pedido.
--
-- Quem vê:
--   - o próprio usuário: no workspace da requisição, ou em todos quando a
--     transação não tem workspace (o job de sincronização, que é quem grava);
--   - dono e mentor do workspace: as dos membros que consentiram
--     (membros.consente_resultados). A API só lhes devolve agregados.

-- +goose Up
CREATE TYPE pedido_status AS ENUM ('nao_pago', 'pendente', 'concluido', 'cancelado');

CREATE TABLE conversoes (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    usuario_id        uuid NOT NULL REFERENCES usuarios (id) ON DELETE CASCADE,
    workspace_id      uuid NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    fonte             text NOT NULL,
    conversao_id      bigint NOT NULL,
    pedido_id         text NOT NULL,
    item_id           bigint NOT NULL,
    modelo_id         bigint NOT NULL DEFAULT 0,
    -- produto do catálogo, quando o item está nele
    produto_id        uuid REFERENCES produtos (id),
    item_nome         text NOT NULL,
    loja_nome         text NOT NULL,
    sub_id            text NOT NULL DEFAULT '',
    -- canal do subId; nulo quando o link não veio do app
    canal             canal,
    status            pedido_status NOT NULL,
    quantidade        integer NOT NULL CHECK (quantidade >= 0),
    -- preço × quantidade
    valor_centavos    bigint NOT NULL,
    comissao_centavos bigint NOT NULL,
    ocorrido_em       timestamptz NOT NULL,
    clicado_em        timestamptz,
    sincronizado_em   timestamptz NOT NULL DEFAULT now(),
    UNIQUE (usuario_id, fonte, pedido_id, item_id, modelo_id)
);

CREATE INDEX conversoes_workspace ON conversoes (workspace_id, ocorrido_em);
CREATE INDEX conversoes_usuario ON conversoes (usuario_id, workspace_id, ocorrido_em);

-- Situação da sincronização de cada usuário.
CREATE TABLE sincronizacoes (
    usuario_id   uuid PRIMARY KEY REFERENCES usuarios (id) ON DELETE CASCADE,
    status       text NOT NULL CHECK (status IN ('sincronizando', 'ok', 'erro', 'sem_credencial')),
    pedida_em    timestamptz NOT NULL DEFAULT now(),
    concluida_em timestamptz,
    conversoes   integer NOT NULL DEFAULT 0,
    erro         text
);

GRANT SELECT, INSERT, UPDATE, DELETE ON conversoes, sincronizacoes TO parceiros_app;

ALTER TABLE conversoes ENABLE ROW LEVEL SECURITY;
ALTER TABLE conversoes FORCE ROW LEVEL SECURITY;
ALTER TABLE sincronizacoes ENABLE ROW LEVEL SECURITY;
ALTER TABLE sincronizacoes FORCE ROW LEVEL SECURITY;

CREATE POLICY conversoes_select ON conversoes FOR SELECT
    USING (
        (usuario_id = app_usuario_id()
         AND (app_workspace_id() IS NULL OR workspace_id = app_workspace_id()))
        OR (workspace_id = app_workspace_id()
            AND app_gestor()
            AND EXISTS (SELECT 1 FROM membros m
                        WHERE m.workspace_id = conversoes.workspace_id
                          AND m.usuario_id = conversoes.usuario_id
                          AND m.consente_resultados))
    );
-- Só a sincronização (transação sem workspace) grava.
CREATE POLICY conversoes_insert ON conversoes FOR INSERT
    WITH CHECK (usuario_id = app_usuario_id() AND app_workspace_id() IS NULL);
CREATE POLICY conversoes_update ON conversoes FOR UPDATE
    USING (usuario_id = app_usuario_id() AND app_workspace_id() IS NULL)
    WITH CHECK (usuario_id = app_usuario_id() AND app_workspace_id() IS NULL);
CREATE POLICY conversoes_delete ON conversoes FOR DELETE
    USING (usuario_id = app_usuario_id() AND app_workspace_id() IS NULL);

CREATE POLICY sincronizacoes_dono ON sincronizacoes
    USING (usuario_id = app_usuario_id())
    WITH CHECK (usuario_id = app_usuario_id());

-- +goose Down
DROP POLICY conversoes_select ON conversoes;
DROP TABLE sincronizacoes;
DROP TABLE conversoes;
DROP TYPE pedido_status;
