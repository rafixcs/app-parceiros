# App Parceiros

App para afiliados da Shopee descobrirem produtos em alta, organizarem o que vão divulgar e acompanharem resultados, com curadoria de um mentor para a sua turma.

- Especificação do MVP: `docs/mvp.md`. **Leia antes de começar qualquer funcionalidade.**
- Stack aprovada e justificativas: `docs/stack.md`
- Pesquisa de mercado e viabilidade: `docs/pesquisa.md`
- Lançamento (pendências, infraestrutura e passo a passo do deploy): `docs/lancamento.md`
- Arquitetura do backend (camadas, regras de dependência, autenticação plugável e como recriar o banco local): `docs/arquitetura.md`

## Idioma
- Documentação, mensagens de commit, descrições de PR e tudo o que o cliente vê (textos da interface, mensagens de erro da API, e-mails, notificações) em **português do Brasil**.
- Código, comentários, logs, tabelas, colunas e valores de enum, variáveis de ambiente, jobs, filas e o contrato da API (caminhos, campos e códigos de erro) em **inglês**. O domínio devolve erros com código (`domain.NewError`), e a camada HTTP traduz para português em `infrastructure/http/messages.go` e `messages_<módulo>.go`. A API responde erro como `{code, message}`.
- As rotas do navegador no front continuam em português (`/entrar`, `/colecao`, `/w/<id>/listas`...), porque o usuário as vê.
- Migrations: arquivos `backend/db/migrations/NNNNN_descricao.sql` no formato goose, embutidos no binário.

## Stack
- **Backend:** Go, monólito modular, um binário em `backend/cmd/parceiros` com os modos `api` e `worker`.
  - HTTP: `net/http` + `chi`. Banco: `pgx` + `sqlc`. Migrations: `goose` (`backend/db/migrations`). Logs: `slog`.
  - Jobs: River (fila no Postgres), com as filas `shopee`, `media` e `default`.
  - Redis para cache e rate limit por credencial.
- **Contrato:** `api/openapi.yaml` é a fonte da verdade. Altere o contrato primeiro e depois gere o código.
- **Frontend:** `web/`, React + TypeScript + Vite (PWA), TanStack Query/Router, Tailwind + shadcn/ui, Uppy.
- **Infra:** Docker, Kubernetes, Kustomize (`deploy/`), Tilt para dev local (kind/k3d), SeaweedFS (S3) no lugar do R2. Produção: GCP por Terraform (`deploy/terraform/gcp`), segredos no Secret Manager via External Secrets, deploy pelo Argo CD. Nada de criar recursos pagos ou fazer deploy sem ok do Rafael.

## Estrutura
```
api/openapi.yaml
backend/cmd/parceiros/        # main, modos api|worker|migrate|vapid
backend/internal/domain/      # entidades, erros e interfaces (não importa nada do projeto)
backend/internal/service/     # casos de uso (importam só o domain)
backend/internal/infrastructure/  # auth, billing, crypto, database, ffmpeg, http, mail, oembed,
                                  # push, queue, ratelimit, repository, shopee, storage
backend/internal/observability/
backend/internal/server/      # config, escolha das implementações (app.go), roteador e modos;
                              # testes de integração de cada módulo pela API
backend/pkg/{env,httputil}/
backend/db/{migrations,queries}/
web/
deploy/{base,components/gcp,overlays/*}/
deploy/terraform/gcp/          # infraestrutura de produção (GKE, Cloud SQL, Memorystore)
deploy/argocd/
Tiltfile
```
Cada módulo (`account`, `product`, `trend`, `collection`, `curation`, `media`, `notification`, `result`, `billing`, `shopee_credential`) se espalha pelas camadas com o mesmo nome: `domain/<m>.go`, `service/<m>.go`, `infrastructure/repository/postgres_<m>.go`, `infrastructure/http/<m>.go` + `<m>_types.go` + `messages_<m>.go`, `infrastructure/queue/<m>.go` (jobs do River) e `backend/db/queries/<m>.sql`; o teste de integração fica em `server/` (`collections_test.go`, `radar_test.go`...).

Os módulos não acessam as tabelas uns dos outros. Quando um serviço precisa de outro módulo, declara do lado dele uma interface pequena só com os métodos que usa (ex.: `collectionProducts` em `service/collection.go`) e recebe o outro serviço por ela. Serviços recebem repositórios e provedores como interfaces; só `server` conhece as implementações concretas e liga tudo em `server/app.go` (`newServices`) e `server/server.go`.

