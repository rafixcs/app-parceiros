-- Mídia: a biblioteca de vídeos de cada usuário no workspace.
--
-- Há dois tipos de vídeo:
--   embed   referência de outro criador (YouTube ou TikTok). Só guardamos os
--           metadados do oEmbed oficial (título, autor, miniatura e URL); o
--           vídeo toca no player oficial. Nunca baixamos o arquivo.
--   upload  vídeo do próprio usuário, enviado direto ao bucket (R2) por URL
--           pré-assinada, com a declaração de direito de uso. O worker gera
--           miniatura e prévia em 720p com ffmpeg.
--
-- O vídeo é do dono. Dono e mentor podem compartilhá-lo com o workspace
-- (compartilhado = true); anexar um vídeo a uma lista da curadoria o
-- compartilha. Os vínculos ligam o vídeo a produtos do catálogo e a listas.

-- +goose Up
CREATE TYPE video_tipo AS ENUM ('embed', 'upload');
-- enviando: upload em andamento; processando: na fila do processar_video;
-- pronto: disponível; falhou: o arquivo não pôde ser processado;
-- indisponivel: o embed sumiu da plataforma.
CREATE TYPE video_status AS ENUM ('enviando', 'processando', 'pronto', 'falhou', 'indisponivel');
CREATE TYPE video_alvo AS ENUM ('produto', 'lista');

CREATE TABLE videos (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id   uuid NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    dono_id        uuid NOT NULL REFERENCES usuarios (id) ON DELETE CASCADE,
    tipo           video_tipo NOT NULL,
    plataforma     text NOT NULL CHECK (plataforma IN ('youtube', 'tiktok', 'upload')),
    status         video_status NOT NULL,
    titulo         text NOT NULL DEFAULT '' CHECK (length(titulo) <= 200),
    autor          text NOT NULL DEFAULT '' CHECK (length(autor) <= 200),
    compartilhado  boolean NOT NULL DEFAULT false,
    -- embed
    url            text,
    embed_id       text,
    thumb_url      text,
    verificado_em  timestamptz,
    -- upload
    storage_key    text,
    upload_id      text,
    nome_arquivo   text,
    content_type   text,
    tamanho_bytes  bigint NOT NULL DEFAULT 0 CHECK (tamanho_bytes >= 0),
    duracao_s      integer,
    largura        integer,
    altura         integer,
    direito_uso_em timestamptz,
    criado_em      timestamptz NOT NULL DEFAULT now(),
    atualizado_em  timestamptz NOT NULL DEFAULT now(),
    UNIQUE (id, workspace_id, dono_id),
    CHECK (tipo <> 'embed' OR (url IS NOT NULL AND embed_id IS NOT NULL)),
    CHECK (tipo <> 'upload' OR (storage_key IS NOT NULL AND direito_uso_em IS NOT NULL))
);

CREATE INDEX videos_workspace ON videos (workspace_id, criado_em DESC);
-- O mesmo vídeo de referência colado de novo devolve o que já existe.
CREATE UNIQUE INDEX videos_embed_unico ON videos (workspace_id, dono_id, plataforma, embed_id) WHERE tipo = 'embed';

CREATE TABLE video_vinculos (
    video_id     uuid NOT NULL,
    workspace_id uuid NOT NULL,
    dono_id      uuid NOT NULL,
    alvo_tipo    video_alvo NOT NULL,
    alvo_id      uuid NOT NULL,
    criado_em    timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (video_id, alvo_tipo, alvo_id),
    FOREIGN KEY (video_id, workspace_id, dono_id)
        REFERENCES videos (id, workspace_id, dono_id) ON DELETE CASCADE
);

CREATE INDEX video_vinculos_alvo ON video_vinculos (workspace_id, alvo_tipo, alvo_id);

-- Bytes de upload usados (ou reservados por uploads em andamento) por
-- workspace, para a cota do plano. Atualizado na mesma transação que o vídeo.
CREATE TABLE uso_videos (
    workspace_id uuid PRIMARY KEY REFERENCES workspaces (id) ON DELETE CASCADE,
    bytes        bigint NOT NULL DEFAULT 0 CHECK (bytes >= 0)
);

-- Cota de upload por plano, em bytes. Provisória até o M7 (assinatura).
INSERT INTO limites (plano, chave, valor) VALUES
    ('avulso', 'video_bytes', 5368709120),    -- 5 GB
    ('mentoria', 'video_bytes', 53687091200)  -- 50 GB
ON CONFLICT DO NOTHING;

GRANT SELECT, INSERT, UPDATE, DELETE ON videos, video_vinculos TO parceiros_app;
GRANT SELECT, INSERT, UPDATE ON uso_videos TO parceiros_app;

ALTER TABLE videos ENABLE ROW LEVEL SECURITY;
ALTER TABLE videos FORCE ROW LEVEL SECURITY;
ALTER TABLE video_vinculos ENABLE ROW LEVEL SECURITY;
ALTER TABLE video_vinculos FORCE ROW LEVEL SECURITY;
ALTER TABLE uso_videos ENABLE ROW LEVEL SECURITY;
ALTER TABLE uso_videos FORCE ROW LEVEL SECURITY;

-- videos: o dono vê e altera os seus; o workspace vê os compartilhados. Só
-- dono e mentor compartilham.
CREATE POLICY videos_select ON videos FOR SELECT
    USING (workspace_id = app_workspace_id() AND (dono_id = app_usuario_id() OR compartilhado));
CREATE POLICY videos_insert ON videos FOR INSERT
    WITH CHECK (workspace_id = app_workspace_id() AND dono_id = app_usuario_id() AND NOT compartilhado);
CREATE POLICY videos_update ON videos FOR UPDATE
    USING (workspace_id = app_workspace_id() AND dono_id = app_usuario_id())
    WITH CHECK (workspace_id = app_workspace_id() AND dono_id = app_usuario_id()
                AND (NOT compartilhado OR app_gestor()));
CREATE POLICY videos_delete ON videos FOR DELETE
    USING (workspace_id = app_workspace_id() AND dono_id = app_usuario_id());

-- vínculos: visíveis junto com o vídeo (a subconsulta passa pela política de
-- videos). O dono do vídeo cria e apaga os seus; dono e mentor também tiram
-- vídeos das listas.
CREATE POLICY video_vinculos_select ON video_vinculos FOR SELECT
    USING (workspace_id = app_workspace_id()
           AND EXISTS (SELECT 1 FROM videos v WHERE v.id = video_id));
CREATE POLICY video_vinculos_insert ON video_vinculos FOR INSERT
    WITH CHECK (workspace_id = app_workspace_id() AND dono_id = app_usuario_id()
                AND (alvo_tipo <> 'lista' OR app_gestor()));
CREATE POLICY video_vinculos_delete ON video_vinculos FOR DELETE
    USING (workspace_id = app_workspace_id()
           AND (dono_id = app_usuario_id() OR (alvo_tipo = 'lista' AND app_gestor())));

CREATE POLICY uso_videos_workspace ON uso_videos
    USING (workspace_id = app_workspace_id())
    WITH CHECK (workspace_id = app_workspace_id());

-- +goose Down
DROP TABLE uso_videos;
DROP TABLE video_vinculos;
DROP TABLE videos;
DELETE FROM limites WHERE chave = 'video_bytes';
DROP TYPE video_alvo;
DROP TYPE video_status;
DROP TYPE video_tipo;
