# Segredos do app no Secret Manager, um por variável de ambiente, com o nome
# "<ambiente>-<VARIAVEL>". O External Secrets Operator lê os que têm os rótulos
# app=parceiros e ambiente=<ambiente> e monta o Secret parceiros-env no
# namespace do app (veja deploy/components/gcp/external-secret.yaml).
#
# O Terraform só grava o valor de DATABASE_URL e REDIS_URL, que ele conhece. Os
# outros nascem vazios e recebem o valor à mão, fora do repositório:
#   printf '%s' "$VALOR" | gcloud secrets versions add prod-ASAAS_API_KEY --data-file=-

locals {
  segredos_manuais = [
    "CRYPTO_KEK",    # 32 bytes em base64: openssl rand -base64 32
    "SHOPEE_APP_ID", # credencial do app na Shopee Affiliate Open API
    "SHOPEE_APP_SECRET",
    "S3_ACCESS_KEY", # token de API do R2 com acesso ao bucket
    "S3_SECRET_KEY",
    "SMTP_USERNAME", # provedor de e-mail transacional
    "SMTP_PASSWORD",
    "ASAAS_API_KEY",        # chave de API de produção do Asaas
    "ASAAS_WEBHOOK_SECRET", # token configurado no webhook do Asaas
    "VAPID_PRIVATE_KEY",    # parceiros vapid
  ]

  segredos_gerados = {
    DATABASE_URL = "postgres://${google_sql_user.parceiros.name}:${random_password.db.result}@${google_sql_database_instance.postgres.private_ip_address}:5432/${google_sql_database.parceiros.name}?sslmode=require"
    REDIS_URL    = "redis://:${google_redis_instance.redis.auth_string}@${google_redis_instance.redis.host}:${google_redis_instance.redis.port}/0"
  }

  rotulos_segredo = {
    app      = "parceiros"
    ambiente = var.ambiente
  }
}

resource "google_secret_manager_secret" "manual" {
  for_each  = toset(local.segredos_manuais)
  secret_id = "${var.ambiente}-${each.value}"
  labels    = local.rotulos_segredo

  replication {
    user_managed {
      replicas {
        location = var.regiao
      }
    }
  }

  depends_on = [google_project_service.apis]
}

resource "google_secret_manager_secret" "gerado" {
  for_each  = toset(keys(local.segredos_gerados))
  secret_id = "${var.ambiente}-${each.value}"
  labels    = local.rotulos_segredo

  replication {
    user_managed {
      replicas {
        location = var.regiao
      }
    }
  }

  depends_on = [google_project_service.apis]
}

resource "google_secret_manager_secret_version" "gerado" {
  for_each    = google_secret_manager_secret.gerado
  secret      = each.value.id
  secret_data = local.segredos_gerados[each.key]
}
