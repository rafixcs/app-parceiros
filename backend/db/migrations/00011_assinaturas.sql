-- Assinaturas: período de teste, planos pagos e suspensão por falta de
-- pagamento.
--
-- O acesso de cada workspace vale até workspaces.acesso_ate. O cadastro dá 7
-- dias de teste; cada pagamento confirmado estende o acesso até o fim do ciclo
-- pago, mais uma tolerância. Passada essa data, o workspace fica suspenso sem
-- depender de nenhum job ou webhook: se um aviso do gateway se perder, o
-- workspace se suspende sozinho, e o próximo pagamento o reativa.
--
-- Quem paga é o dono do workspace: o afiliado avulso paga o próprio plano e o
-- mentor paga a mentoria por assento (um assento por afiliado da turma).
--
-- A tabela assinaturas guarda a assinatura no gateway (Asaas por padrão). O
-- webhook não tem usuário nem workspace: ele acha a assinatura pelo id externo
-- (app.assinatura_externa) e depois opera com o escopo do workspace dela.

-- +goose Up
CREATE FUNCTION app_assinatura_externa() RETURNS text
    LANGUAGE sql STABLE
    AS $$ SELECT nullif(current_setting('app.assinatura_externa', true), '') $$;

CREATE FUNCTION app_dono() RETURNS boolean
    LANGUAGE sql STABLE
    AS $$
        SELECT EXISTS (
            SELECT 1 FROM membros
            WHERE workspace_id = app_workspace_id()
              AND usuario_id = app_usuario_id()
              AND papel = 'dono'
        )
    $$;

-- A situação do workspace (teste, ativo ou suspenso) passa a ser calculada a
-- partir de acesso_ate e pago_em, e a coluna status sai. Os workspaces que já
-- existem começam o teste agora (o default de acesso_ate vale para eles).
ALTER TABLE workspaces
    DROP COLUMN status,
    ADD COLUMN acesso_ate timestamptz NOT NULL DEFAULT now() + interval '7 days',
    -- último pagamento confirmado; nulo enquanto o workspace está no teste
    ADD COLUMN pago_em timestamptz,
    -- Assentos contratados na mentoria; nulo enquanto não há pagamento, e
    -- então vale o limite de teste (limites.assentos_teste).
    ADD COLUMN assentos integer CHECK (assentos >= 0);

-- Limites e preços por plano. "assentos" passa a ser o máximo contratável.
-- Preços em centavos por mês: o avulso por workspace, a mentoria por assento.
-- Valores provisórios, ajustáveis sem deploy.
UPDATE limites SET valor = 200 WHERE plano = 'mentoria' AND chave = 'assentos';
INSERT INTO limites (plano, chave, valor) VALUES
    ('avulso', 'assentos_teste', 0),
    ('mentoria', 'assentos_teste', 5),
    ('avulso', 'preco_centavos', 2990),
    ('mentoria', 'preco_assento_centavos', 1490)
ON CONFLICT (plano, chave) DO UPDATE SET valor = EXCLUDED.valor;

CREATE TYPE assinatura_status AS ENUM ('aguardando', 'ativa', 'atrasada', 'cancelada');

-- A assinatura atual de cada workspace. Uma nova só substitui uma cancelada.
CREATE TABLE assinaturas (
    workspace_id       uuid PRIMARY KEY REFERENCES workspaces (id) ON DELETE CASCADE,
    provedor           text NOT NULL,
    cliente_externo_id text NOT NULL,
    externo_id         text NOT NULL,
    status             assinatura_status NOT NULL DEFAULT 'aguardando',
    assentos           integer NOT NULL CHECK (assentos >= 1),
    valor_centavos     bigint NOT NULL CHECK (valor_centavos > 0),
    -- vencimento da cobrança em aberto (ou da próxima)
    proximo_ciclo      date NOT NULL,
    -- fatura em aberto no gateway (PIX, boleto ou cartão)
    url_pagamento      text,
    criado_por         uuid NOT NULL REFERENCES usuarios (id),
    criada_em          timestamptz NOT NULL DEFAULT now(),
    atualizada_em      timestamptz NOT NULL DEFAULT now(),
    cancelada_em       timestamptz,
    UNIQUE (provedor, externo_id)
);

-- Eventos de webhook já processados, para ignorar reenvios.
CREATE TABLE eventos_cobranca (
    provedor     text NOT NULL,
    evento_id    text NOT NULL,
    workspace_id uuid NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    tipo         text NOT NULL,
    recebido_em  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (provedor, evento_id)
);

CREATE INDEX eventos_cobranca_workspace ON eventos_cobranca (workspace_id, recebido_em);

GRANT SELECT, INSERT, UPDATE ON assinaturas TO parceiros_app;
GRANT SELECT, INSERT ON eventos_cobranca TO parceiros_app;

ALTER TABLE assinaturas ENABLE ROW LEVEL SECURITY;
ALTER TABLE assinaturas FORCE ROW LEVEL SECURITY;
ALTER TABLE eventos_cobranca ENABLE ROW LEVEL SECURITY;
ALTER TABLE eventos_cobranca FORCE ROW LEVEL SECURITY;

-- Pela API, só dono e mentor veem a assinatura e só o dono a altera. Sem
-- usuário no escopo (o webhook, já no workspace da assinatura), vale o
-- workspace.
CREATE POLICY assinaturas_select ON assinaturas FOR SELECT
    USING (
        (workspace_id = app_workspace_id() AND (app_usuario_id() IS NULL OR app_gestor()))
        OR externo_id = app_assinatura_externa()
    );
CREATE POLICY assinaturas_insert ON assinaturas FOR INSERT
    WITH CHECK (workspace_id = app_workspace_id() AND (app_usuario_id() IS NULL OR app_dono()));
CREATE POLICY assinaturas_update ON assinaturas FOR UPDATE
    USING (workspace_id = app_workspace_id() AND (app_usuario_id() IS NULL OR app_dono()));

CREATE POLICY eventos_cobranca_workspace ON eventos_cobranca
    USING (workspace_id = app_workspace_id())
    WITH CHECK (workspace_id = app_workspace_id());

-- +goose Down
DROP TABLE eventos_cobranca;
DROP TABLE assinaturas;
DROP TYPE assinatura_status;
DELETE FROM limites WHERE chave IN ('assentos_teste', 'preco_centavos', 'preco_assento_centavos');
UPDATE limites SET valor = 30 WHERE plano = 'mentoria' AND chave = 'assentos';
ALTER TABLE workspaces
    DROP COLUMN assentos,
    DROP COLUMN pago_em,
    DROP COLUMN acesso_ate,
    ADD COLUMN status text NOT NULL DEFAULT 'ativo';
DROP FUNCTION app_dono();
DROP FUNCTION app_assinatura_externa();
