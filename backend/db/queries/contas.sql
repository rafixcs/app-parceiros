-- Queries do módulo contas. Gere o código com `make sqlc`.

-- name: UsuarioPorIdentidade :one
SELECT * FROM usuarios WHERE auth_provider = $1 AND auth_subject = $2;

-- name: UpsertUsuario :one
-- Cria o usuário no primeiro acesso ou atualiza nome e e-mail. `criado` diz
-- se a linha acabou de ser inserida.
INSERT INTO usuarios (auth_provider, auth_subject, nome, email, email_verificado)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (auth_provider, auth_subject) DO UPDATE
    SET nome = EXCLUDED.nome,
        email = EXCLUDED.email,
        email_verificado = EXCLUDED.email_verificado
RETURNING id, (xmax = 0)::boolean AS criado;

-- name: UsuarioPorID :one
SELECT * FROM usuarios WHERE id = $1;

-- name: CriarWorkspace :one
INSERT INTO workspaces (tipo, nome, foto_url, dono_id, plano)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: Workspace :one
SELECT * FROM workspaces WHERE id = $1;

-- name: TravarWorkspace :one
-- Trava a linha do workspace para serializar a contagem de assentos.
SELECT * FROM workspaces WHERE id = $1 FOR UPDATE;

-- name: AtualizarWorkspace :one
UPDATE workspaces
SET nome = coalesce(sqlc.narg(nome), nome),
    foto_url = CASE WHEN sqlc.arg(limpar_foto)::boolean THEN NULL
                    ELSE coalesce(sqlc.narg(foto_url), foto_url) END
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: WorkspacesDoUsuario :many
SELECT w.*, m.papel
FROM membros m
JOIN workspaces w ON w.id = m.workspace_id
WHERE m.usuario_id = $1
ORDER BY w.tipo, w.criado_em;

-- name: InserirMembro :exec
INSERT INTO membros (workspace_id, usuario_id, papel)
VALUES ($1, $2, $3);

-- name: Membro :one
SELECT * FROM membros WHERE workspace_id = $1 AND usuario_id = $2;

-- name: MembrosDoWorkspace :many
SELECT m.usuario_id, u.nome, u.email, m.papel, m.entrou_em, m.consente_resultados
FROM membros m
JOIN usuarios u ON u.id = m.usuario_id
WHERE m.workspace_id = $1
ORDER BY m.papel, m.entrou_em;

-- name: RemoverMembro :execrows
DELETE FROM membros WHERE workspace_id = $1 AND usuario_id = $2;

-- name: ContarAfiliados :one
SELECT count(*) FROM membros WHERE workspace_id = $1 AND papel = 'afiliado';

-- name: ContarConvitesPendentes :one
SELECT count(*) FROM convites
WHERE workspace_id = $1
  AND usado_por IS NULL
  AND revogado_em IS NULL
  AND expira_em > now();

-- name: Limite :one
SELECT valor FROM limites WHERE plano = $1 AND chave = $2;

-- name: CriarConvite :one
INSERT INTO convites (workspace_id, email, token_hash, expira_em, criado_por)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: ConvitesPendentes :many
SELECT * FROM convites
WHERE workspace_id = $1
  AND usado_por IS NULL
  AND revogado_em IS NULL
  AND expira_em > now()
ORDER BY criado_em DESC;

-- name: ConvitePorHash :one
SELECT * FROM convites WHERE token_hash = $1;

-- name: TravarConvitePorHash :one
SELECT * FROM convites WHERE token_hash = $1 FOR UPDATE;

-- name: MarcarConviteUsado :exec
UPDATE convites SET usado_por = $2, usado_em = now() WHERE id = $1;

-- name: RevogarConvite :execrows
UPDATE convites SET revogado_em = now()
WHERE id = $1
  AND workspace_id = $2
  AND usado_por IS NULL
  AND revogado_em IS NULL;

-- name: DefinirConsentimento :execrows
UPDATE membros SET consente_resultados = @consente
WHERE workspace_id = @workspace_id AND usuario_id = @usuario_id;

-- name: LiberarAcesso :one
-- Estende o acesso até `ate` (nunca encurta) e marca o pagamento. Assentos
-- nulos mantêm os contratados.
UPDATE workspaces
SET acesso_ate = greatest(acesso_ate, @ate::timestamptz),
    pago_em = now(),
    assentos = coalesce(sqlc.narg(assentos), assentos)
WHERE id = @id
RETURNING *;

-- name: BloquearAcesso :execrows
UPDATE workspaces SET acesso_ate = least(acesso_ate, now()) WHERE id = $1;

-- name: DefinirAssentos :exec
UPDATE workspaces SET assentos = $2 WHERE id = $1;

-- name: AlunoDeMentoriaAtiva :one
-- Se o usuário é afiliado de alguma mentoria com acesso em dia. Enquanto for,
-- o workspace pessoal dele não é cobrado.
SELECT EXISTS (
    SELECT 1
    FROM membros m
    JOIN workspaces w ON w.id = m.workspace_id
    WHERE m.usuario_id = $1
      AND m.papel = 'afiliado'
      AND w.tipo = 'mentoria'
      AND w.acesso_ate > now()
)::boolean AS aluno;
