# Arquitetura do backend

O backend segue a arquitetura limpa: o domínio e os casos de uso não conhecem banco, HTTP nem fornecedores; dependem só de interfaces. A infraestrutura implementa essas interfaces, e o pacote `server` liga tudo. Trocar o banco, o provedor de identidade ou um gateway é escrever outra implementação da interface e escolhê-la na configuração.

A migração para esta estrutura acontece em fases, um PR por fase (plano completo na pasta do projeto, `arquitetura/plano-arquitetura-limpa.md`). Enquanto ela não termina, os módulos ainda não migrados continuam nas pastas antigas (`internal/contas`, `internal/colecoes`...), já usando a infraestrutura nova.

| Fase | Conteúdo | Situação |
|---|---|---|
| 1 | Fundação: `infrastructure`, `pkg`, `server`, variáveis de ambiente em inglês e autenticação plugável (OIDC, interna, dev) | feita |
| 2 | Contas (usuários, workspaces, membros, convites) | a fazer |
| 3 | Catálogo: fonte Shopee, produtos e tendências | a fazer |
| 4 | Coleções e curadoria | a fazer |
| 5 | Vídeos e notificações | a fazer |
| 6 | Resultados e assinatura; saem as pastas antigas | a fazer |
| 7 | Contrato da API em inglês (caminhos, campos e códigos de erro) | a fazer |
| 8 | Front em inglês (identificadores; os textos da tela seguem em português) | a fazer |

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
│   │   ├── crypto/                # envelope encryption dos segredos de usuário
│   │   ├── database/              # pool, transação com escopo de RLS, dbgen (sqlc), dbtest
│   │   ├── http/                  # roteador base, middlewares, handlers, *_types.go e messages.go (pt-BR)
│   │   ├── mail/                  # SMTP e o layout dos e-mails
│   │   ├── queue/                 # River
│   │   ├── ratelimit/             # token bucket no Redis
│   │   ├── repository/            # postgres_*.go e inmem_*.go (testes)
│   │   └── storage/               # bucket S3
│   ├── observability/             # logger (slog em JSON para o Cloud Logging)
│   └── server/                    # config.go, auth.go, router.go, server.go: monta e roda tudo
└── pkg/
    ├── env/                       # leitura de variáveis de ambiente
    └── httputil/                  # JSON e corpo de erro padrão
```

## Regras de dependência

- `domain` não importa nenhum pacote do projeto.
- `service` importa só `domain`. Recebe repositórios e provedores como interfaces no construtor.
- `infrastructure` implementa as interfaces do `domain` e chama os `service` (os handlers HTTP).
- Só `server` conhece as implementações concretas e decide qual usar pela configuração.
- Cada repositório toca só as suas tabelas. Um caso de uso que precisa de outro módulo recebe a interface do serviço dele.

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
- Tabelas e colunas novas nascem em inglês. As antigas são renomeadas por migrations novas, na fase de cada módulo, sem reescrever as já aplicadas.
- O código do sqlc dos módulos migrados fica num pacote só, `infrastructure/database/dbgen`.

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

Cada usuário pertence a um provedor: `usuarios` guarda o par `(auth_provider, auth_subject)`. Trocar de provedor num ambiente com usuários exige migrar as contas (ainda não há ferramenta para isso).

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

- Serviços: testes de unidade com os repositórios em memória (`repository.NewInMemory*`).
- Repositórios: o mesmo contrato roda contra a implementação em memória e a do Postgres, mais os testes de isolamento por RLS.
- `server`: testes ponta a ponta pela API HTTP.
- `web/e2e`: o roteiro do MVP com `AUTH_PROVIDER=dev` e o login por e-mail e senha com `AUTH_PROVIDER=internal` (e-mails lidos no Mailpit).
