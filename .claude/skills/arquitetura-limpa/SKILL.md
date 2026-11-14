---
name: arquitetura-limpa
description: Regras da arquitetura limpa e do idioma (código em inglês, cliente em pt-BR) do App Parceiros. Use ao criar, alterar ou revisar código do backend Go, migrations, queries, contrato da API ou front.
---

# Arquitetura limpa e código em inglês (App Parceiros)

Regras extraídas do código depois do PR #12. Valem para todo código novo e para toda revisão. Detalhes em `docs/arquitetura.md` e `CLAUDE.md`; em caso de dúvida, copie o padrão do módulo `curation`, que segue tudo isto.

## 1. Idioma

**Inglês** em tudo que é técnico:
- identificadores Go e TypeScript, nomes de arquivos e pastas, pacotes, comentários e logs (`slog`, chaves em `snake_case`: `"item_id"`, `"err"`);
- tabelas, colunas, índices, valores de enum, papéis e funções SQL;
- jobs (`generate_affiliate_link`), filas (`shopee`, `media`, `default`), variáveis de ambiente (`SHOPEE_MODE`, `BILLING_MODE`);
- contrato da API: caminhos (`/v1/workspaces/{workspaceId}/lists`), campos JSON em `snake_case` e códigos de erro (`list_not_found`);
- Terraform, manifests, CI, scripts e títulos de testes.

**Português do Brasil** só no que o cliente vê:
- mensagens de erro da API, em `infrastructure/http/messages_<módulo>.go`;
- e-mails (`infrastructure/mail/*.go`) e textos de notificação montados no serviço (marque a constante ou o trecho com o comentário `// customer text, pt-BR`);
- textos das telas do `web/` e as rotas do navegador (`/entrar`, `/colecao`, `/w/<id>/listas`);
- documentação (`docs/`), mensagens de commit e descrições de PR.

O nome do produto não se traduz: binário `parceiros`, papel `parceiros_app`, módulo `app-parceiros`.

Errado: `func (s *ColecaoService) SalvarItem`, coluna `criado_em`, log `"falha ao gerar link"`, erro `errors.New("lista não encontrada")`.
Certo: `func (s *CollectionService) SaveItem`, coluna `created_at`, log `"generate link failed"`, erro `domain.ErrListNotFound` com a mensagem em `messages_curation.go`.

## 2. Camadas e dependências

```
backend/internal/
  domain/          entidades, regras puras, erros e interfaces
  service/         casos de uso
  infrastructure/  implementações: auth, billing, crypto, database, ffmpeg, http,
                   mail, oembed, push, queue, ratelimit, repository, shopee, storage
  observability/   logger
  server/          configuração e ligação de tudo; testes de integração
backend/pkg/       env, httputil (sem regra de negócio)
backend/cmd/parceiros/main.go   só escolhe o modo e chama o server
```

| Pacote | Pode importar do projeto |
|---|---|
| `domain` | nada |
| `service` | só `domain` (testes podem usar `infrastructure/repository` e `auth`) |
| `infrastructure/*` | `domain`, `pkg`, `infrastructure/database`; os handlers HTTP e os jobs podem usar tipos de `service` |
| `server` | tudo; é o único que conhece as implementações concretas |

Ninguém importa `server`. Se um serviço precisa de algo de fora (banco, Shopee, gateway, fila, e-mail, S3), a interface vai no `domain` e a implementação em `infrastructure`.

## 3. Módulos

Cada módulo tem o mesmo nome em todas as camadas:

| Camada | Arquivo |
|---|---|
| Domínio | `domain/<m>.go`: entidades, limites (`const`), erros, `<M>Repository` e interfaces de provedores |
| Caso de uso | `service/<m>.go`: `<M>Service` e `New<M>Service(...)` |
| Repositório | `infrastructure/repository/postgres_<m>.go`: `Postgres<M>` |
| Queries | `db/queries/<m>.sql` (sqlc gera tudo em `infrastructure/database/dbgen`) |
| HTTP | `infrastructure/http/<m>.go`, `<m>_types.go`, `messages_<m>.go` |
| Jobs | `infrastructure/queue/<m>.go` |
| Testes | `server/<módulo>_test.go` pela API (`collections_test.go`, `curation_test.go`, `radar_test.go`) |

