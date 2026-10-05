# Postgres (Cloud SQL) e Redis (Memorystore), só com IP privado na VPC.

resource "google_sql_database_instance" "postgres" {
  name             = "parceiros-${var.ambiente}"
  database_version = "POSTGRES_17"
  region           = var.regiao

  settings {
    tier              = var.db_tier
    edition           = "ENTERPRISE"
    availability_type = var.db_alta_disponibilidade ? "REGIONAL" : "ZONAL"
    disk_autoresize   = true
    disk_size         = 20

    ip_configuration {
      ipv4_enabled    = false
      private_network = google_compute_network.vpc.id
      ssl_mode        = "ENCRYPTED_ONLY"
    }

    backup_configuration {
      enabled                        = true
      point_in_time_recovery_enabled = true
      start_time                     = "06:00" # 03:00 em Brasília
      transaction_log_retention_days = 7
      backup_retention_settings {
        retained_backups = 14
      }
    }

    maintenance_window {
      day  = 7 # domingo
      hour = 7 # 04:00 em Brasília
    }

    insights_config {
      query_insights_enabled = true
    }

    database_flags {
      name  = "max_connections"
      value = "100"
    }
  }

  deletion_protection = true
  depends_on          = [google_service_networking_connection.servicos_privados]
}

resource "google_sql_database" "parceiros" {
  name     = "parceiros"
  instance = google_sql_database_instance.postgres.name
}

# Usuário dono das tabelas: roda as migrations e o worker. A API assume o papel
# parceiros_app em cada transação (RLS), criado pela migration 00002.
resource "random_password" "db" {
  length  = 32
  special = false
}

resource "google_sql_user" "parceiros" {
  name     = "parceiros"
  instance = google_sql_database_instance.postgres.name
  password = random_password.db.result
}

resource "google_redis_instance" "redis" {
  name               = "parceiros-${var.ambiente}"
  region             = var.regiao
  tier               = "BASIC"
  memory_size_gb     = var.redis_gb
  redis_version      = "REDIS_7_2"
  authorized_network = google_compute_network.vpc.id
  connect_mode       = "PRIVATE_SERVICE_ACCESS"
  auth_enabled       = true

  depends_on = [google_service_networking_connection.servicos_privados]
}