## Regras inegociáveis
1. **Só APIs oficiais.** Nada de scraping e nada de baixar vídeos de terceiros. Vídeos de outros criadores entram apenas por oEmbed (metadados).
2. **Multi-tenant:** toda tabela com dados de cliente tem `workspace_id` e política de RLS. Toda query filtra por workspace. Teste de vazamento entre workspaces é obrigatório em cada módulo novo.
3. **Segredos:** a credencial Shopee de cada usuário é cifrada (envelope encryption) e nunca vai para log, erro ou resposta da API.
4. **Dinheiro** em centavos (`bigint`, colunas `*_cents`). **Comissão** em basis points (`commission_bp`).
5. **Shopee:** paginação do `productOfferV2` sequencial dentro do job (o `scrollId` expira em ~30 s), no máximo 50 itens por página e janela de relatório de no máximo 90 dias. Respeite o rate limit por credencial.
6. **LGPD:** o mentor só vê resultados de afiliados que consentiram.

## Acesso a dados e multi-tenant
- Toda leitura e escrita de dados de cliente passa por `database.InTx` (`backend/internal/infrastructure/database`), que assume o papel `parceiros_app` e define o `database.Scope` (`app.user_id`, `app.workspace_id`...). Esse papel não ignora RLS, mesmo com conexão de superusuário.
- Tabela nova com `workspace_id`: `GRANT` para `parceiros_app`, `ENABLE` e `FORCE ROW LEVEL SECURITY`, e políticas usando `app_workspace_id()` (veja `00002_accounts.sql`).
- Rotas de um workspace ficam sob `/v1/workspaces/{workspaceId}/...` com o middleware `AccountHandler.RequireMember`; o handler lê o membro e o papel com `MemberFromContext` (pacote `infrastructure/http`, importado como `httpapi`).
- Queries ficam em `backend/db/queries/<módulo>.sql` e o sqlc gera um pacote só, `infrastructure/database/dbgen`; rode `make sqlc` depois de mudar uma query ou migration.
- As migrations foram reescritas em inglês antes da produção. Um banco local criado com o esquema antigo (tabela `usuarios`) é recusado pela `00001_extensions.sql` e precisa ser recriado (passo a passo em `docs/arquitetura.md`).
- Autenticação plugável atrás de `domain.Authenticator`, escolhida por `AUTH_PROVIDER` (`server/auth.go`): `oidc` (Zitadel, padrão), `internal` (e-mail e senha, `service.InternalAuth`, rotas `/v1/auth/*`) ou `dev` (`Authorization: Bearer dev:<sub>`, só com `APP_ENV=dev`). O front usa `VITE_AUTH_PROVIDER`. Usuários, workspaces, papéis e convites ficam no Postgres (módulo `account`, tabelas `users`, `workspaces`, `members`, `invites`), identificados por `(auth_provider, auth_subject)`.

## Comandos
- `make cluster`: cria o cluster local (kind) uma vez
- `tilt up`: sobe Postgres, Redis, SeaweedFS (S3), migrations, API (porta 8080) e worker
- `make test`: testes do backend
- `make lint`: `go vet` + `golangci-lint`
- `make sqlc`: gera o código Go das queries
- `make migrate`: aplica as migrations no banco de `DATABASE_URL`
- Binário: `parceiros api|worker|migrate` (`backend/cmd/parceiros`)
- Front (`web/`): `npm run dev`, `npm test`, `npm run build`; `npm run api` regenera os tipos a partir do `api/openapi.yaml`
- Testes das telas (`web/e2e/`, Playwright): `npm run e2e` com a API e o worker no ar em `localhost:8080` (`tilt up`, ou os binários com `APP_ENV=dev` e `AUTH_PROVIDER=dev`). Sobe o Vite sozinho. Cada execução usa usuários novos, então roda de novo no mesmo banco.