Módulos existentes: `account`, `shopee_credential`, `product`, `trend`, `collection`, `curation`, `media`, `notification`, `result`, `billing` e `auth` (provedor interno).

**Um módulo não toca as tabelas de outro.** Quando um serviço precisa de outro módulo, declara do seu lado uma interface pequena, não exportada, com o nome `<módulo><Outro>` e só os métodos que usa, e um comentário dizendo quem a implementa:

```go
// curationCollections is the collection of each member
// (service.CollectionService): curation only changes it through here.
type curationCollections interface {
	ResolveProduct(ctx context.Context, productID *uuid.UUID, link string) (domain.Product, error)
	Import(ctx context.Context, a domain.Actor, items []domain.ImportedItem, collectionName string) (domain.ImportResult, error)
}
```

## 4. Domínio

- Sem dependência de infraestrutura: nada de `pgx`, `net/http`, `river`, JSON tags ou SQL.
- Erros de negócio têm código estável e nenhum texto:

  ```go
  var ErrListNotFound = NewError(KindNotFound, "list_not_found")
  ```

  O `Kind` (`KindInvalid`, `KindForbidden`, `KindNotFound`, `KindConflict`, `KindPaymentRequired`...) vira o status HTTP em `infrastructure/http/errors.go`. O código entra em `messages_<m>.go` com o texto em pt-BR; `TestEveryDomainErrorHasMessage` falha se faltar.
- Repositórios devolvem `domain.ErrNotFound`, e o serviço traduz para o erro do módulo (`ErrListNotFound`).
- Repositórios recebem `domain.Actor` (usuário + workspace) ou `domain.Member` e não sabem de HTTP. Transações entre repositórios usam `domain.Transactor.WithinTx`.
- Dinheiro em centavos (`int64`, `*_cents`), comissão em basis points (`commission_bp`).

## 5. Serviço (caso de uso)

- Recebe tudo por interface no construtor (`repo domain.<M>Repository`, `tx domain.Transactor`, provedores, `log *slog.Logger`) e não sabe de RLS, SQL, HTTP nem fila concreta.
- Valida entrada e permissões (ex.: `requireCurator(m)`) e devolve erros do `domain`.
- Chama outros módulos só pelas interfaces pequenas da seção 3; notifica por `domain.Notifier`.

## 6. Infraestrutura

**Repositório Postgres**
- `var _ domain.<M>Repository = (*Postgres<M>)(nil)` para garantir a interface.
- Toda chamada passa por `run(ctx, pool, scope, fn)` (que usa `database.Run`/`InTx` com o papel `parceiros_app`) e um escopo: `scopeOf(actor)`, `userScope(id)` ou `workspaceScope(id)`. O repositório decide o escopo; o serviço não.
- Toda query filtra por `workspace_id` além da RLS.
- `pgx.ErrNoRows` vira `domain.ErrNotFound` (`notFound(err)`); unique violation, o erro de conflito do domínio.
- Converta as linhas do `dbgen` em entidades do domínio (`curatedListOf`); tipos do `dbgen` não saem do repositório.

**Migration** (`db/migrations/NNNNN_<descricao>.sql`, goose)
- Tabela com dado de cliente tem `workspace_id`; tabela filha repete `workspace_id` (e `user_id` quando é do usuário) com FK composta para o pai.
- `GRANT` para `parceiros_app`, `ENABLE` e `FORCE ROW LEVEL SECURITY`, políticas com `app_workspace_id()`, `app_user_id()`, `app_is_manager()`.
- Depois: `make sqlc`.

