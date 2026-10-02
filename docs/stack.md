# App Parceiros: proposta de stack (v3)

Data: 2026-10-02 · Status: **aprovada pelo Rafael em 2026-10-02** (cloud e cobrança da mentoria ainda em aberto)

## O produto (decisões até agora)
- Público: **mentor + seus afiliados** (workspaces), com opção de **afiliado avulso**.
- MVP **só com a Shopee**, usando apenas a **API oficial de afiliados da Shopee BR**. Sem scraping.
- Vídeos: **embed oficial** (referências de outros criadores) + **upload próprio** (vídeos do afiliado ou do mentor). Sem baixar vídeos de terceiros.
- Funções do MVP: radar de produtos em alta, calculadora de ganho, coleções com link próprio/notas/título/descrição, link de afiliado automático, curadoria do mentor, biblioteca de vídeos e dashboard de resultados.

Já decidido antes: **Go, Docker, Postgres, Redis, Kubernetes, Tilt.**
Detalhes de produto e mercado: [pesquisa.md](pesquisa.md).

## O que mudou em relação à v2
- **Saíram:** `yt-dlp`, scraping (`colly`/`rod`), proxies, os modos `collector` e `media` separados, e KEDA.
- **Entraram:** oEmbed para referências, upload direto ao storage por URL pré-assinada, `ffmpeg` só para processar uploads, e o modelo de **workspaces** (mentor, afiliados e avulsos).

## Resumo da recomendação

| Camada | Recomendação | Evolução |
|---|---|---|
| Arquitetura | Monólito modular em Go, um binário com modos `api` e `worker` | Separar filas pesadas em Deployments próprios se precisar |
| Fonte de dados | API de afiliados Shopee BR (GraphQL, assinatura SHA-256) | TikTok Shop atrás da mesma interface `fontes` |
| Filas / jobs | River (fila em Postgres) | — |
| Cache / rate limit | Redis (token bucket por credencial Shopee) | — |
| Banco | Postgres, com `workspace_id` em tudo e Row-Level Security | — |
| Séries temporais | Snapshots de produto em tabela particionada por mês | ClickHouse |
| Busca | Postgres full-text + `pg_trgm` | Meilisearch |
| Vídeos de referência | oEmbed oficial (TikTok, YouTube), guardando só metadados | — |
| Vídeos próprios | Upload direto ao **Cloudflare R2** por URL pré-assinada (multipart), `ffmpeg` no worker | — |
| Contrato da API | REST com OpenAPI 3.1 | — |
| Go | `net/http` + `chi`, `pgx` + `sqlc`, `goose`, `slog` | — |
| Frontend | React + TypeScript + Vite, responsivo + PWA, TanStack, Tailwind + shadcn/ui, Uppy para upload | Expo (app nativo) |
| Identidade | Zitadel (OIDC, login com Google) | — |
| Workspaces, papéis, convites | No nosso Postgres | — |
| Notificações | E-mail (Resend/SES) + Web Push (PWA) | Push nativo com Expo |
| Cobrança | Asaas ou Mercado Pago, assinatura por workspace | Stripe se houver público fora do BR |
| Segredos de usuário | Credenciais Shopee criptografadas (envelope encryption com KMS + Tink) | — |
| Observabilidade | OpenTelemetry + Grafana + Sentry | — |
| CI/CD | GitHub Actions → GHCR → Argo CD + Kustomize | — |
| Infra | OpenTofu, K8s gerenciado, Postgres e Redis gerenciados | — |
| Repositório | Monorepo | — |

## Justificativas

### Arquitetura: `api` + `worker`
Sem scraping nem download de terceiros, não há mais carga imprevisível que justifique coletor e mídia separados. O mesmo binário Go roda como:
- `api`: atende o front, gera URLs de upload, serve dados;
- `worker`: roda os jobs do River em filas separadas (`shopee`, `midia`, `default`), cada uma com seu limite de concorrência.

Se a fila `midia` crescer, basta subir um segundo Deployment do `worker` ouvindo só essa fila. Nenhuma mudança de código.

Módulos de domínio: `contas` (usuários, workspaces, papéis, convites), `fontes/shopee`, `produtos`, `tendencias`, `colecoes`, `curadoria`, `midia`, `resultados`, `assinaturas`, `notificacoes`.

### Shopee
- **Credencial do app** (AppID/Secret nosso): alimenta o catálogo global e o radar.
- **Credencial de cada afiliado**: gera o link dele (`generateShortLink`, com subId por canal) e sincroniza as conversões (`conversionReport`). Pertence ao **usuário**, não ao workspace, porque a comissão é do afiliado.
- Jobs:
  - `snapshot_catalogo` (periódico): percorre `productOfferV2` por categoria e ordenação. A paginação fica sequencial dentro do job, porque o `scrollId` expira em ~30 s e cada página traz no máximo 50 itens;
  - `calcular_tendencias`: score de crescimento (vendas, comissão, nota) e ganho por venda (preço × comissão);
  - `gerar_link`: quando o afiliado salva um produto ou importa uma lista do mentor;
  - `sync_conversoes` (diário por afiliado): janela deslizante dentro do limite de ~90 dias.
- Rate limit por credencial no Redis, para não bloquear a conta de ninguém.
- Cada resposta bruta fica guardada (JSON comprimido no R2) para reprocessar se o parser mudar.

