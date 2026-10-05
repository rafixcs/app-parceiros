# Lançamento: o que falta para produção

Levantamento feito em 05/10/2026, com o MVP (M0 a M7) mergeado. A infraestrutura como código e os manifests de produção estão no repositório (seção 4), mas **nada foi criado na cloud**: criar recursos pagos e fazer o primeiro deploy esperam o ok do Rafael.

Legenda: ✅ pronto no repositório · ⬜ falta fazer · 🔶 decisão do Rafael

## 1. Decisões em aberto

- 🔶 **Cloud.** O repositório saiu com GCP (GKE Autopilot, Cloud SQL, Memorystore) em São Paulo, como sugerido em `docs/stack.md`. Trocar de cloud muda só `deploy/terraform/` e o componente `deploy/components/gcp`; o app não depende da cloud.
- 🔶 **Domínio** do app (ex.: `app.<marca>.com.br`) e da marca nos e-mails.
- 🔶 **Provedor de e-mail transacional** (Brevo, Resend, Amazon SES...). Precisa de SMTP na porta 587, porque a GCP bloqueia a porta 25.
- 🔶 **Preços e cobrança da mentoria** (`docs/mvp.md` §8, itens 3 a 5). Os valores atuais da tabela `limites` são provisórios.
- 🔶 **Textos legais**: termos de uso e política de privacidade (ver 3.1).

## 2. Contas e credenciais externas

Cada item gera valores que vão para o Secret Manager (segredos) ou para o ConfigMap do overlay (configuração). Nenhum segredo entra no git.

| | Serviço | O que fazer | Onde o valor entra |
|---|---|---|---|
| ⬜ | **Shopee Affiliate Open API** | Pedir acesso pelo painel de afiliados (pré-requisito desde o M2). Com a aprovação, conferir as respostas reais contra as gravadas em `testdata/` do mock e ajustar o que divergir. **É o item de maior risco do lançamento**: sem ele, o radar e os links não funcionam em produção. | `SHOPEE_APP_ID`, `SHOPEE_APP_SECRET` (segredos) |
| ⬜ | **Asaas** | Conta de produção (exige CNPJ e aprovação). Testar o fluxo inteiro no sandbox antes (`ASAAS_URL=https://api-sandbox.asaas.com/v3`). Criar o webhook apontando para `https://<domínio>/v1/webhooks/cobranca`, com um token de autenticação. | `ASAAS_CHAVE`, `ASAAS_WEBHOOK_SEGREDO` (segredos) |
| ⬜ | **Zitadel Cloud** | Criar a instância e um projeto; nele, um app do tipo *User Agent* (PKCE, sem segredo) com redirect `https://<domínio>/callback` e logout `https://<domínio>`. Ligar o login por e-mail e Google, a tela em pt-BR e o SMTP do Zitadel (para verificação de e-mail). | `OIDC_ISSUER`, `OIDC_AUDIENCE` (overlay) e `VITE_OIDC_ISSUER`, `VITE_OIDC_CLIENT_ID` (variáveis do GitHub) |
| ⬜ | **Cloudflare R2** | Criar o bucket `parceiros-prod` e um token de API só para ele. Configurar o CORS (abaixo). | `S3_ENDPOINT` (overlay), `S3_ACCESS_KEY`, `S3_SECRET_KEY` (segredos) |
| ⬜ | **Domínio e DNS** | Registrar o domínio (registro.br) e criar o registro A apontando para a saída `ip_ingress` do Terraform. O certificado é emitido pelo Google depois que o DNS propaga (pode levar até uma hora). | overlay (`TROCAR.dominio`) |
| ⬜ | **E-mail transacional** | Conta no provedor escolhido, domínio verificado com SPF, DKIM e DMARC. | `SMTP_ADDR`, `SMTP_REMETENTE` (overlay), `SMTP_USUARIO`, `SMTP_SENHA` (segredos) |
| ⬜ | **Web Push** | Gerar o par de chaves com `parceiros vapid`. | `VAPID_PUBLICA` (overlay), `VAPID_PRIVADA` (segredo) |
| ⬜ | **Chave mestra (KEK)** | `openssl rand -base64 32`. **Guarde uma cópia offline**: sem ela, as credenciais Shopee cifradas dos usuários ficam ilegíveis. | `CRYPTO_KEK` (segredo) |
| ⬜ | **GCP** | Projeto, conta de faturamento, alerta de orçamento e o bucket do estado do Terraform. | ver 4.2 |

