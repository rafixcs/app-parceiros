# App Parceiros

App para afiliados da Shopee descobrirem produtos em alta, organizarem o que vão divulgar e acompanharem resultados, com curadoria de um mentor para a sua turma.

- Especificação do MVP: `docs/mvp.md`. **Leia antes de começar qualquer funcionalidade.**
- Stack aprovada e justificativas: `docs/stack.md`
- Pesquisa de mercado e viabilidade: `docs/pesquisa.md`

## Idioma
- Documentação, mensagens de commit, descrições de PR e textos da interface em **português do Brasil**.
- Os nomes do domínio no código seguem os da especificação, em português (`workspaces`, `membros`, `colecoes`, `curadoria`). Termos técnicos e de bibliotecas ficam em inglês.
- Migrations: arquivos `backend/db/migrations/NNNNN_descricao.sql` no formato goose, embutidos no binário.

## Stack
- **Backend:** Go, monólito modular, um binário em `backend/cmd/parceiros` com os modos `api` e `worker`.
  - HTTP: `net/http` + `chi`. Banco: `pgx` + `sqlc`. Migrations: `goose` (`backend/db/migrations`). Logs: `slog`.
  - Jobs: River (fila no Postgres), com as filas `shopee`, `midia` e `default`.
  - Redis para cache e rate limit por credencial.
- **Contrato:** `api/openapi.yaml` é a fonte da verdade. Altere o contrato primeiro e depois gere o código.
- **Frontend:** `web/`, React + TypeScript + Vite (PWA), TanStack Query/Router, Tailwind + shadcn/ui, Uppy.
- **Infra:** Docker, Kubernetes, Kustomize (`deploy/`), Tilt para dev local (kind/k3d), SeaweedFS (S3) no lugar do R2.

## Estrutura
```
api/openapi.yaml
backend/cmd/parceiros/        # main, modos api|worker
backend/internal/<módulo>/    # contas, fontes/shopee, produtos, tendencias, colecoes,
                              # curadoria, midia, resultados, assinaturas, notificacoes
backend/internal/platform/    # db, http, auth, jobs, storage, crypto, observabilidade
backend/db/{migrations,queries}/
web/
deploy/{base,overlays/*}/
Tiltfile
```
Os módulos não acessam as tabelas uns dos outros. Quando precisam, um módulo chama a interface pública do outro.

## Regras inegociáveis
1. **Só APIs oficiais.** Nada de scraping e nada de baixar vídeos de terceiros. Vídeos de outros criadores entram apenas por oEmbed (metadados).
2. **Multi-tenant:** toda tabela com dados de cliente tem `workspace_id` e política de RLS. Toda query filtra por workspace. Teste de vazamento entre workspaces é obrigatório em cada módulo novo.
3. **Segredos:** a credencial Shopee de cada usuário é cifrada (envelope encryption) e nunca vai para log, erro ou resposta da API.
4. **Dinheiro** em centavos (`bigint`). **Comissão** em basis points (`comissao_bp`).
5. **Shopee:** paginação do `productOfferV2` sequencial dentro do job (o `scrollId` expira em ~30 s), no máximo 50 itens por página e janela de relatório de no máximo 90 dias. Respeite o rate limit por credencial.
6. **LGPD:** o mentor só vê resultados de afiliados que consentiram.

## Acesso a dados e multi-tenant
- Toda leitura e escrita de dados de cliente passa por `postgres.InTx` (`backend/internal/platform/postgres`), que assume o papel `parceiros_app` e define o `Escopo` (`app.usuario_id`, `app.workspace_id`...). Esse papel não ignora RLS, mesmo com conexão de superusuário.
- Tabela nova com `workspace_id`: `GRANT` para `parceiros_app`, `ENABLE` e `FORCE ROW LEVEL SECURITY`, e políticas usando `app_workspace_id()` (veja `00002_contas.sql`).
- Rotas de um workspace ficam sob `/v1/workspaces/{workspaceId}/...` com o middleware `contas.ExigirMembro`; o handler lê o papel com `contas.MembroDoContexto`.
- Queries ficam em `backend/db/queries/<módulo>.sql`; rode `make sqlc` depois de mudar uma query ou migration.
- Autenticação: Zitadel (OIDC) valida o token; usuários, workspaces, papéis e convites ficam no Postgres (módulo `contas`). No cluster local, `AUTH_MODE=dev` aceita `Authorization: Bearer dev:<sub>`.

## Comandos
- `make cluster`: cria o cluster local (kind) uma vez
- `tilt up`: sobe Postgres, Redis, SeaweedFS (S3), migrations, API (porta 8080) e worker
- `make test`: testes do backend
- `make lint`: `go vet` + `golangci-lint`
- `make sqlc`: gera o código Go das queries
- `make migrate`: aplica as migrations no banco de `DATABASE_URL`
- Binário: `parceiros api|worker|migrate` (`backend/cmd/parceiros`)
- Front (`web/`): `npm run dev`, `npm test`, `npm run build`; `npm run api` regenera os tipos a partir do `api/openapi.yaml`

## Fluxo de trabalho
- Um PR por história ou marco pequeno (veja os marcos em `docs/mvp.md` §7).
- Antes de abrir PR: `make lint test` passando.
- Testes de integração usam Postgres real (testcontainers, via `pgtest.New`), não mocks de banco. Com `TEST_DATABASE_URL`, usam um servidor já existente.
- O cliente Shopee tem uma interface e um mock com respostas gravadas em `testdata/`, para desenvolver sem credencial (`SHOPEE_MODO=mock`, padrão em dev).
- Outros módulos registram rotas pelo `contas.Modulo` (`Autenticadas` e `DoWorkspace`), passado para `contas.Handler.Rotas`.
- Catálogo global (`produtos`, `produto_snapshots`, `tendencias`) não tem `workspace_id`: a API só lê, e o worker escreve com o papel dono das tabelas.
