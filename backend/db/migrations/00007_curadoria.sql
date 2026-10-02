-- Curadoria: listas de produtos que o mentor monta, com um comentário por
-- produto, e publica para a turma. O afiliado importa a lista (toda ou parte)
-- para a própria coleção, e cada importação fica registrada para o painel do
-- mentor.
--
-- Rascunhos só aparecem para dono e mentor; depois de publicada, a lista
-- aparece para todo o workspace. Quem pode gerir a turma é decidido pela
-- função app_gestor(), que lê o papel do usuário em membros.

-- +goose Up
CREATE FUNCTION app_gestor() RETURNS boolean
    LANGUAGE sql STABLE
    AS $$
        SELECT EXISTS (
            SELECT 1 FROM membros
            WHERE workspace_id = app_workspace_id()
              AND usuario_id = app_usuario_id()
              AND papel IN ('dono', 'mentor')
        )
    $$;

CREATE TABLE listas_curadoria (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id  uuid NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    autor_id      uuid NOT NULL REFERENCES usuarios (id),
    titulo        text NOT NULL CHECK (length(titulo) BETWEEN 1 AND 120),
    descricao     text NOT NULL DEFAULT '' CHECK (length(descricao) <= 2000),
    publicada_em  timestamptz,
    criado_em     timestamptz NOT NULL DEFAULT now(),
    atualizado_em timestamptz NOT NULL DEFAULT now(),
    UNIQUE (id, workspace_id)
);

CREATE INDEX listas_curadoria_workspace ON listas_curadoria (workspace_id, criado_em DESC);

CREATE TABLE lista_itens (
    lista_id     uuid NOT NULL,
    workspace_id uuid NOT NULL,
    produto_id   uuid NOT NULL REFERENCES produtos (id),
    comentario   text NOT NULL DEFAULT '' CHECK (length(comentario) <= 1000),
    ordem        integer NOT NULL,
    PRIMARY KEY (lista_id, produto_id),
    FOREIGN KEY (lista_id, workspace_id)
        REFERENCES listas_curadoria (id, workspace_id) ON DELETE CASCADE
);

-- Uma linha por produto importado: dá para saber quem importou cada lista e
-- cada produto (painel do mentor e resultados por lista no M6).
CREATE TABLE importacoes (
    lista_id     uuid NOT NULL,
    workspace_id uuid NOT NULL,
    usuario_id   uuid NOT NULL REFERENCES usuarios (id) ON DELETE CASCADE,
    produto_id   uuid NOT NULL REFERENCES produtos (id),
    importado_em timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (lista_id, usuario_id, produto_id),
    FOREIGN KEY (lista_id, workspace_id)
        REFERENCES listas_curadoria (id, workspace_id) ON DELETE CASCADE
);

CREATE INDEX importacoes_usuario ON importacoes (workspace_id, usuario_id);

INSERT INTO limites (plano, chave, valor) VALUES
    ('avulso', 'listas', 0),
    ('mentoria', 'listas', 200)
ON CONFLICT DO NOTHING;

GRANT SELECT, INSERT, UPDATE, DELETE ON listas_curadoria, lista_itens TO parceiros_app;
GRANT SELECT, INSERT ON importacoes TO parceiros_app;

ALTER TABLE listas_curadoria ENABLE ROW LEVEL SECURITY;
ALTER TABLE listas_curadoria FORCE ROW LEVEL SECURITY;
ALTER TABLE lista_itens ENABLE ROW LEVEL SECURITY;
ALTER TABLE lista_itens FORCE ROW LEVEL SECURITY;
ALTER TABLE importacoes ENABLE ROW LEVEL SECURITY;
ALTER TABLE importacoes FORCE ROW LEVEL SECURITY;

-- listas: o workspace vê as publicadas; dono e mentor veem também os
-- rascunhos e são os únicos que escrevem.
CREATE POLICY listas_select ON listas_curadoria FOR SELECT
    USING (workspace_id = app_workspace_id() AND (publicada_em IS NOT NULL OR app_gestor()));
CREATE POLICY listas_insert ON listas_curadoria FOR INSERT
    WITH CHECK (workspace_id = app_workspace_id() AND autor_id = app_usuario_id() AND app_gestor());
CREATE POLICY listas_update ON listas_curadoria FOR UPDATE
    USING (workspace_id = app_workspace_id() AND app_gestor());
CREATE POLICY listas_delete ON listas_curadoria FOR DELETE
    USING (workspace_id = app_workspace_id() AND app_gestor());

-- itens da lista: visíveis junto com a lista (a subconsulta passa pela
-- política de listas_curadoria).
CREATE POLICY lista_itens_select ON lista_itens FOR SELECT
    USING (workspace_id = app_workspace_id()
           AND EXISTS (SELECT 1 FROM listas_curadoria l WHERE l.id = lista_id));
CREATE POLICY lista_itens_insert ON lista_itens FOR INSERT
    WITH CHECK (workspace_id = app_workspace_id() AND app_gestor());
CREATE POLICY lista_itens_update ON lista_itens FOR UPDATE
    USING (workspace_id = app_workspace_id() AND app_gestor());
CREATE POLICY lista_itens_delete ON lista_itens FOR DELETE
    USING (workspace_id = app_workspace_id() AND app_gestor());

-- importações: o afiliado vê e grava as dele; dono e mentor veem as da turma.
CREATE POLICY importacoes_select ON importacoes FOR SELECT
    USING (workspace_id = app_workspace_id() AND (usuario_id = app_usuario_id() OR app_gestor()));
CREATE POLICY importacoes_insert ON importacoes FOR INSERT
    WITH CHECK (workspace_id = app_workspace_id() AND usuario_id = app_usuario_id()
                AND EXISTS (SELECT 1 FROM listas_curadoria l WHERE l.id = lista_id AND l.publicada_em IS NOT NULL));

-- +goose Down
DROP TABLE importacoes;
DROP TABLE lista_itens;
DROP TABLE listas_curadoria;
DELETE FROM limites WHERE chave = 'listas';
DROP FUNCTION app_gestor();