### Vídeos: embed + upload próprio
**Referência por embed:**
1. O usuário cola o link do TikTok ou do YouTube.
2. A API chama o oEmbed oficial e guarda só título, autor, miniatura e URL. O resultado fica em cache no Redis.
3. O front mostra o player oficial (iframe) no card do produto.
4. Um job periódico revalida os links e marca "indisponível" se o vídeo sumir.

Links da Shopee Video não têm oEmbed. Para eles, guardamos o link e a miniatura do produto.

**Upload próprio:**
1. O front (Uppy) pede à API uma URL pré-assinada de upload multipart e envia o arquivo **direto ao R2**, sem passar pelo nosso servidor. Isso funciona bem no celular e com arquivos grandes.
2. Ao terminar, a API valida tipo, tamanho e cota do plano e enfileira o job `processar_video`.
3. O worker (imagem com `ffmpeg`) extrai duração e resolução, gera miniatura e uma prévia leve (720p H.264).
4. O download de volta usa URL assinada de curta duração. O R2 não cobra a saída, o que importa porque o afiliado baixa o vídeo para postar.
5. O usuário marca "tenho direito de uso" no upload, e isso fica registrado.
6. Vídeos do mentor podem ser compartilhados com o workspace.

### Workspaces: mentor + afiliados e avulsos
- **Zitadel cuida só da identidade** (login, senha, Google, MFA). Workspaces, papéis e convites ficam no nosso Postgres. Isso mantém a regra de negócio no nosso código e evita depender do modelo de organizações do provedor.
- Tabelas centrais: `usuarios`, `workspaces` (tipo `mentoria` ou `pessoal`), `membros` (papel `dono`, `mentor` ou `afiliado`), `convites`.
- **Afiliado avulso** = workspace `pessoal` criado no cadastro. **Mentor** cria um workspace `mentoria` e convida a turma por link ou e-mail.
- Um usuário pode estar em vários workspaces, por exemplo no da mentoria e no pessoal.
- Toda tabela de dados de cliente tem `workspace_id`. A API filtra por ele, e o **Row-Level Security do Postgres** é a segunda barreira contra vazamento entre workspaces.
- **Curadoria:** o mentor monta uma lista no workspace. Cada afiliado recebe uma notificação e "importa" a lista. Os produtos vão para a coleção dele, e o job `gerar_link` cria os links com a credencial **do próprio afiliado**.
- **Painel do mentor:** mostra o desempenho agregado da turma só com consentimento registrado de cada afiliado (LGPD).

### Cobrança
A assinatura é **por workspace**:
- plano **Mentoria**: cobrado do mentor por quantidade de afiliados (assentos);
- plano **Avulso**: cobrado do próprio afiliado.

Asaas e Mercado Pago cobrem PIX recorrente, boleto e cartão, com webhooks. Uma tabela de `limites` por plano (assentos, cota de vídeo em GB, listas) é consultada pela API, para que mudar um plano não exija deploy.

### Frontend
React + Vite em PWA, responsivo e pensado primeiro para o celular. Uppy cuida do upload com retomada, e Web Push avisa quando o mentor manda uma lista nova. Se o uso no celular exigir mais (salvar na galeria do iPhone, push mais confiável), a fase 2 é Expo, reaproveitando o cliente TypeScript gerado do OpenAPI.

### Itens iguais às versões anteriores
OpenAPI primeiro; `pgx` + `sqlc` + `goose`; OpenTelemetry + Grafana + Sentry (com painel de saúde da integração Shopee); GitHub Actions + Argo CD + Kustomize; OpenTofu; Postgres e Redis gerenciados em produção; dev local com Tilt em `kind`/`k3d`, usando MinIO no lugar do R2.

## Estrutura do repositório (monorepo)

```
app-parceiros/
├── api/openapi.yaml
├── backend/
│   ├── cmd/parceiros/            # modos: api, worker
│   ├── internal/
│   │   ├── contas/               # usuários, workspaces, papéis, convites
│   │   ├── fontes/shopee/
│   │   ├── produtos/
│   │   ├── tendencias/
│   │   ├── colecoes/
│   │   ├── curadoria/
│   │   ├── midia/                # oEmbed, uploads, ffmpeg
│   │   ├── resultados/
│   │   ├── assinaturas/
│   │   ├── notificacoes/
│   │   └── platform/             # db, http, auth, jobs, storage, crypto, observabilidade
│   ├── db/migrations/
│   ├── db/queries/
│   └── Dockerfile                # inclui ffmpeg
├── web/                          # React + Vite (PWA)
├── deploy/
│   ├── base/
│   └── overlays/{dev,staging,prod}
├── infra/                        # OpenTofu
├── Tiltfile
└── .github/workflows/
```

## Cuidados desde o dia 1
- Apenas APIs oficiais. Nenhum arquivo de vídeo de terceiros é salvo.
- Credenciais Shopee criptografadas e nunca registradas em log.
- `workspace_id` + RLS em todos os dados de cliente.
- LGPD: consentimento para o painel do mentor, retenção curta de dados de clique, política de privacidade.
- Dinheiro em centavos (`bigint`) com moeda; comissão em percentual com histórico.
- Cota de vídeo por plano e limpeza de uploads abandonados.

## Decisões em aberto
1. **Cloud:** GCP, AWS ou DigitalOcean. Sugiro GCP (GKE Autopilot + Cloud SQL), com R2 para vídeos.
2. **Cobrança:** Asaas ou Mercado Pago.
3. **Mentor paga pela turma ou cada aluno paga?** A stack suporta os dois, mas o plano Mentoria precisa de uma regra.

Ação que não depende de código: **pedir agora o acesso à Shopee Affiliate Open API**, porque precisa de aprovação.
