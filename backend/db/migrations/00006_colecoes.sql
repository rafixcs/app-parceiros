-- Coleções: os produtos que o afiliado salvou para divulgar, com título,
-- descrição, notas, tags, status e link de afiliado, e as pastas (coleções)
-- em que ele os organiza.
--
-- A coleção é do usuário dentro de cada workspace: as políticas exigem o
-- workspace da requisição E o próprio usuário. O mentor não vê a coleção nem
-- as notas do afiliado. Tabelas filhas repetem workspace_id e usuario_id e os
-- amarram ao pai por chave estrangeira composta, para a política valer em
-- cada linha sem subconsulta.

-- +goose Up
CREATE TYPE item_status AS ENUM ('testando', 'campeao', 'descartado');
CREATE TYPE link_origem AS ENUM ('auto', 'manual');
-- pendente: sem credencial da Shopee; gerando: job gerar_link enfileirado;
-- pronto: link disponível; falhou: a Shopee não gerou depois das tentativas.
CREATE TYPE link_status AS ENUM ('pendente', 'gerando', 'pronto', 'falhou');
CREATE TYPE canal AS ENUM ('instagram', 'tiktok', 'whatsapp', 'outro');

CREATE TABLE itens_colecao (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id  uuid NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    usuario_id    uuid NOT NULL REFERENCES usuarios (id) ON DELETE CASCADE,
    produto_id    uuid NOT NULL REFERENCES produtos (id),
    titulo        text NOT NULL DEFAULT '' CHECK (length(titulo) <= 200),
    descricao     text NOT NULL DEFAULT '' CHECK (length(descricao) <= 2000),
    notas         text NOT NULL DEFAULT '' CHECK (length(notas) <= 5000),
    tags          text[] NOT NULL DEFAULT '{}',
    status        item_status NOT NULL DEFAULT 'testando',
    link_afiliado text,
    link_origem   link_origem NOT NULL DEFAULT 'auto',
    link_status   link_status NOT NULL,
    criado_em     timestamptz NOT NULL DEFAULT now(),
    atualizado_em timestamptz NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, usuario_id, produto_id),
    UNIQUE (id, workspace_id, usuario_id)
);

CREATE INDEX itens_colecao_dono ON itens_colecao (workspace_id, usuario_id, criado_em DESC);
CREATE INDEX itens_colecao_tags ON itens_colecao USING gin (tags);

CREATE TABLE colecoes (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    usuario_id   uuid NOT NULL REFERENCES usuarios (id) ON DELETE CASCADE,
    nome         text NOT NULL CHECK (length(nome) BETWEEN 1 AND 60),
    criado_em    timestamptz NOT NULL DEFAULT now(),
    UNIQUE (id, workspace_id, usuario_id)
);

CREATE UNIQUE INDEX colecoes_nome_unico ON colecoes (workspace_id, usuario_id, lower(nome));

CREATE TABLE colecao_itens (
    colecao_id    uuid NOT NULL,
    item_id       uuid NOT NULL,
    workspace_id  uuid NOT NULL,
    usuario_id    uuid NOT NULL,
    adicionado_em timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (colecao_id, item_id),
    FOREIGN KEY (colecao_id, workspace_id, usuario_id)
        REFERENCES colecoes (id, workspace_id, usuario_id) ON DELETE CASCADE,
    FOREIGN KEY (item_id, workspace_id, usuario_id)
        REFERENCES itens_colecao (id, workspace_id, usuario_id) ON DELETE CASCADE
);

CREATE INDEX colecao_itens_item ON colecao_itens (item_id);

-- Link automático por canal, gerado pelo job gerar_link com a credencial do
-- usuário e um subId por canal.
CREATE TABLE links_canal (
    item_id      uuid NOT NULL,
    workspace_id uuid NOT NULL,
    usuario_id   uuid NOT NULL,
    canal        canal NOT NULL,
    sub_id       text NOT NULL,
    url          text NOT NULL,
    gerado_em    timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (item_id, canal),
    FOREIGN KEY (item_id, workspace_id, usuario_id)
        REFERENCES itens_colecao (id, workspace_id, usuario_id) ON DELETE CASCADE
);

GRANT SELECT, INSERT, UPDATE, DELETE ON itens_colecao, colecoes, colecao_itens, links_canal TO parceiros_app;

ALTER TABLE itens_colecao ENABLE ROW LEVEL SECURITY;
ALTER TABLE itens_colecao FORCE ROW LEVEL SECURITY;
ALTER TABLE colecoes ENABLE ROW LEVEL SECURITY;
ALTER TABLE colecoes FORCE ROW LEVEL SECURITY;
ALTER TABLE colecao_itens ENABLE ROW LEVEL SECURITY;
ALTER TABLE colecao_itens FORCE ROW LEVEL SECURITY;
ALTER TABLE links_canal ENABLE ROW LEVEL SECURITY;
ALTER TABLE links_canal FORCE ROW LEVEL SECURITY;

CREATE POLICY itens_colecao_dono ON itens_colecao
    USING (workspace_id = app_workspace_id() AND usuario_id = app_usuario_id())
    WITH CHECK (workspace_id = app_workspace_id() AND usuario_id = app_usuario_id());
CREATE POLICY colecoes_dono ON colecoes
    USING (workspace_id = app_workspace_id() AND usuario_id = app_usuario_id())
    WITH CHECK (workspace_id = app_workspace_id() AND usuario_id = app_usuario_id());
CREATE POLICY colecao_itens_dono ON colecao_itens
    USING (workspace_id = app_workspace_id() AND usuario_id = app_usuario_id())
    WITH CHECK (workspace_id = app_workspace_id() AND usuario_id = app_usuario_id());
CREATE POLICY links_canal_dono ON links_canal
    USING (workspace_id = app_workspace_id() AND usuario_id = app_usuario_id())
    WITH CHECK (workspace_id = app_workspace_id() AND usuario_id = app_usuario_id());

-- +goose Down
DROP TABLE links_canal;
DROP TABLE colecao_itens;
DROP TABLE colecoes;
DROP TABLE itens_colecao;
DROP TYPE canal;
DROP TYPE link_status;
DROP TYPE link_origem;
DROP TYPE item_status;