## Fluxo de trabalho
- Um PR por história ou marco pequeno (veja os marcos em `docs/mvp.md` §7).
- Antes de abrir PR: `make lint test` passando.
- Testes de integração usam Postgres real (testcontainers, via `dbtest.New`), não mocks de banco. Com `TEST_DATABASE_URL`, usam um servidor já existente. Cada módulo é testado pela API em `server/` (`accounts_test.go`, `collections_test.go`, `radar_test.go`...), com o `newTestApp`: o roteador inteiro com o provedor `dev` e falsos só para os serviços externos.
- O cliente Shopee (`infrastructure/shopee`) tem um mock com respostas gravadas em `infrastructure/shopee/testdata/`, para desenvolver sem credencial (`SHOPEE_MODE=mock`, padrão em dev).
- Cada handler de `infrastructure/http` devolve um `Routes` (`Public`, `User`, `Workspace` e `WorkspaceAnyStatus`), e `server/router.go` passa todos para `AccountHandler.Mount`, que aplica a autenticação, `RequireMember` e `RequireActive`.
- Catálogo global (`categories`, `products`, `product_snapshots`, `trends`) não tem `workspace_id`: a API só lê, e o worker escreve com o papel dono das tabelas (jobs `schedule_snapshots`, `snapshot_catalog` e `compute_trends`). A exceção é `ProductService.Import`, que grava o produto colado por link na coleção.
- Coleções (`saved_items`, `collections`, `collection_items`, `channel_links`) são do usuário dentro do workspace: as políticas exigem `app_workspace_id()` e `app_user_id()`. Tabelas filhas repetem `workspace_id` e `user_id` com chave estrangeira composta para o pai. O job `generate_affiliate_link` (fila `shopee`) gera um link por canal.
- Entre módulos: `CollectionService` lê produtos pelo `ProductService` e gera links por `domain.Affiliator` (implementado em `shopee.Affiliator`, com a credencial do usuário vinda do `ShopeeCredentialService`; tabela `shopee_credentials`).
- Curadoria (`curated_lists`, `curated_list_items`, `list_imports`): rascunhos só para dono e mentor, pela função `app_is_manager()` nas políticas. A importação grava na coleção só por `CollectionService.Import`, e a turma vem do `AccountService`. O número de listas é limitado pela chave `lists` de `plan_limits`.
- Vídeos (`videos`, `video_links`, `video_usage`): referências por oEmbed (YouTube e TikTok, `infrastructure/oembed`) e uploads próprios em multipart direto ao bucket, com URLs assinadas pela API (`S3_PUBLIC_ENDPOINT` é o endereço que o navegador enxerga). O worker gera a prévia com ffmpeg (`infrastructure/ffmpeg`) nos jobs `process_video`, `revalidate_embed` e `clean_upload`. A curadoria anexa vídeos às listas só pelo `MediaService`.
- Notificações (`notifications`, `push_subscriptions`, `notification_preferences`): outros módulos chamam `NotificationService.Notify`, que enfileira um `deliver_notification` por destinatário (caixa no app, e-mail por SMTP e Web Push, `infrastructure/push`). No cluster local, os e-mails ficam no Mailpit (http://localhost:8025).
- Assinatura (`subscriptions`, `billing_events`): o acesso do workspace é a data `workspaces.access_until` (7 dias de teste no cadastro, estendida por cada pagamento confirmado). O workspace pessoal de quem é afiliado de uma mentoria em dia fica com acesso `free` (regra provisória, `docs/mvp.md` §8). `AccountHandler.RequireActive` barra com 402 as rotas de um workspace vencido; as rotas que seguem valendo (ver o workspace, sair, consentimento, assinatura) são registradas em `Routes.WorkspaceAnyStatus`. Preços e limites ficam em `plan_limits`, por plano (`solo` e `mentorship`). O gateway de cobrança fica atrás de `domain.PaymentGateway` (`billing.Asaas` em produção, `billing.Mock` em dev com `BILLING_MODE=mock`), e só o `BillingService` fala com ele; o webhook (`POST /v1/webhooks/billing`) acha a assinatura pelo id externo (`app.external_subscription_id`) e ignora reenvios por `billing_events`.
- Resultados (`conversions`, `conversion_syncs`): o job `sync_conversions` lê o `conversionReport` com a credencial do usuário (últimos 89 dias, diário por `schedule_conversion_syncs` e sob pedido em `POST /v1/me/results/sync`) e põe cada venda no workspace marcado no subId (`domain.WorkspaceMark`); sem marca, no pessoal. Só a transação sem workspace (a sincronização) grava. Dono e mentor leem pela RLS apenas os membros com `members.shares_results`, e a API só lhes devolve somas (nunca por afiliado). As listas da turma vêm de `CurationService.Imports`. A Open API não informa cliques.
