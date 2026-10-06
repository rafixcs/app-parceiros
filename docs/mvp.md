# App Parceiros: especificação do MVP

Versão 1 · 02/10/2026 · Fontes: `docs/stack.md` (stack aprovada) e `docs/pesquisa.md` (mercado e viabilidade).

## 1. Objetivo

Ajudar afiliados da Shopee a **descobrir produtos em alta**, **organizar** os que vão divulgar (com link próprio, notas, título e descrição), **ver quanto ganham** por venda e **acompanhar resultados**. Um **mentor** pode fazer a curadoria de produtos para a sua turma de afiliados.

## 2. Fora do escopo do MVP

- TikTok Shop, YouTube, Mercado Livre e Amazon (a interface `fontes` já nasce preparada para eles).
- Baixar vídeos de terceiros e qualquer scraping.
- App nativo: o MVP é uma PWA responsiva.
- Disparo automático para WhatsApp e Telegram.
- IA para gerar textos (fica para a V2).

## 3. Perfis e workspaces

| Conceito | Descrição |
|---|---|
| Usuário | Pessoa com login (Zitadel). Pode participar de vários workspaces. |
| Workspace `pessoal` | Criado automaticamente no cadastro. É onde o **afiliado avulso** trabalha. |
| Workspace `mentoria` | Criado por um mentor, que convida afiliados para ele. |
| Papel `dono` | Quem criou o workspace e paga a assinatura. |
| Papel `mentor` | Faz curadoria e vê o painel da turma. O dono de uma mentoria também é mentor. |
| Papel `afiliado` | Recebe listas, organiza a própria coleção e vê os próprios resultados. |

Regras:
- A credencial da Shopee (AppID/Secret) pertence ao **usuário**, não ao workspace, porque a comissão é dele.
- A coleção do afiliado é dele **dentro de cada workspace**. O mentor **não** vê notas pessoais nem a coleção do afiliado; vê apenas os resultados agregados, se o afiliado consentir.

## 4. Épicos e histórias

O critério de aceite vem abaixo de cada história.

### E1. Conta e workspaces
1. **Cadastro e login** com e-mail/senha ou Google.
   - Ao entrar pela primeira vez, o usuário ganha um workspace `pessoal`.
2. **Criar mentoria:** o usuário cria um workspace `mentoria` com nome e foto.
3. **Convidar afiliados** por link ou e-mail, com expiração e limite de assentos do plano.
   - O convite aceito cria `membro` com papel `afiliado`.
   - Um convite expirado ou usado mostra uma mensagem clara.
4. **Trocar de workspace** por um seletor no topo.
5. **Sair ou remover** um membro. Os dados pessoais do afiliado (coleção, credencial) continuam dele.

### E2. Conexão com a Shopee
1. **Conectar a conta de afiliado:** o usuário informa AppID e Secret, e o app valida com uma chamada de teste.
   - Os segredos são guardados criptografados (envelope KMS + Tink) e **nunca** aparecem em log nem em resposta da API.
   - O status mostra conectado, inválido ou expirado.
2. **Desconectar:** apaga a credencial.

### E3. Radar de produtos em alta
1. **Lista ranqueada** de produtos com foto, nome, preço, % de comissão, **ganho por venda** (preço × comissão), vendas, nota, loja e score de tendência.
2. **Filtros:** categoria, faixa de preço, comissão mínima, nota mínima e busca por texto.
3. **Ordenação:** tendência, comissão, ganho por venda e vendas.
4. **Detalhe do produto:** dados atuais e um gráfico do histórico de preço, comissão e vendas (snapshots).
5. **Frescor:** a tela mostra "atualizado há X".
   - Dados vêm do job `snapshot_catalogo`, que roda com a credencial do app. O score sai do job `calcular_tendencias`.

### E4. Coleções e organização
1. **Salvar produto** do radar ou colando um link da Shopee.
2. **Campos editáveis:** título próprio, descrição, notas, tags, status (`testando`, `campeão` ou `descartado`) e link de afiliado.
3. **Link automático:** ao salvar, o job `gerar_link` cria o link com a credencial **do próprio usuário** e um subId por canal (Instagram, TikTok, WhatsApp e outro). O usuário pode sobrescrever o link manualmente.
   - Sem credencial conectada, o produto é salvo com o link em "pendente" e um aviso.
4. **Coleções** (pastas) com nome. Um produto pode estar em várias.
5. **Copiar rápido:** um botão copia título + descrição + link para colar em redes sociais.

### E5. Curadoria do mentor
1. **Criar lista** (ex.: "Achados da semana") com produtos, comentário por produto e vídeos de referência ou próprios anexados.
2. **Publicar** a lista para todos os afiliados do workspace.
   - Cada afiliado recebe uma notificação (e-mail e Web Push).
3. **Importar lista:** o afiliado importa a lista inteira ou parte dela para a sua coleção, e os links são gerados com a credencial dele.
4. **Painel do mentor:** número de afiliados ativos, quem importou cada lista e, **com consentimento**, cliques, pedidos e comissão agregados por lista e por produto.

