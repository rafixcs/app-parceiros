# App Parceiros: pesquisa de mercado e viabilidade

*Pesquisa feita em 01/10/2026. Itens marcados com "(a confirmar)" vieram de fontes secundárias ou de inferência e precisam ser validados no portal oficial antes de virar código.*

## 1. Resumo executivo

- **Dá para fazer quase tudo que você descreveu, com uma exceção importante: baixar vídeos de terceiros.** Isso viola os termos do YouTube e do TikTok, esbarra em direitos autorais (Lei 9.610/98) e pode tirar o app das lojas. A proposta é trocar "baixar" por **salvar com embed oficial + upload do próprio afiliado + materiais liberados pelo vendedor**.
- **A Shopee é o melhor ponto de partida.** Ela tem uma API oficial de afiliados no Brasil que já entrega produtos, % de comissão, vendas, geração de link e relatório de conversões. Cobre "produtos em alta", "ver comissão", "link próprio" e "tracking" de uma vez.
- **O TikTok Shop tem APIs oficiais de afiliado** (buscar produtos com colaboração aberta por categoria/comissão, gerar link e consultar pedidos), mas exige cadastro como desenvolvedor no Partner Center e aprovação. A disponibilidade no Brasil está (a confirmar). Não existe endpoint oficial de "produtos em alta"; as ferramentas que mostram isso (Kalodata, FastMoss) usam coleta própria.
- **O YouTube não é um marketplace.** Ele serve como sinal de tendência (vídeos populares por região via YouTube Data API) e como canal de venda (YouTube Shopping Afiliados no Brasil com Shopee e Mercado Livre desde nov/2025).
- **Diferencial claro que encontrei:** sua frase "indicar ao cliente afiliado quais produtos ele deve olhar" descreve um papel de **curador/mentor/agência** que as ferramentas gringas não atendem. Elas são feitas para o afiliado sozinho ou para o vendedor. Um app em português, com curadoria de um mentor para vários afiliados e link de cada um gerado automaticamente, é um posicionamento forte.

## 2. Viabilidade de cada ideia

| Ideia | Viável? | Como | Risco principal |
|---|---|---|---|
| Ver produtos em alta (Shopee) | **Sim, oficial** | API de afiliados Shopee (GraphQL `productOfferV2`), ordenando por vendas/comissão e guardando histórico para calcular crescimento | Limite de 50 itens por consulta; precisa aprovação de acesso à Open API |
| Ver produtos em alta (TikTok Shop) | **Parcial** | API de afiliado do TikTok Shop busca produtos com colaboração aberta por categoria e comissão. "Em alta" teria que ser calculado por nós a partir de snapshots | Aprovação no Partner Center; Brasil (a confirmar); scraping do site viola termos |
| Ver produtos em alta (YouTube) | **Parcial (indireto)** | YouTube Data API: `videos.list?chart=mostPopular&regionCode=BR` e busca por termo ("achadinhos shopee") como sinal de tendência | Cota padrão de 10 mil unidades/dia; uma busca custa 100 |
| Baixar vídeos | **Não recomendado** | Substituir por: embed oficial (YouTube/TikTok oEmbed), upload do próprio afiliado, imagens/vídeos que a API/vendedor disponibilizam | Violação de termos, direito autoral, remoção da Play Store/App Store |
| Organizar produtos (link próprio, notas, título, descrição) | **Sim** | Funcionalidade nossa, sem dependência externa | Nenhum relevante |
| Ver % de comissão | **Sim** na Shopee e no TikTok Shop | Shopee retorna `commissionRate`, `sellerCommissionRate` e `shopeeCommissionRate`; TikTok retorna a taxa da colaboração aberta | Mercado Livre e Amazon: tabela por categoria, mantida manualmente |
| Tracking dos produtos afiliados | **Sim** | (a) Shopee `conversionReport` (cliques, conversões, janela máx. ~90 dias por consulta); (b) pedidos de afiliado via API do TikTok Shop; (c) nossos próprios links curtos com contagem de cliques; (d) histórico de preço/comissão/vendas por snapshot | Cada afiliado precisa conectar as próprias credenciais; LGPD nos dados de clique |

### 2.1 Shopee (prioridade 1)

- Endpoint Brasil: `https://open-api.affiliate.shopee.com.br/graphql`, autenticação por assinatura SHA-256 (`AppId + Timestamp + Payload + Secret`).
- Operações úteis: `productOfferV2` (produtos com comissão, preço e vendas), `shopOfferV2` (lojas), `generateShortLink` (link de afiliado), `conversionReport` (conversões e cliques).
- Limites observados em SDKs: 50 resultados por página, `scrollId` expira em ~30 s, relatório com janela máxima de ~90 dias.
- **Modelo recomendado:** cada afiliado conecta o próprio AppID/Secret (guardado criptografado). Assim o link gerado e as conversões são dele, e o mentor só faz a curadoria.
- Para "em alta": um job periódico (no worker River já proposto na stack) salva snapshots de vendas/preço/comissão. O ranking de tendência é calculado pelo crescimento de vendas entre snapshots, não só pelo total.

