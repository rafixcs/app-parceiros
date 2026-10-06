# Arquitetura do backend

O backend segue a arquitetura limpa: o domínio e os casos de uso não conhecem banco, HTTP nem fornecedores; dependem só de interfaces. A infraestrutura implementa essas interfaces, e o pacote `server` liga tudo. Trocar o banco, o provedor de identidade ou um gateway é escrever outra implementação da interface e escolhê-la na configuração.

A migração para esta estrutura, planejada em oito fases (plano na pasta do projeto, `arquitetura/plano-arquitetura-limpa.md`), entrou inteira num PR só, o #12: as camadas, os módulos com nomes em inglês, o esquema do banco reescrito em inglês, o contrato da API em inglês (caminhos, campos e códigos de erro) e o código do front em inglês. As pastas antigas em português (`internal/contas`, `internal/colecoes`, `internal/fontes/shopee`, `web/src/rotas`...) não existem mais. Seguem em português só os textos que o cliente vê (telas, mensagens de erro da API, e-mails e notificações) e as rotas do navegador (`/entrar`, `/colecao`, `/w/<id>/listas`...).

## Estrutura

```
backend/
├── cmd/parceiros/main.go          # só escolhe o modo (api|worker|migrate|vapid) e chama o server
├── db/migrations/                 # goose, embutidas no binário
├── db/queries/                    # SQL do sqlc
├── internal/
│   ├── domain/                    # entidades, regras puras, erros de negócio e interfaces
│   ├── service/                   # casos de uso; importam só o domain
│   ├── infrastructure/
│   │   ├── auth/                  # provedores de identidade: oidc.go (Zitadel), dev.go, argon2.go
│   │   ├── billing/               # gateways de cobrança: asaas.go e mock.go (domain.PaymentGateway)
│   │   ├── crypto/                # envelope encryption dos segredos de usuário
│   │   ├── database/              # pool, transação com escopo de RLS, dbgen (sqlc), dbtest
│   │   ├── ffmpeg/                # prévia, miniatura e metadados dos vídeos enviados
│   │   ├── http/                  # roteador base, middlewares, handlers, *_types.go e messages*.go (pt-BR)
│   │   ├── mail/                  # SMTP e o layout dos e-mails
│   │   ├── oembed/                # YouTube e TikTok
│   │   ├── push/                  # Web Push (VAPID)
│   │   ├── queue/                 # River: cliente e os jobs de cada módulo
│   │   ├── ratelimit/             # token bucket no Redis
│   │   ├── repository/            # postgres_*.go; inmem_auth.go (testes do provedor interno)
│   │   ├── shopee/                # cliente da Open API, mock e testdata/
│   │   └── storage/               # bucket S3
│   ├── observability/             # logger (slog em JSON para o Cloud Logging)
│   └── server/                    # config.go, auth.go, app.go, router.go, server.go: monta e roda tudo;
│                                  # *_test.go: testes de integração de cada módulo pela API
└── pkg/
    ├── env/                       # leitura de variáveis de ambiente
    └── httputil/                  # JSON e corpo de erro padrão
```

### Módulos

Cada módulo usa o mesmo nome em todas as camadas:

| Camada | Arquivo |
|---|---|
| Domínio | `domain/<m>.go`: entidades, regras puras, erros e interfaces de repositório e provedores |
| Caso de uso | `service/<m>.go`: `<M>Service` |
| Repositório | `infrastructure/repository/postgres_<m>.go`, com as queries de `db/queries/<m>.sql` |
| HTTP | `infrastructure/http/<m>.go` (handler), `<m>_types.go` (JSON) e `messages_<m>.go` (mensagens em pt-BR) |
| Jobs | `infrastructure/queue/<m>.go` |
| Testes | `server/<módulo>_test.go` (`accounts_test.go`, `collections_test.go`, `radar_test.go`...) |

| Módulo | Tabelas | Jobs |
|---|---|---|
| `account` | `users`, `workspaces`, `members`, `invites`, `plan_limits` | |
| `shopee_credential` | `shopee_credentials` | |
| `product` e `trend` (catálogo) | `categories`, `products`, `product_snapshots`, `trends` | `schedule_snapshots`, `snapshot_catalog`, `compute_trends` |
| `collection` | `saved_items`, `collections`, `collection_items`, `channel_links` | `generate_affiliate_link` |
| `curation` | `curated_lists`, `curated_list_items`, `list_imports` | |
| `media` | `videos`, `video_links`, `video_usage` | `process_video`, `revalidate_embed`, `clean_upload` |
| `notification` | `notifications`, `push_subscriptions`, `notification_preferences` | `deliver_notification` |
| `result` | `conversions`, `conversion_syncs` | `schedule_conversion_syncs`, `sync_conversions` |
| `billing` | `subscriptions`, `billing_events` | |
| `auth` (provedor interno) | `auth_accounts`, `auth_sessions`, `auth_tokens` | |