### E6. Biblioteca de vídeos
1. **Referência por embed:** o usuário cola um link do TikTok ou do YouTube, e a API resolve pelo oEmbed e guarda só título, autor, miniatura e URL.
   - O player oficial aparece no card. Um job de revalidação marca o vídeo como "indisponível" quando ele some.
2. **Upload próprio:** envio direto ao R2 (multipart pré-assinado, via Uppy), com checkbox obrigatório "tenho direito de uso".
   - O job `processar_video` extrai duração, gera miniatura e prévia em 720p.
   - O upload respeita a cota do plano (GB), contada por workspace. Uploads abandonados são limpos depois de 24 h.
   - Até 1 GB por arquivo, em MP4, MOV ou WebM.
3. **Vincular** vídeos a produtos (o item da coleção mostra os vídeos do seu produto) e a listas da curadoria.
   - Só o dono e o mentor compartilham vídeos com a turma. Anexar um vídeo a uma lista o compartilha.
4. **Baixar** o próprio vídeo, ou um compartilhado pelo mentor, por URL assinada de curta duração.

### E7. Resultados
1. **Sincronização diária** de conversões (`conversionReport`) por usuário conectado.
2. **Dashboard:** cliques, pedidos, comissão estimada e comissão validada por período, produto e canal (subId).
3. **Consentimento:** o afiliado autoriza, ou não, que o mentor veja seus resultados agregados naquele workspace.

### E8. Assinatura
1. Planos avulso (`solo`) e mentoria (`mentorship`, por assento), com limites e preços em tabela (`plan_limits`): assentos, assentos do teste, cota de vídeo, número de listas e preço.
2. Checkout no gateway (Asaas, atrás da interface `domain.PaymentGateway`), com a fatura paga em PIX, boleto ou cartão, e webhook que ativa ou suspende o workspace.
   - O acesso é a data `workspaces.access_until`: cada pagamento confirmado a estende até o fim do ciclo, mais 3 dias de tolerância; passada a data, o workspace fica suspenso sem depender de job nem de webhook.
   - Um estorno suspende na hora. Cancelar mantém o acesso até o fim do período já pago.
   - Suspenso, o workspace só deixa ver a si mesmo, sair dele, mexer no consentimento e cuidar da assinatura; o resto responde 402.
3. Período de teste de 7 dias, com assentos de teste (5 na mentoria).
4. Assentos: um por afiliado da turma (convite pendente já ocupa). Diminuir vale na hora, nunca abaixo dos em uso; aumentar vale no próximo pagamento confirmado.

## 5. Modelo de dados

Tabelas, colunas e valores de enum em inglês (as migrations ficam em `backend/db/migrations`). Resumo das colunas principais:

```
users(id, auth_provider, auth_subject, name, email, email_verified, created_at)
workspaces(id, kind[personal|mentorship], name, photo_url, owner_id, plan, access_until, paid_at, seats, created_at)
members(workspace_id, user_id, role[owner|mentor|affiliate], shares_results, joined_at)
invites(id, workspace_id, email?, token_hash, expires_at, used_by?, used_at?, revoked_at?, created_by)
shopee_credentials(user_id, app_id, encrypted_secret, encrypted_dek, kek_id, status[connected|invalid|expired], verified_at)
auth_accounts, auth_sessions, auth_tokens  -- só o provedor interno de autenticação

categories(source, id, name, monitored)
products(id, source[shopee], item_id, shop_id, shop_name, name, image_url, category_id, categories[], url, min_price_cents, max_price_cents, commission_bp, sales, rating, collected_at)
product_snapshots(product_id, collected_at, min_price_cents, max_price_cents, commission_bp, sales, rating)  -- particionada por mês
trends(product_id, computed_at, score, earnings_per_sale_cents, sales_growth_7d, ...)  -- cópia dos dados do produto para o radar

saved_items(id, workspace_id, user_id, product_id, title, description, notes, tags[], status[testing|winner|discarded], affiliate_link, link_origin[auto|manual], link_status[pending|generating|ready|failed])
collections(id, workspace_id, user_id, name)
collection_items(collection_id, item_id, workspace_id, user_id)
channel_links(item_id, workspace_id, user_id, channel[instagram|tiktok|whatsapp|other], sub_id, url)

curated_lists(id, workspace_id, author_id, title, description, published_at)
curated_list_items(list_id, workspace_id, product_id, comment, position)
list_imports(list_id, workspace_id, user_id, product_id, imported_at)

videos(id, workspace_id, owner_id, kind[embed|upload], platform, status, title, author, shared, url, embed_id, thumbnail_url, storage_key, size_bytes, duration_s, usage_rights_at)
video_links(video_id, workspace_id, owner_id, target_kind[product|list], target_id)
video_usage(workspace_id, bytes)

notifications(id, workspace_id, user_id, kind, key, title, body, url, read_at, emailed_at, pushed_at)
push_subscriptions(id, user_id, endpoint, p256dh, auth)
notification_preferences(user_id, email)

conversions(id, user_id, workspace_id, conversion_id, order_id, item_id, product_id?, sub_id, channel, status[unpaid|pending|completed|cancelled], quantity, amount_cents, commission_cents, occurred_at)
conversion_syncs(user_id, status, requested_at, finished_at, conversions, error)

plan_limits(plan[solo|mentorship], key, value)
subscriptions(workspace_id, provider, external_customer_id, external_id, status, seats, amount_cents, next_due_date, payment_url, created_by, cancelled_at)
billing_events(provider, event_id, workspace_id, kind, received_at)  -- reenvios do webhook
```