CORS do bucket R2 (upload direto do navegador):

```json
[
  {
    "AllowedOrigins": ["https://<domínio>"],
    "AllowedMethods": ["GET", "PUT", "HEAD"],
    "AllowedHeaders": ["*"],
    "ExposeHeaders": ["ETag"],
    "MaxAgeSeconds": 3600
  }
]
```

## 3. Lacunas no app antes de abrir ao público

### 3.1 Obrigatórias

- ⬜ **LGPD: termos de uso e política de privacidade** publicados e aceitos no cadastro. Hoje não existem no app.
- ⬜ **LGPD: exclusão de conta e dos dados** a pedido do titular (e exportação, se possível). Hoje não há rota para isso; a credencial Shopee pode ser removida, mas a conta não.
- ⬜ **Validação com a API real da Shopee** (ver tabela acima): o mock foi gravado a partir da documentação.
- ⬜ **Teste de ponta a ponta no sandbox do Asaas**: assinar, pagar, receber o webhook, vencer e suspender.

### 3.2 Recomendadas

- ⬜ **Rastreamento de erros** (Sentry ou o Error Reporting do Google). Hoje há só logs.
- ⬜ **Chave mestra no Cloud KMS**, como previsto em `docs/stack.md`. Hoje a KEK vem de variável de ambiente (lida do Secret Manager), o que é aceitável para o lançamento, mas o KMS evita que a chave fique na memória dos pods e permite rotação.
- ⬜ **Ensaio de restauração do backup** do Cloud SQL antes do lançamento (o backup diário e o PITR de 7 dias já ficam ligados).
- ⬜ **Overlay de staging** nos moldes do de produção (hoje só tem a base). O Terraform cria staging com `ambiente = "staging"`.

### 3.3 Feitas neste levantamento

- ✅ Logs em JSON com `severity` e `message`, para o Cloud Logging mostrar o nível certo (`backend/internal/platform/logs`).
- ✅ Imagem de produção do front (`web/Dockerfile`, nginx), servida no mesmo domínio da API: o Ingress manda `/v1` para a API e o resto para o front, sem CORS.
- ✅ Uptime check do `/healthz` com alerta por e-mail (Terraform).

## 4. Infraestrutura como código

### 4.1 O que está no repositório

| Caminho | O que é |
|---|---|
| `deploy/terraform/gcp/` | VPC com Cloud NAT, GKE Autopilot com nós privados, Cloud SQL Postgres 17 (IP privado, TLS obrigatório, backup diário e PITR), Memorystore Redis com AUTH, Artifact Registry, Secret Manager (um segredo por variável), Workload Identity para o External Secrets e para o GitHub Actions, IP fixo do Ingress, uptime check e alerta. |
| `deploy/components/gcp/` | Front (nginx), Ingress do GKE com certificado gerenciado e redirecionamento para HTTPS, HPA e PDB da API, recursos de produção e pods sem root. |
| `deploy/overlays/prod/` | O app em produção. Valores a trocar marcados com `TROCAR`. |
| `deploy/overlays/prod/ambiente/` | Namespace, ConfigMap `parceiros-config` e o `ExternalSecret` que monta o Secret `parceiros-env` a partir do Secret Manager. |
| `deploy/argocd/prod.yaml` | As duas aplicações do Argo CD (ambiente e app). O deploy do app é manual: conferir o diff e sincronizar. |
| `.github/workflows/imagens.yml` | Publica as imagens no Artifact Registry a cada push na `main`, com a tag do commit. Desligado até as variáveis do repositório existirem. |

### 4.2 Passo a passo do primeiro deploy (depois do ok)

