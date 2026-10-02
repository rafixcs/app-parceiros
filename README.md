# App Parceiros

App para afiliados da Shopee descobrirem produtos em alta, organizarem o que vão divulgar (link próprio, notas, título e descrição), verem quanto ganham por venda e acompanharem resultados, com curadoria de um mentor para a sua turma.

- [Especificação do MVP](docs/mvp.md)
- [Stack](docs/stack.md)
- [Pesquisa de mercado e viabilidade](docs/pesquisa.md)

## Rodando localmente

### Dependências

| Ferramenta | Versão | Para quê |
|---|---|---|
| [Docker](https://docs.docker.com/engine/install/) | recente, rodando sem `sudo` | imagens e o cluster kind |
| [Go](https://go.dev/dl/) | 1.26 ou mais nova | backend, testes e `make lint`/`make sqlc` |
| [Node.js](https://nodejs.org/) | 22 | front (`web/`) |
| [kind](https://kind.sigs.k8s.io/) | recente | cluster Kubernetes local |
| [kubectl](https://kubernetes.io/docs/tasks/tools/) | recente | usado pelo Tilt |
| [Tilt](https://tilt.dev/) | recente | sobe e recarrega tudo no cluster |

O `make setup` instala o que faltar (Ubuntu/Debian e macOS com Homebrew; no Windows, use o WSL2), baixa as dependências do backend e do front e cria o cluster. No Linux, ele pede `sudo` para instalar em `/usr/local`. Se instalar o Docker, abra um terminal novo e rode de novo.

```sh
make setup     # uma vez: dependências e cluster kind
tilt up        # sobe tudo; front em http://localhost:5173, API em http://localhost:8080/readyz
```

O front também roda sozinho, apontando para a API em `localhost:8080`:

```sh
cd web
npm ci
npm run dev    # http://localhost:5173 (entre com qualquer nome no modo dev)
npm run api    # regenera src/api/schema.d.ts depois de mudar api/openapi.yaml
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

## Shopee

O radar usa a **credencial do app** (`SHOPEE_APP_ID` e `SHOPEE_APP_SECRET`) para coletar o catálogo. A credencial de cada afiliado é conectada por ele em "Shopee" no app e fica cifrada no banco.

| Variável | Uso |
|---|---|
| `SHOPEE_MODO` | `api` ou `mock`. O padrão é `mock` em `APP_ENV=dev` sem `SHOPEE_APP_ID`; `mock` é recusado fora de dev |
| `SHOPEE_APP_ID`, `SHOPEE_APP_SECRET` | Credencial do app para o catálogo. Sem elas (e fora do mock), o worker não coleta |
| `SHOPEE_RATE_POR_HORA` | Chamadas por hora por credencial (padrão 1800) |
| `SHOPEE_PAGINAS` | Páginas de 50 produtos por categoria em cada coleta (padrão 10) |
| `CRYPTO_KEK`, `CRYPTO_KEK_ID` | Chave mestra (32 bytes em base64) que cifra as credenciais. Gere com `head -c32 /dev/urandom \| base64` |
| `S3_ENDPOINT`, `S3_BUCKET`, `S3_ACCESS_KEY`, `S3_SECRET_KEY` | Bucket onde ficam as respostas brutas da Shopee (R2 em produção, MinIO local) |

Como funciona:
- O job `agendar_snapshots` roda a cada 6 h (e quando o worker sobe) e enfileira um `snapshot_catalogo` geral e um por categoria com `monitorar = true` na tabela `categorias`.
- Cada `snapshot_catalogo` pagina o `productOfferV2` em sequência, grava os produtos e um snapshot por hora, guarda a resposta bruta no bucket e agenda o `calcular_tendencias`.
- O score vai de 0 a 100: crescimento de vendas em 7 dias (escala log) × comissão (0,5× a 1,5×) × nota (0,5× a 1×). Sem um dia de histórico, o produto aparece como "Sem histórico".

**Modo mock.** Sem credencial aprovada, a Shopee é simulada com as respostas gravadas em `backend/internal/fontes/shopee/testdata/`, e as vendas crescem um pouco a cada dia para o radar ter tendência. Qualquer AppID numérico conecta, exceto estes, que simulam erros da Open API: `10020` (credencial recusada), `10030` (limite de chamadas) e `10031` (acesso negado).

Para ligar a Shopee de verdade: defina `SHOPEE_MODO=api`, `SHOPEE_APP_ID` e `SHOPEE_APP_SECRET` e marque as categorias a monitorar:

```sql
INSERT INTO categorias (fonte, id, nome, monitorar) VALUES ('shopee', <id>, '<nome>', true);
```
