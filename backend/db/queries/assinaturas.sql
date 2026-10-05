-- Queries do módulo assinaturas. Gere o código com `make sqlc`.

-- name: Assinatura :one
SELECT * FROM assinaturas WHERE workspace_id = $1;

-- name: AssinaturaPorExterno :one
SELECT * FROM assinaturas WHERE provedor = $1 AND externo_id = $2;

-- name: SalvarAssinatura :one
-- Grava a assinatura do workspace. Uma nova só substitui uma cancelada: sem
-- linha devolvida, já existe uma em andamento.
INSERT INTO assinaturas (
    workspace_id, provedor, cliente_externo_id, externo_id, status,
    assentos, valor_centavos, proximo_ciclo, url_pagamento, criado_por
) VALUES ($1, $2, $3, $4, 'aguardando', $5, $6, $7, $8, $9)
ON CONFLICT (workspace_id) DO UPDATE
    SET provedor = EXCLUDED.provedor,
        cliente_externo_id = EXCLUDED.cliente_externo_id,
        externo_id = EXCLUDED.externo_id,
        status = 'aguardando',
        assentos = EXCLUDED.assentos,
        valor_centavos = EXCLUDED.valor_centavos,
        proximo_ciclo = EXCLUDED.proximo_ciclo,
        url_pagamento = EXCLUDED.url_pagamento,
        criado_por = EXCLUDED.criado_por,
        atualizada_em = now(),
        cancelada_em = NULL
    WHERE assinaturas.cancelada_em IS NOT NULL
RETURNING *;

-- name: AtualizarPlano :one
-- Muda assentos e valor de uma assinatura em andamento (mais ou menos
-- afiliados na turma).
UPDATE assinaturas
SET assentos = @assentos,
    valor_centavos = @valor_centavos,
    atualizada_em = now()
WHERE workspace_id = @workspace_id AND cancelada_em IS NULL
RETURNING *;

-- name: AtualizarCobranca :one
-- Situação vinda do gateway. url_pagamento nula mantém a atual (um aviso de
-- pagamento confirmado não traz fatura em aberto).
UPDATE assinaturas
SET status = @status,
    proximo_ciclo = coalesce(sqlc.narg(proximo_ciclo), proximo_ciclo),
    url_pagamento = coalesce(sqlc.narg(url_pagamento), url_pagamento),
    atualizada_em = now()
WHERE workspace_id = @workspace_id
RETURNING *;

-- name: CancelarAssinatura :one
UPDATE assinaturas
SET status = 'cancelada',
    url_pagamento = NULL,
    cancelada_em = now(),
    atualizada_em = now()
WHERE workspace_id = $1 AND cancelada_em IS NULL
RETURNING *;

-- name: RegistrarEvento :execrows
-- Devolve 0 quando o evento já tinha sido processado (reenvio do gateway).
INSERT INTO eventos_cobranca (provedor, evento_id, workspace_id, tipo)
VALUES ($1, $2, $3, $4)
ON CONFLICT (provedor, evento_id) DO NOTHING;