1. Criar o projeto GCP, ligar o faturamento e criar o bucket do estado:
   `gcloud storage buckets create gs://<projeto>-tfstate --location=southamerica-east1 --uniform-bucket-level-access --public-access-prevention`
2. `cd deploy/terraform/gcp`, copiar `prod.tfvars.example` para `prod.tfvars` e ajustar.
3. `terraform init -backend-config="bucket=<projeto>-tfstate" -backend-config="prefix=prod"`, depois `terraform plan -var-file=prod.tfvars` e, com o plano conferido, `terraform apply -var-file=prod.tfvars`.
4. Preencher os segredos listados na saída `segredos_a_preencher`:
   `printf '%s' "$VALOR" | gcloud secrets versions add prod-ASAAS_CHAVE --data-file=-`
5. Conectar ao cluster (`gcloud container clusters get-credentials parceiros-prod --region southamerica-east1`) e instalar pelo Helm o External Secrets Operator (namespace `external-secrets`, nome da release `external-secrets`) e o Argo CD (namespace `argocd`).
6. Trocar os `TROCAR` em `deploy/overlays/prod` e `deploy/overlays/prod/ambiente` e criar as variáveis do repositório no GitHub (ver o cabeçalho de `imagens.yml`).
7. Fazer merge na `main` e esperar o workflow **Imagens** publicar. Pôr a tag do commit em `deploy/overlays/prod/kustomization.yaml` (`newTag`) por PR.
8. `kubectl apply -n argocd -f deploy/argocd/prod.yaml`. Conferir que o Secret `parceiros-env` apareceu em `parceiros-prod` e sincronizar `parceiros-prod` (a migration roda antes, como PreSync).
9. Criar o registro A do domínio e esperar o certificado ficar `Active` (`kubectl -n parceiros-prod get managedcertificate`).
10. Configurar o webhook do Asaas e o redirect do Zitadel com o domínio final.
11. Teste de fumaça (4.3).

Deploys seguintes: merge na `main`, trocar o `newTag` por PR e sincronizar no Argo CD.

### 4.3 Teste de fumaça

- ⬜ `https://<domínio>/healthz` responde e o uptime check fica verde.
- ⬜ Cadastro e login pelo Zitadel; o workspace pessoal nasce em teste.
- ⬜ Conectar a credencial Shopee e ver o radar com produtos reais.
- ⬜ Salvar um produto e copiar o link de afiliado.
- ⬜ Mentor cria workspace, convida um afiliado (e-mail chega), publica uma lista e o afiliado importa.
- ⬜ Upload de um vídeo próprio e a prévia gerada pelo worker.
- ⬜ Assinar e pagar no Asaas; o webhook estende o acesso.
- ⬜ Notificação no app, por e-mail e por push.

## 5. Custo estimado

Estimativa por mês em `southamerica-east1`, a partir das tabelas públicas de preço do Google, sem tráfego e sem impostos. É uma inferência, não uma cotação: confira na calculadora do Google antes de aprovar.

| Item | Configuração | US$/mês |
|---|---|---|
| GKE Autopilot | taxa do cluster (coberta pelo crédito mensal gratuito de um cluster) + pods (~1,1 vCPU e ~1,1 GB) | 55 a 70 |
| Cloud SQL | 1 vCPU, 3,75 GB, 20 GB SSD, zonal, backups | 70 a 90 |
| Memorystore | Redis básico, 1 GB | 45 a 60 |
| Load balancer + IP | 1 regra de encaminhamento | ~20 |
| Cloud NAT | gateway + tráfego de saída | 5 a 15 |
| **Total** | | **~200 a 250** |

Fora da GCP: Zitadel Cloud e R2 têm faixa gratuita que cobre o começo; o Asaas cobra por transação; o e-mail depende do provedor. Para baixar a conta: o Redis pode rodar dentro do cluster (economiza o Memorystore) e a alta disponibilidade do Cloud SQL (`db_alta_disponibilidade`) fica desligada até haver clientes pagando.
