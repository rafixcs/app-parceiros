# App Parceiros

App para afiliados da Shopee descobrirem produtos em alta, organizarem o que vão divulgar e acompanharem resultados, com curadoria de um mentor para a sua turma.

- Especificação do MVP: `docs/mvp.md`. **Leia antes de começar qualquer funcionalidade.**
- Stack aprovada e justificativas: `docs/stack.md`
- Pesquisa de mercado e viabilidade: `docs/pesquisa.md`
- Lançamento (pendências, infraestrutura e passo a passo do deploy): `docs/lancamento.md`
- Arquitetura do backend (camadas, regras de dependência, autenticação plugável e fases da migração): `docs/arquitetura.md`

## Idioma
- Documentação, mensagens de commit, descrições de PR e tudo o que o cliente vê (textos da interface, mensagens de erro da API, e-mails, notificações) em **português do Brasil**.
- Código, comentários, logs, tabelas e colunas novas, variáveis de ambiente, jobs e filas em **inglês**. O domínio devolve erros com código (`domain.NewError`), e a camada HTTP traduz para português em `infrastructure/http/messages.go`.
- Os módulos antigos (`internal/contas`, `internal/colecoes`...) ainda têm nomes em português; eles passam para o inglês na fase de cada um (`docs/arquitetura.md`). Código novo já nasce em inglês.
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
backend/internal/infrastructure/  # auth, crypto, database, http, mail, queue, ratelimit,
                                  # repository, storage
backend/internal/observability/
backend/internal/server/      # config, escolha das implementações, roteador e modos
backend/internal/<módulo>/    # módulos ainda não migrados: contas, fontes/shopee, produtos,
                              # tendencias, colecoes, curadoria, midia, resultados,
                              # assinaturas, notificacoes
