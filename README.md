# App Parceiros

App para afiliados da Shopee descobrirem produtos em alta, organizarem o que vão divulgar (link próprio, notas, título e descrição), verem quanto ganham por venda e acompanharem resultados, com curadoria de um mentor para a sua turma.

- [Especificação do MVP](docs/mvp.md)
- [Stack](docs/stack.md)
- [Pesquisa de mercado e viabilidade](docs/pesquisa.md)

## Rodando localmente

Pré-requisitos: Docker, [kind](https://kind.sigs.k8s.io/), [Tilt](https://tilt.dev/) e Go 1.26.

```sh
make cluster   # uma vez
tilt up        # sobe tudo; API em http://localhost:8080/readyz
```

Sem Kubernetes, com Postgres e Redis já rodando:

```sh
export DATABASE_URL=postgres://... REDIS_URL=redis://...
make migrate
./bin/parceiros api      # ou: ./bin/parceiros worker
```

## Autenticação

A identidade vem do Zitadel (OIDC). A API valida o token de acesso (JWT) e, no primeiro acesso, cria o usuário e o seu workspace pessoal.

| Variável | Uso |
|---|---|
| `AUTH_MODE` | `oidc` (padrão) ou `dev` |
| `OIDC_ISSUER` | URL do Zitadel, ex.: `https://auth.exemplo.com.br` |
| `OIDC_AUDIENCE` | ID do projeto ou do app no Zitadel (precisa estar no `aud` do token) |
| `OIDC_JWKS_URL`, `OIDC_USERINFO_URL` | Opcionais; o padrão segue os caminhos do Zitadel |
| `APP_URL` | Endereço do front, usado nos links de convite |

No app do Zitadel, configure o token de acesso como **JWT** e peça os escopos `openid profile email`.

No cluster local não há Zitadel: o overlay `dev` liga `AUTH_MODE=dev`, que aceita `Authorization: Bearer dev:<qualquer-nome>` (recusado fora de `APP_ENV=dev`). Exemplo do fluxo de convite:

```sh
curl -s -X POST localhost:8080/v1/workspaces -H 'Authorization: Bearer dev:mentor' -d '{"nome":"Minha turma"}'
curl -s -X POST localhost:8080/v1/workspaces/<id>/convites -H 'Authorization: Bearer dev:mentor' -d '{}'
curl -s -X POST localhost:8080/v1/convites/<token>/aceitar -H 'Authorization: Bearer dev:afiliada'
```
