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
1. Planos `avulso` e `mentoria` (por assento), com limites e preços em tabela (`limites`): assentos, assentos do teste, cota de vídeo, número de listas e preço.
2. Checkout no gateway (Asaas, atrás da interface `assinaturas.Gateway`), com a fatura paga em PIX, boleto ou cartão, e webhook que ativa ou suspende o workspace.
   - O acesso é a data `workspaces.acesso_ate`: cada pagamento confirmado a estende até o fim do ciclo, mais 3 dias de tolerância; passada a data, o workspace fica suspenso sem depender de job nem de webhook.
   - Um estorno suspende na hora. Cancelar mantém o acesso até o fim do período já pago.
   - Suspenso, o workspace só deixa ver a si mesmo, sair dele, mexer no consentimento e cuidar da assinatura; o resto responde 402.
3. Período de teste de 7 dias, com assentos de teste (5 na mentoria).
4. Assentos: um por afiliado da turma (convite pendente já ocupa). Diminuir vale na hora, nunca abaixo dos em uso; aumentar vale no próximo pagamento confirmado.

## 5. Modelo de dados (inicial)

```
usuarios(id, zitadel_sub, nome, email, criado_em)
workspaces(id, tipo[pessoal|mentoria], nome, dono_id, plano, status, criado_em)
membros(workspace_id, usuario_id, papel[dono|mentor|afiliado], consente_resultados bool, entrou_em)
convites(id, workspace_id, email?, token, expira_em, usado_por?, criado_por)
credenciais_shopee(usuario_id, app_id, secret_cifrado, dek_cifrada, status, verificado_em)

produtos(id, fonte[shopee], item_id, loja_id, nome, imagem_url, categoria_id, url, atualizado_em)
produto_snapshots(produto_id, coletado_em, preco_min_centavos, preco_max_centavos, comissao_bp, vendas, nota)  -- particionada por mês
tendencias(produto_id, calculado_em, score, ganho_por_venda_centavos, variacao_vendas_7d)

itens_colecao(id, workspace_id, usuario_id, produto_id, titulo, descricao, notas, tags[], status, link_afiliado, link_origem[auto|manual], criado_em)
colecoes(id, workspace_id, usuario_id, nome)
colecao_itens(colecao_id, item_id)

listas_curadoria(id, workspace_id, autor_id, titulo, descricao, publicada_em)
lista_itens(lista_id, produto_id, comentario, ordem)
importacoes(lista_id, usuario_id, importado_em)

videos(id, workspace_id, dono_id, tipo[embed|upload], plataforma, url, titulo, autor, thumb_url, storage_key, duracao_s, status, compartilhado, direito_uso_em)
video_vinculos(video_id, workspace_id, dono_id, alvo_tipo[produto|lista], alvo_id)
uso_videos(workspace_id, bytes)

links_canal(item_id, canal, sub_id, url)
conversoes(id, usuario_id, workspace_id?, produto_id?, sub_id, pedido_id, status, valor_centavos, comissao_centavos, ocorrido_em)

limites(plano, chave, valor)
workspaces.acesso_ate, workspaces.pago_em, workspaces.assentos  -- acesso e assentos contratados
assinaturas(workspace_id, provedor, cliente_externo_id, externo_id, status, assentos, valor_centavos, proximo_ciclo, url_pagamento, criado_por, cancelada_em)
eventos_cobranca(provedor, evento_id, workspace_id, tipo, recebido_em)  -- reenvios do webhook
```

Toda tabela com `workspace_id` tem RLS. Valores em dinheiro ficam em centavos (`bigint`) e comissões em basis points (`comissao_bp`, 1% = 100).

## 6. Jobs (River)

| Job | Fila | Gatilho | Observações |
|---|---|---|---|
| `snapshot_catalogo` | shopee | a cada 6 h por categoria | Paginação sequencial (`scrollId` dura ~30 s, 50 itens por página). Guarda o JSON bruto no R2. |
| `calcular_tendencias` | default | depois de cada snapshot | Score = crescimento de vendas em 7 dias, ponderado por comissão e nota. |
| `gerar_link` | shopee | ao salvar ou importar | Usa a credencial do usuário. Retry com backoff. |
| `sync_conversoes` | shopee | diário por usuário | Janela ≤ 90 dias. |
| `revalidar_embed` | default | 7 dias depois de colar, e a cada 7 dias | Um job por vídeo, com o escopo do dono. O oEmbed ao colar é síncrono, com cache de 24 h no Redis. |
| `processar_video` | midia | fim do upload | ffmpeg: duração, miniatura e prévia em 720p. |
| `limpar_upload` | midia | 24 h depois de iniciar o upload | Um job por upload; descarta o que não terminou e devolve a cota. |

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

1. Cloud de produção (sugestão: GCP).
2. Asaas ou Mercado Pago: o M7 saiu com Asaas, a confirmar. Trocar é escrever outra implementação de `assinaturas.Gateway`.
3. No plano Mentoria, quem paga: o M7 saiu com o mentor pagando por assento (o afiliado avulso paga o próprio plano), a confirmar.
4. Preços: R$ 29,90 por mês no avulso e R$ 14,90 por assento na mentoria, provisórios na tabela `limites`.
