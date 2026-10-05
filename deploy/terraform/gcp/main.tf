locals {
  apis = [
    "compute.googleapis.com",
    "container.googleapis.com",
    "sqladmin.googleapis.com",
    "redis.googleapis.com",
    "servicenetworking.googleapis.com",
    "secretmanager.googleapis.com",
    "artifactregistry.googleapis.com",
    "iam.googleapis.com",
    "iamcredentials.googleapis.com",
    "sts.googleapis.com",
    "monitoring.googleapis.com",
    "logging.googleapis.com",
  ]
  namespace = "parceiros-${var.ambiente}"
}

data "google_project" "atual" {}

resource "google_project_service" "apis" {
  for_each           = toset(local.apis)
  service            = each.value
  disable_on_destroy = false
}

# GKE Autopilot: o Google cuida dos nós; paga-se pelos pods. Nós privados,
# Workload Identity ligado (padrão do Autopilot) e canal de versões regular.
resource "google_container_cluster" "gke" {
  name             = "parceiros-${var.ambiente}"
  location         = var.regiao
  enable_autopilot = true
  network          = google_compute_network.vpc.id
  subnetwork       = google_compute_subnetwork.gke.id

  ip_allocation_policy {
    cluster_secondary_range_name  = "pods"
    services_secondary_range_name = "servicos"
  }

  private_cluster_config {
    enable_private_nodes    = true
    enable_private_endpoint = false
    master_ipv4_cidr_block  = "172.16.0.0/28"
  }

  release_channel {
    channel = "REGULAR"
  }

  deletion_protection = true
}

resource "google_artifact_registry_repository" "imagens" {
  location      = var.regiao
  repository_id = "parceiros"
  format        = "DOCKER"
  description   = "Imagens do App Parceiros (api/worker e web)."

  cleanup_policies {
    id     = "manter-recentes"
    action = "KEEP"
    most_recent_versions {
      keep_count = 30
    }
  }
  cleanup_policies {
    id     = "apagar-antigas"
    action = "DELETE"
    condition {
      older_than = "2592000s" # 30 dias
    }
  }

  depends_on = [google_project_service.apis]
}