Toda tabela com `workspace_id` tem RLS. Valores em dinheiro ficam em centavos (`bigint`, colunas `*_cents`) e comissões em basis points (`commission_bp`, 1% = 100).

## 6. Jobs (River)

| Job | Fila | Gatilho | Observações |
|---|---|---|---|
| `schedule_snapshots` | shopee | a cada 6 h e quando o worker sobe | Enfileira um `snapshot_catalog` geral e um por categoria monitorada. |
| `snapshot_catalog` | shopee | por categoria | Paginação sequencial (`scrollId` dura ~30 s, 50 itens por página). Guarda o JSON bruto no bucket. |
| `compute_trends` | default | depois de cada snapshot | Score = crescimento de vendas em 7 dias, ponderado por comissão e nota. |
| `generate_affiliate_link` | shopee | ao salvar ou importar | Usa a credencial do usuário. Retry com backoff. |
| `schedule_conversion_syncs` | default | diário | Enfileira um `sync_conversions` por usuário conectado. |
| `sync_conversions` | shopee | diário por usuário e sob pedido | Janela ≤ 90 dias (usa 89). |
| `revalidate_embed` | default | 7 dias depois de colar, e a cada 7 dias | Um job por vídeo, com o escopo do dono. O oEmbed ao colar é síncrono, com cache de 24 h no Redis. |
| `process_video` | media | fim do upload | ffmpeg: duração, miniatura e prévia em 720p. |
| `clean_upload` | media | 24 h depois de iniciar o upload | Um job por upload; descarta o que não terminou e devolve a cota. |
| `deliver_notification` | default | a cada aviso, por destinatário | Caixa do app, e-mail e Web Push. |

O rate limit é por credencial (token bucket no Redis).

## 7. Ordem de entrega (marcos)

| Marco | Entrega | Pronto quando |
|---|---|---|
| **M0. Fundação** | Monorepo, binário `api`/`worker`, Postgres, Redis, MinIO, Tilt em kind/k3d, migrations com goose, OpenAPI, CI (lint, testes, build) | `tilt up` sobe tudo e `/healthz` responde |
| **M1. Contas** | Zitadel, usuários, workspaces, papéis, convites, RLS | Mentor convida e afiliado entra |
| **M2. Shopee + Radar** | Cliente GraphQL assinado, credenciais criptografadas, snapshots, tendências, tela do radar | Radar mostra produtos reais |
| **M3. Coleções** | Salvar, editar, coleções, link automático, copiar rápido | Afiliado salva um produto e recebe o link dele |
| **M4. Curadoria** | Listas, publicação, notificações, importação | Lista do mentor chega ao afiliado com o link dele |
| **M5. Vídeos** | oEmbed, upload R2, ffmpeg, vínculos | Vídeo de referência e vídeo próprio aparecem no produto |
| **M6. Resultados** | sync de conversões, dashboard, consentimento, painel do mentor | Números batem com o painel da Shopee |
| **M7. Assinatura** | Planos, limites, checkout, webhooks | Workspace sem pagamento fica suspenso |

**Pré-requisito externo:** acesso aprovado à Shopee Affiliate Open API, necessário a partir do M2. Até lá, usar um mock do cliente Shopee com respostas gravadas.

## 8. Decisões em aberto

1. Cloud de produção: a infraestrutura saiu em GCP (`deploy/terraform/gcp`), a confirmar. Pendências do lançamento em `docs/lancamento.md`.
2. Asaas ou Mercado Pago: o M7 saiu com Asaas, a confirmar. Trocar é escrever outra implementação de `domain.PaymentGateway`.
3. No plano Mentoria, quem paga: o M7 saiu com o mentor pagando por assento (o afiliado avulso paga o próprio plano), a confirmar.
4. Preços: R$ 29,90 por mês no avulso e R$ 14,90 por assento na mentoria, provisórios na tabela `plan_limits`.
5. **Cobrança da mentoria e do aluno (a revisitar).** Decisão provisória do Rafael (05/10/2026): o Asaas é o gateway; nesse primeiro momento o mentor paga a mentoria por assento e o **workspace pessoal do aluno é grátis** enquanto ele for afiliado de uma mentoria em dia (situação `free`). No futuro o workspace do aluno pode passar a ser pago. Falta definir:
   - como cobrar os workspaces dos mentores (modelo e valores);
   - quanto o aluno paga por cadeira, e se é o aluno ou o mentor quem paga por ela.