**HTTP**
- O handler declara a interface do serviço que usa (`type CurationService interface {...}`), recebe `log` e devolve um `Routes` com `Public`, `User`, `Workspace` ou `WorkspaceAnyStatus`. Registre-o em `server/router.go`.
- Lê o membro com `currentMember(r)` / `MemberFromContext`, o corpo com `decodeBody(w, r, max, &in)` e responde erro só com `WriteError(w, r, h.log, err)`. Nunca escreva texto de erro no handler.
- Tipos JSON ficam em `<m>_types.go` (`listRequest`, `listResponse`, `listResponseOf(domain.X)`), com tags em `snake_case`.
- O contrato muda primeiro em `api/openapi.yaml`; depois `npm run api` no `web/`.

**Jobs** (`infrastructure/queue/<m>.go`)
- `<Name>Args` com `Kind()` em inglês `snake_case`, `InsertOpts()` com a fila (`QueueShopee`, `QueueMedia`, `QueueDefault`).
- O worker recebe o serviço por interface e monta o `domain.Actor` a partir dos args. A fila é exposta ao serviço por uma interface do domínio (`domain.AffiliateLinkQueue`).

**Provedores externos** (Shopee, Asaas, S3, SMTP, oEmbed, Web Push, ffmpeg): implementam uma interface do `domain` e têm um falso ou mock para dev e testes (`SHOPEE_MODE=mock`, `BILLING_MODE=mock`). Segredos nunca vão para log, erro ou resposta.

## 7. Ligação (`server`)

- `server/app.go` (`newServices`) cria repositórios, gateways e serviços; `server/server.go` monta os modos `api` e `worker`, filas e jobs periódicos.
- A escolha da implementação vem da configuração (`server/config.go`). Autenticação: `AUTH_PROVIDER` em `server/auth.go`, atrás de `domain.Authenticator`:
  - `oidc` (padrão): `infrastructure/auth.OIDC` (Zitadel);
  - `internal`: `service.InternalAuth`, e-mail e senha, rotas `/v1/auth/*`;
  - `dev`: `infrastructure/auth.Dev`, `Bearer dev:<sub>`, só com `APP_ENV=dev`.
  O front usa `VITE_AUTH_PROVIDER`. Usuários são identificados por `(auth_provider, auth_subject)`.
- Uma implementação nova (outro banco, outro gateway) é um arquivo novo em `infrastructure` e uma escolha em `server`; serviços e domínio não mudam.

## 8. Testes

- Cada módulo é testado pela API em `server/<módulo>_test.go` com `newTestApp` (roteador inteiro, Postgres real por `dbtest.New`, provedor `dev`, falsos só para serviços externos).
- Módulo com dado de cliente tem teste de vazamento entre workspaces e entre usuários (veja `TestCurationLeak`).
- Regras puras: `domain/*_test.go`. Adaptadores: testes na própria pasta de `infrastructure`.

## 9. Conferir antes do PR

```sh
# domain não importa nada do projeto (deve sair vazio)
grep -rn 'app-parceiros/backend/' backend/internal/domain --include=*.go
# service importa só domain (deve sair vazio)
grep -n 'app-parceiros/backend/internal/' backend/internal/service/*.go | grep -v _test.go | grep -v '/internal/domain"'
# ninguém importa server (deve sair vazio)
grep -rln 'backend/internal/server"' backend/internal/{domain,service,infrastructure}
# acentos fora de mensagens: cada linha deve ser texto do cliente (e-mail, notificação) ou símbolo
grep -rnP '[À-ÿ]' backend/internal backend/cmd backend/pkg --include=*.go | grep -v _test.go | grep -v /messages
make lint test
```

No front (`web/`): código, nomes de arquivo, hooks e tipos em inglês (`videos-api.ts`, `useVideos`), textos de tela em pt-BR e caminhos de rota em português. Rode `npm test` e `npm run build`.
