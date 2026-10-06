# App secrets in Secret Manager, one per environment variable, named
# "<environment>-<VARIABLE>". The External Secrets Operator reads the ones with
# the labels app=parceiros and environment=<environment> and builds the
# parceiros-env Secret in the app namespace (see
# deploy/overlays/prod/environment/external-secret.yaml).
#
# Terraform only writes the value of DATABASE_URL and REDIS_URL, which it knows.
# The others start empty and get their value by hand, outside the repository:
#   printf '%s' "$VALUE" | gcloud secrets versions add prod-ASAAS_API_KEY --data-file=-

locals {
  manual_secrets = [
    "CRYPTO_KEK",    # 32 bytes in base64: openssl rand -base64 32
    "SHOPEE_APP_ID", # app credential of the Shopee Affiliate Open API
    "SHOPEE_APP_SECRET",
    "S3_ACCESS_KEY", # R2 API token with access to the bucket
    "S3_SECRET_KEY",
    "SMTP_USERNAME", # transactional email provider
    "SMTP_PASSWORD",
    "ASAAS_API_KEY",        # Asaas production API key
    "ASAAS_WEBHOOK_SECRET", # token set on the Asaas webhook
    "VAPID_PRIVATE_KEY",    # parceiros vapid
  ]

  generated_secrets = {
    DATABASE_URL = "postgres://${google_sql_user.parceiros.name}:${random_password.db.result}@${google_sql_database_instance.postgres.private_ip_address}:5432/${google_sql_database.parceiros.name}?sslmode=require"
    REDIS_URL    = "redis://:${google_redis_instance.redis.auth_string}@${google_redis_instance.redis.host}:${google_redis_instance.redis.port}/0"
  }

  secret_labels = {
    app         = "parceiros"
    environment = var.environment
  }
}

resource "google_secret_manager_secret" "manual" {
  for_each  = toset(local.manual_secrets)
  secret_id = "${var.environment}-${each.value}"
  labels    = local.secret_labels

  replication {
    user_managed {
      replicas {
        location = var.region
      }
    }
  }

  depends_on = [google_project_service.apis]
}

resource "google_secret_manager_secret" "generated" {
  for_each  = toset(keys(local.generated_secrets))
  secret_id = "${var.environment}-${each.value}"
  labels    = local.secret_labels

  replication {
    user_managed {
      replicas {
        location = var.region
      }
    }
  }

  depends_on = [google_project_service.apis]
}

resource "google_secret_manager_secret_version" "generated" {
  for_each    = google_secret_manager_secret.generated
  secret      = each.value.id
  secret_data = local.generated_secrets[each.key]
}