Os jobs rodam nas filas `shopee`, `media` e `default`.

### Rotas

Cada handler devolve um `Routes` com quatro grupos: `Public` (sem login), `User` (usuário autenticado, como `/v1/me/...`), `Workspace` (sob `/v1/workspaces/{workspaceId}`, só com o workspace ativo) e `WorkspaceAnyStatus` (sob o workspace, valendo também com ele suspenso, como a assinatura e o consentimento). `server/router.go` passa todos para `AccountHandler.Mount`, que aplica `RequireIdentity`, `RequireMember` e `RequireActive` (402 para workspace vencido). O handler lê o membro com `MemberFromContext`. A API responde erro como `{code, message}`.

## Regras de dependência

- `domain` não importa nenhum pacote do projeto.
- `service` importa só `domain`. Recebe repositórios e provedores como interfaces no construtor.
- `infrastructure` implementa as interfaces do `domain` e chama os `service` (os handlers HTTP).
- Só `server` conhece as implementações concretas e decide qual usar pela configuração.
- Cada repositório toca só as suas tabelas. Um caso de uso que precisa de outro módulo declara do seu lado uma interface pequena, só com os métodos que usa (ex.: `curationCollections` em `service/curation.go`), e recebe o serviço do outro módulo por ela.
- A ligação fica em `server/app.go` (`newServices`, que monta repositórios, gateways e serviços) e `server/server.go` (modos `api` e `worker`, filas e jobs periódicos).

## Idioma

- Código, comentários, logs, nomes de tabelas e colunas, variáveis de ambiente, jobs e filas ficam em inglês.
- Tudo o que o cliente vê fica em português: mensagens de erro da API, e-mails, notificações e a interface web.
- O domínio devolve erros com código estável (`domain.Error{Kind, Code}`), sem texto. A camada HTTP traduz o código na mensagem em português (`infrastructure/http/messages.go`) e o tipo de erro no status HTTP (`errors.go`). Um teste garante que todo erro declarado com `domain.NewError` tem mensagem.
- O nome do produto continua em português: binário `parceiros`, papel `parceiros_app`, módulo `app-parceiros`.

## Acesso a dados

- Toda leitura e escrita de dados de cliente passa por `database.InTx`, que assume o papel `parceiros_app` (sem bypass de RLS) e define o `database.Scope` nas variáveis de sessão:

  | Campo | Variável | Função SQL |
  |---|---|---|
  | `UserID` | `app.user_id` | `app_user_id()` |
  | `WorkspaceID` | `app.workspace_id` | `app_workspace_id()` |
  | `AuthProvider`, `AuthSubject` | `app.auth_provider`, `app.auth_subject` | `app_auth_provider()`, `app_auth_subject()` |
  | `InviteHash` | `app.invite_hash` | `app_invite_hash()` |
  | `ExternalSubscriptionID` | `app.external_subscription_id` | `app_external_subscription_id()` |
  | `AuthEmail`, `SessionHash`, `AuthTokenHash` | `app.auth_email`, `app.session_hash`, `app.auth_token_hash` | provedor interno |

- O repositório Postgres decide o escopo de cada chamada; o caso de uso não sabe que existe RLS.
- Tabelas, colunas e valores de enum são em inglês. Dinheiro fica em centavos (`*_cents`) e comissão em basis points (`commission_bp`).
- O código do sqlc de todos os módulos fica num pacote só, `infrastructure/database/dbgen`.

### Recriar o banco local

Como ainda não há produção, as migrations foram reescritas em inglês no PR #12, em vez de renomear as tabelas antigas. As versões aplicadas ficam agora na tabela `schema_migrations` (antes, a padrão do goose), e a `00001_extensions.sql` recusa um banco que ainda tem a tabela antiga `usuarios`, com o erro `this database has the old schema`. Um banco local criado antes disso precisa ser recriado (os dados se perdem):