### 2.2 TikTok Shop

- TikTok Shop está no Brasil desde maio/2025 e o programa de afiliados funciona dentro do app (vitrine, vídeos, lives).
- APIs de afiliado (lançadas em 2024): buscar produtos com colaboração aberta (categoria, comissão, palavra-chave), gerar link de promoção e consultar pedidos de afiliado. Exige registro de app no TikTok Shop Partner Center e autorização do criador.
- **A confirmar:** quais APIs de afiliado estão liberadas para a região Brasil e se links de afiliado podem ser usados fora do TikTok no Brasil (nos EUA sim, para criadores elegíveis).
- Creative Center (Top Ads/Top Products) é só web, sem API pública para isso. A Commercial Content API só cobre anúncios da UE e é voltada para pesquisa.

### 2.3 YouTube

- YouTube Shopping Afiliados Brasil (desde 04/11/2025): parceiros Shopee e Mercado Livre; exige YPP e 5 mil inscritos; pagamento via AdSense em 60 a 120 dias.
- Não existe API para listar "produtos em alta no YouTube Shopping". O uso viável é monitorar vídeos e termos em alta como sinal e salvar vídeos de referência por embed.

### 2.4 Mercado Livre e Amazon (fase 2)

- Mercado Livre Afiliados: comissões de até 12% por categoria (moda) e até 6% em celulares/informática. Cookie curto, de 24 a 48 h, segundo fonte secundária (a confirmar). Não encontrei API oficial de afiliados; links são gerados no painel/extensão.
- Amazon: a PA-API 5 foi descontinuada e agora responde 403. A substituta é a **Creators API** (REST). Os requisitos de elegibilidade estão (a confirmar).

### 2.5 Download de vídeos: por que não, e o que fazer no lugar

