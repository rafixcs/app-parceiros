# Postgres (Cloud SQL) and Redis (Memorystore), with private IPs in the VPC only.

resource "google_sql_database_instance" "postgres" {
  name             = "parceiros-${var.environment}"
  database_version = "POSTGRES_17"
  region           = var.region

  settings {
    tier              = var.db_tier
    edition           = "ENTERPRISE"
    availability_type = var.db_high_availability ? "REGIONAL" : "ZONAL"
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
      start_time                     = "06:00" # 03:00 in Brasília
      transaction_log_retention_days = 7
      backup_retention_settings {
        retained_backups = 14
      }
    }

    maintenance_window {
      day  = 7 # Sunday
      hour = 7 # 04:00 in Brasília
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
  depends_on          = [google_service_networking_connection.private_services]
}

resource "google_sql_database" "parceiros" {
  name     = "parceiros"
  instance = google_sql_database_instance.postgres.name
}

# Owner of the tables: runs the migrations and the worker. The API takes the
# parceiros_app role in each transaction (RLS), created by migration 00002.
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
  name               = "parceiros-${var.environment}"
  region             = var.region
  tier               = "BASIC"
  memory_size_gb     = var.redis_gb
  redis_version      = "REDIS_7_2"
  authorized_network = google_compute_network.vpc.id
  connect_mode       = "PRIVATE_SERVICE_ACCESS"
  auth_enabled       = true

  depends_on = [google_service_networking_connection.private_services]
}