- **No cluster do Tilt**, o mais simples é apagar o namespace `parceiros`, que leva junto o volume do Postgres (o `tilt down` sozinho mantém o volume, e o banco antigo volta no próximo `tilt up`):

  ```sh
  tilt down --delete-namespaces
  tilt up
  ```

  Para manter o resto e recriar só o banco, com o `tilt up` no ar:

  ```sh
  kubectl -n parceiros exec postgres-0 -- psql -U parceiros -d postgres \
    -c 'DROP DATABASE parceiros WITH (FORCE)' -c 'CREATE DATABASE parceiros'
  ```

  e depois rode de novo o `parceiros-migrate` e reinicie `parceiros-api` e `parceiros-worker` pela interface do Tilt.
- Em último caso, `kind delete cluster --name parceiros` e `make cluster`.
- **Fora do cluster**, apague e crie de novo o banco de `DATABASE_URL` (`dropdb` e `createdb`) e rode `make migrate`.

Os testes não são afetados: o `dbtest` cria um banco novo a cada execução.

## Autenticação plugável

O resto da aplicação só conhece a interface `domain.Authenticator`:

```go
type Authenticator interface {
    Authenticate(ctx context.Context, token string) (Identity, error)
    Profile(ctx context.Context, id Identity) (Profile, error)
}
```

`AUTH_PROVIDER` escolhe a implementação (`server/auth.go`):

| Valor | Implementação | Uso |
|---|---|---|
| `oidc` (padrão) | `infrastructure/auth.OIDC` | Zitadel ou outro provedor OIDC externo. Valida o JWT pelo JWKS e busca o perfil no userinfo. |
| `internal` | `service.InternalAuth` | Cadastro e login por e-mail e senha no próprio app. |
| `dev` | `infrastructure/auth.Dev` | Tokens `dev:<nome>`, só com `APP_ENV=dev`. |

O front usa `VITE_AUTH_PROVIDER` com o mesmo valor.

Cada usuário pertence a um provedor: `users` guarda o par `(auth_provider, auth_subject)`. Trocar de provedor num ambiente com usuários exige migrar as contas (ainda não há ferramenta para isso).

### Provedor interno

- Senha com argon2id (19 MiB, 2 passadas, formato PHC, então os parâmetros podem subir sem invalidar as senhas guardadas).
- Sessão com token opaco de 32 bytes; o banco guarda só o SHA-256. Vale 30 dias e é renovada a cada uso (no máximo uma escrita por hora). O logout apaga a sessão.
- Confirmação do e-mail e redefinição da senha por link com token de uso único (7 dias e 1 hora). Redefinir a senha encerra todas as sessões.
- Rate limit no Redis: 10 tentativas por e-mail e 100 por IP a cada 15 minutos.
- Pedir a redefinição responde igual com ou sem conta, para não revelar quem está cadastrado. O login com e-mail desconhecido gasta o mesmo tempo de um com senha errada.
- Rotas: `POST /v1/auth/register`, `/login`, `/logout`, `/email/verify`, `/email/resend`, `/password/forgot`, `/password/reset` (só existem com `AUTH_PROVIDER=internal`).
- Tabelas `auth_accounts`, `auth_sessions` e `auth_tokens`, com RLS pelo e-mail, pelo hash da sessão ou do token e pela conta autenticada.
- O e-mail confirmado chega ao módulo de contas no próximo acesso, e com ele os convites por e-mail e os avisos por e-mail passam a valer.

## Testes

- Módulos: testes de integração pela API HTTP em `server/*_test.go`, um arquivo por módulo. O `newTestApp` monta o roteador inteiro sobre um Postgres real (`dbtest.New`), com o provedor `dev` e falsos só para os serviços externos (fila de notificações, mock da Shopee, gateway de cobrança). Os módulos com dados de cliente testam ali também o isolamento entre workspaces e entre usuários.
- Domínio e infraestrutura: testes de unidade das regras puras (`domain/*_test.go`, como o score do radar) e dos adaptadores (cliente Shopee, Asaas, ffmpeg, oEmbed, Web Push, cifra).
- Provedor interno de autenticação: é o único com repositório em memória (`repository/inmem_auth.go`). O `service/auth_test.go` usa esse repositório, e `repository/auth_contract_test.go` roda o mesmo contrato contra a implementação em memória e a do Postgres; `server/auth_e2e_test.go` cobre as rotas `/v1/auth/*`.
- `web/e2e`: o roteiro do MVP com `AUTH_PROVIDER=dev` e o login por e-mail e senha com `AUTH_PROVIDER=internal` (e-mails lidos no Mailpit).