- YouTube: os Termos proíbem baixar conteúdo sem botão/permissão do serviço. O Google Play remove apps que fazem isso.
- TikTok: os Termos proíbem coleta automatizada e download fora do recurso nativo (que o criador pode desativar e que aplica marca d'água).
- Repostar vídeo de outro criador para vender com link próprio gera denúncia de direito autoral e pode banir a conta do afiliado. **O risco recai sobre o seu cliente, não só sobre o app.**
- **Alternativas que entregam o mesmo valor:**
  1. "Biblioteca de referências": salvar o vídeo com embed oficial, notas e o produto vinculado, para o afiliado se inspirar e gravar o próprio.
  2. Upload dos vídeos do próprio afiliado, com organização por produto.
  3. Mídia oficial do produto (imagens que vêm na API; vídeos que o vendedor libera como material de divulgação).
  4. Gerador de roteiro com IA baseado no vídeo de referência (título, descrição, gancho e hashtags).

## 3. Concorrentes

| Ferramenta | Foco | O que oferece | Preço aprox. |
|---|---|---|---|
| **FastMoss** | TikTok Shop analytics | Produtos, criadores e lives em tempo real, 15+ mercados, ~120 mi produtos indexados, outreach de criadores | US$ 39 a 299/mês |
| **Kalodata** | TikTok Shop analytics | Histórico de até 500 dias, interface mais limpa, ranking de produtos/lojas/vídeos | a partir de ~US$ 19/mês |
| **EchoTik** | TikTok Shop (entrada) | Extensão Chrome, métricas básicas | US$ 9 a 19/mês |
| **Shoplus** | TikTok Shop | Plano gratuito, monitoramento de concorrentes | ~US$ 40/mês |
| **Pipiads / Minea / AdMapix** | Anúncios | Biblioteca de criativos/anúncios, análise de vídeo | variados |
| **Divulga Ninja** (BR) | Afiliados Shopee/ML | Automação de ofertas para WhatsApp/Telegram, conteúdo sobre Shopee Trends e Top Vendas (inferido pelos materiais publicados) | (a confirmar) |
| **Painéis nativos** (Shopee Afiliados, TikTok Creator Center) | Gratuitos | Lista de ofertas, comissão, link e relatório, cada um isolado na sua plataforma | grátis |
| **Linktree / Beacons** | Vitrine | Link na bio com produtos | freemium |

**Lacunas que o App Parceiros pode ocupar:**
1. Multiplataforma em português (Shopee + TikTok Shop + ML) num lugar só. As gringas são focadas em TikTok/EUA/Ásia.
2. **Curadoria mentor → afiliados:** ninguém resolve bem "eu escolho os produtos e meus 50 alunos/clientes recebem, cada um com o próprio link".
3. Organização pessoal (notas, coleções, títulos/descrições prontos), que os painéis nativos não têm.
4. Tracking consolidado do ganho de todas as plataformas.

## 4. Funcionalidades sugeridas

### MVP (Shopee primeiro)
1. **Radar de produtos em alta (Shopee):** ranking por crescimento de vendas, comissão e nota, com filtro por categoria e faixa de preço.
2. **Calculadora de ganho:** preço × comissão = quanto o afiliado ganha por venda, já no card do produto.
3. **Coleções e organização:** link próprio, notas, título, descrição, tags e status (testando, campeão, descartado). (Pedido original.)
4. **Link automático:** gerar o link de afiliado com as credenciais do próprio afiliado, com subId por canal (Instagram, WhatsApp, TikTok).
5. **Curadoria para clientes:** você (mentor/agência) monta listas "produtos da semana" e cada afiliado recebe e importa com o próprio link.
6. **Dashboard de resultados:** cliques, pedidos e comissão por produto e por canal (Shopee `conversionReport`).

### V2
7. **Alertas:** produto disparando em vendas, comissão subiu, preço caiu, oferta relâmpago.
8. **Histórico** de preço, comissão e vendas por produto (gráfico).
9. **Biblioteca de referências de vídeo** (embed + notas + produto vinculado) e upload dos vídeos do próprio afiliado.
10. **IA de conteúdo:** gerar título, descrição, roteiro de vídeo curto e hashtags a partir do produto e da referência.
11. **TikTok Shop** (se a API de afiliado estiver liberada no Brasil) e sinal de tendências do YouTube.
12. **Vitrine pública** "link na bio" com tracking de clique.

### V3
13. Mercado Livre e Amazon (Creators API).
14. Disparo agendado para grupos de Telegram (API oficial de bots) e WhatsApp (somente via WhatsApp Business Platform oficial; bots não oficiais violam os termos e derrubam números).
15. Gamificação/ranking entre afiliados do mesmo mentor e metas.
16. Relatório fiscal: a Shopee passou a exigir, desde ago/2026, uma NFS-e por marca para afiliado PJ nas comissões extras. Um resumo por marca ajuda o afiliado.

## 5. Riscos e cuidados

- **Termos de uso:** usar apenas APIs oficiais. Scraping de Shopee/TikTok pode bloquear credenciais e contas dos afiliados.
- **Aprovação de APIs:** Shopee Open API e TikTok Partner Center exigem pedido e aprovação. Vale abrir essas solicitações já, antes do código.
- **Dependência de plataforma:** comissões e regras mudam sem aviso. Guardar snapshots ajuda a mostrar mudanças.
- **LGPD:** cliques em links curtos geram dados pessoais (IP, user agent). Precisamos de política de privacidade, minimização e retenção curta.
- **Credenciais:** AppID/Secret de cada afiliado criptografados (KMS/secret do cluster), nunca em log.
- **Cotas:** o polling de produtos tem que respeitar limites, com cache no Redis e jobs com backoff no worker.

## 6. Impacto na stack (para o thread de stack)

Nada muda nas escolhas atuais. Pontos para considerar:
- Jobs periódicos de coleta (River) por afiliado/credencial e um job global de tendências.
- Tabela de snapshots de produto (série temporal) em Postgres, particionada por mês.
- Serviço de link curto/redirect com contagem de cliques (caminho quente, cache Redis).
- Cofre de credenciais por usuário.

## 7. Decisões para o Rafael

1. **Começar só com Shopee no MVP?** (recomendo sim)
2. **Trocar "baixar vídeos" por embed + upload próprio?** (recomendo sim)
3. **O público inicial é você como mentor + seus afiliados, ou afiliados avulsos?** Isso muda o modelo de contas e de cobrança.

## Fontes

- [SDK Shopee.Affiliate (endpoint BR, assinatura, limites)](https://www.nuget.org/packages/Shopee.Affiliate)
- [saapi: wrapper da Shopee Affiliate API](https://pypi.org/project/saapi)
- [Shopee Help: API Access](https://help.shopee.sg/10/article/191702-API-Access)
- [TikTok for Developers: lançamento das Affiliate APIs](https://developers.tiktok.com/blog/2024-tiktok-shop-affiliate-apis-launch-developer-opportunity)
- [TikTok Commercial Content API](https://developers.tiktok.com/products/commercial-content-api)
- [TikTok Shop chega ao Brasil (newsroom)](https://newsroom.tiktok.com/tiktok-shop-chega-ao-brasil?lang=pt-BR)
- [Programa de afiliados TikTok Shop (requisitos EUA)](https://www.clickanalytic.com/tiktok-shop-affiliate-program/)
- [YouTube Shopping Afiliados Brasil](https://blog.youtube/intl/pt-br/news-and-events/yt-shopping-afiliados-brasil/)
- [Amazon: descontinuação da PA-API 5](https://affiliate-program.amazon.com/creatorsapi/docs/en-us/paapiv5-deprecation)
- [Comparativo FastMoss, Kalodata, Shoplus, EchoTik](https://www.admapix.com/blog/best-practices/fastmoss-alternative)
- [Mercado Livre Afiliados 2026](https://www.cupomonline.com.br/afiliado-mercado-livre-vale-a-pena/)
- [Mudanças na Shopee em 2026](https://actana.com.br/blog/mudancas-na-shopee-em-2026)
- [Divulga Ninja: blog de afiliados Shopee](https://www.divulganinja.com.br/blog/divulgador-inteligente-afiliados-shopee-whatsapp)

## 8. Detalhamento (02/10/2026)

**Decisão do Rafael:** o MVP começa apenas com a Shopee.

### 8.1 Como funciona "embed + upload próprio"

**A) Referência por embed (vídeos de outros criadores)**
1. O afiliado cola no app o link de um vídeo (TikTok, YouTube ou Shorts).
2. O app consulta o oEmbed oficial (`tiktok.com/oembed?url=...` e `youtube.com/oembed?url=...`) e guarda **só metadados**: título, autor, miniatura e URL. O arquivo de vídeo não é salvo.
3. O vídeo toca no player oficial dentro do card, vinculado ao produto, com campos para notas ("gancho nos 3 primeiros segundos", "mostra o antes/depois") e tags.
4. Se o criador apagar o vídeo, o card mostra "indisponível". Esse é o comportamento correto, porque o vídeo é dele.
5. Vídeos da Shopee Video não têm oEmbed público. Nesse caso guardamos o link e a miniatura do produto.

**B) Upload próprio (vídeos que o afiliado ou o mentor têm direito de usar)**
1. O usuário envia o vídeo pelo app. O upload vai direto para o storage (R2/GCS) por URL pré-assinada, sem passar pela API.
2. Um job no worker gera miniatura e uma versão leve (ffmpeg) para pré-visualização.
3. O vídeo fica vinculado a um ou mais produtos e pode ser baixado de volta para postar em qualquer rede.
4. No upload, o usuário marca "tenho direito de uso deste vídeo" (registro nos termos do app).
5. O mentor pode subir vídeos próprios na curadoria e liberá-los para os afiliados dele. Como o conteúdo é do mentor, ele pode distribuir.

**C) Mídia oficial do produto**
- As imagens que vêm da API da Shopee podem ser baixadas e usadas em criativos.

Impacto na stack: sai o yt-dlp e o scraping. Ficam storage de objetos + ffmpeg no worker para os uploads.

### 8.2 Mentor + afiliados × afiliados avulsos

| | Mentor + afiliados (B2B2C) | Afiliados avulsos (B2C) |
|---|---|---|
| Quem escolhe os produtos | O mentor faz a curadoria e envia listas | O próprio afiliado, guiado pelo radar automático |
| Entrada de usuários | O mentor convida a turma | Cada um se cadastra sozinho |
| Quem paga | O mentor (por assento/turma) ou o mentor repassa aos alunos | Cada afiliado paga a própria assinatura |
| Aquisição | Um cliente traz dezenas de usuários; CAC baixo | Marketing para cada usuário; CAC alto |
| Funcionalidades extras | Papéis (mentor/afiliado), convites, listas compartilhadas, painel do mentor com desempenho agregado da turma (com autorização do afiliado) | Onboarding autoexplicativo, radar mais forte, planos e cobrança self-service |
| Credenciais Shopee | Cada afiliado conecta a própria (igual nos dois) | Igual |
| Validação do MVP | Rápida: você já tem clientes para testar | Lenta: precisa achar usuários |

**Recomendação:** começar com mentor + afiliados e modelar os dados com "workspace" (organização). Um afiliado avulso vira depois um workspace de uma pessoa só, então a escolha não fecha a porta para o B2C.

### 8.3 Decisões confirmadas (02/10/2026)
- MVP apenas com Shopee.
- Sem download de vídeos de terceiros: embed oficial + upload próprio + mídia oficial do produto.
- Público: mentor + afiliados, cobrindo também afiliados avulsos. Modelo de "workspace": o avulso é um workspace de uma pessoa; o mentor é dono de um workspace com vários afiliados.