backend/pkg/{env,httputil}/
backend/db/{migrations,queries}/
web/
deploy/{base,components/gcp,overlays/*}/
deploy/terraform/gcp/          # infraestrutura de produção (GKE, Cloud SQL, Memorystore)
deploy/argocd/
Tiltfile
```
Os módulos não acessam as tabelas uns dos outros. Quando precisam, um módulo chama a interface pública do outro. Serviços recebem repositórios e provedores como interfaces; só `server` conhece as implementações concretas.

## Regras inegociáveis
1. **Só APIs oficiais.** Nada de scraping e nada de baixar vídeos de terceiros. Vídeos de outros criadores entram apenas por oEmbed (metadados).
2. **Multi-tenant:** toda tabela com dados de cliente tem `workspace_id` e política de RLS. Toda query filtra por workspace. Teste de vazamento entre workspaces é obrigatório em cada módulo novo.
3. **Segredos:** a credencial Shopee de cada usuário é cifrada (envelope encryption) e nunca vai para log, erro ou resposta da API.
4. **Dinheiro** em centavos (`bigint`). **Comissão** em basis points (`comissao_bp`).
5. **Shopee:** paginação do `productOfferV2` sequencial dentro do job (o `scrollId` expira em ~30 s), no máximo 50 itens por página e janela de relatório de no máximo 90 dias. Respeite o rate limit por credencial.
6. **LGPD:** o mentor só vê resultados de afiliados que consentiram.

## Acesso a dados e multi-tenant
- Toda leitura e escrita de dados de cliente passa por `database.InTx` (`backend/internal/infrastructure/database`), que assume o papel `parceiros_app` e define o `database.Scope` (`app.user_id`, `app.workspace_id`...). Esse papel não ignora RLS, mesmo com conexão de superusuário.
- Tabela nova com `workspace_id`: `GRANT` para `parceiros_app`, `ENABLE` e `FORCE ROW LEVEL SECURITY`, e políticas usando `app_workspace_id()` (veja `00002_contas.sql`).
- Rotas de um workspace ficam sob `/v1/workspaces/{workspaceId}/...` com o middleware `contas.ExigirMembro`; o handler lê o papel com `contas.MembroDoContexto`.
- Queries ficam em `backend/db/queries/<módulo>.sql`; rode `make sqlc` depois de mudar uma query ou migration.
- Autenticação plugável atrás de `domain.Authenticator`, escolhida por `AUTH_PROVIDER` (`server/auth.go`): `oidc` (Zitadel, padrão), `internal` (e-mail e senha, `service.InternalAuth`, rotas `/v1/auth/*`) ou `dev` (`Authorization: Bearer dev:<sub>`, só com `APP_ENV=dev`). O front usa `VITE_AUTH_PROVIDER`. Usuários, workspaces, papéis e convites ficam no Postgres (módulo `contas`), identificados por `(auth_provider, auth_subject)`.

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
- Testes de integração usam Postgres real (testcontainers, via `dbtest.New`), não mocks de banco. Com `TEST_DATABASE_URL`, usam um servidor já existente.
- O cliente Shopee tem uma interface e um mock com respostas gravadas em `testdata/`, para desenvolver sem credencial (`SHOPEE_MODE=mock`, padrão em dev).
- Outros módulos registram rotas pelo `contas.Modulo` (`Autenticadas` e `DoWorkspace`), passado para `contas.Handler.Rotas`.
- Catálogo global (`produtos`, `produto_snapshots`, `tendencias`) não tem `workspace_id`: a API só lê, e o worker escreve com o papel dono das tabelas. A exceção é `produtos.Service.Importar`, que grava o produto colado por link na coleção.
- Coleções (`itens_colecao`, `colecoes`, `colecao_itens`, `links_canal`) são do usuário dentro do workspace: as políticas exigem `app_workspace_id()` e `app_user_id()`. Tabelas filhas repetem `workspace_id` e `usuario_id` com chave estrangeira composta para o pai.
- Entre módulos: `colecoes` lê produtos por `produtos.Service` e gera links por `fontes.Afiliador` (implementado em `shopee.Afiliador`).
- Curadoria (`listas_curadoria`, `lista_itens`, `importacoes`): rascunhos só para dono e mentor, pela função `app_is_manager()` nas políticas. A importação grava na coleção só por `colecoes.Service.Importar`, e a turma vem de `contas.Service`.
- Vídeos (`videos`, `video_vinculos`, `uso_videos`): referências por oEmbed (YouTube e TikTok) e uploads próprios em multipart direto ao bucket, com URLs assinadas pela API (`S3_PUBLIC_ENDPOINT` é o endereço que o navegador enxerga). O worker gera a prévia com ffmpeg. A curadoria anexa vídeos às listas só por `midia.Service`.
- Notificações: outros módulos chamam `notificacoes.Service.Notificar`, que enfileira um `entregar_notificacao` por destinatário (caixa no app, e-mail por SMTP e Web Push). No cluster local, os e-mails ficam no Mailpit (http://localhost:8025).
- Assinatura: o acesso do workspace é a data `workspaces.acesso_ate` (7 dias de teste no cadastro, estendida por cada pagamento confirmado). O workspace pessoal de quem é afiliado de uma mentoria em dia fica `gratuito` (regra provisória, `docs/mvp.md` §8). `contas.Handler.ExigirAtivo` barra com 402 as rotas de um workspace vencido; as rotas que seguem valendo (ver o workspace, sair, consentimento, assinatura) são registradas em `contas.Modulo.Livres`. Preços e limites ficam em `limites`, por plano. O gateway de cobrança fica atrás de `assinaturas.Gateway` (`Asaas` em produção, `Mock` em dev com `BILLING_MODE=mock`), e só o módulo `assinaturas` fala com ele; o webhook acha a assinatura pelo id externo (`app.external_subscription_id`) e ignora reenvios por `eventos_cobranca`.
- Resultados (`conversoes`, `sincronizacoes`): o job `sync_conversoes` lê o `conversionReport` com a credencial do usuário (últimos 89 dias, diário e sob pedido) e põe cada venda no workspace marcado no subId (`colecoes.MarcaWorkspace`); sem marca, no pessoal. Só a transação sem workspace (a sincronização) grava. Dono e mentor leem pela RLS apenas os membros com `consente_resultados`, e a API só lhes devolve somas (nunca por afiliado). As listas da turma vêm de `curadoria.Service.Importacoes`. A Open API não informa cliques.
