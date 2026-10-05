# Rede própria com faixas secundárias para os pods e serviços do GKE, acesso
# privado aos serviços gerenciados (Cloud SQL e Memorystore sem IP público) e
# Cloud NAT para a saída dos nós privados (Shopee, Asaas, Zitadel, R2, SMTP).

resource "google_compute_network" "vpc" {
  name                    = "parceiros-${var.ambiente}"
  auto_create_subnetworks = false
  depends_on              = [google_project_service.apis]
}

resource "google_compute_subnetwork" "gke" {
  name                     = "parceiros-${var.ambiente}-gke"
  network                  = google_compute_network.vpc.id
  region                   = var.regiao
  ip_cidr_range            = "10.10.0.0/20"
  private_ip_google_access = true

  secondary_ip_range {
    range_name    = "pods"
    ip_cidr_range = "10.20.0.0/16"
  }
  secondary_ip_range {
    range_name    = "servicos"
    ip_cidr_range = "10.30.0.0/20"
  }
}

resource "google_compute_global_address" "servicos_privados" {
  name          = "parceiros-${var.ambiente}-servicos-privados"
  network       = google_compute_network.vpc.id
  purpose       = "VPC_PEERING"
  address_type  = "INTERNAL"
  prefix_length = 20
}

resource "google_service_networking_connection" "servicos_privados" {
  network                 = google_compute_network.vpc.id
  service                 = "servicenetworking.googleapis.com"
  reserved_peering_ranges = [google_compute_global_address.servicos_privados.name]
}

resource "google_compute_router" "nat" {
  name    = "parceiros-${var.ambiente}-nat"
  network = google_compute_network.vpc.id
  region  = var.regiao
}

resource "google_compute_router_nat" "nat" {
  name                               = "parceiros-${var.ambiente}-nat"
  router                             = google_compute_router.nat.name
  region                             = var.regiao
  nat_ip_allocate_option             = "AUTO_ONLY"
  source_subnetwork_ip_ranges_to_nat = "ALL_SUBNETWORKS_ALL_IP_RANGES"

  log_config {
    enable = true
    filter = "ERRORS_ONLY"
  }
}

# IP fixo do load balancer do Ingress. O registro DNS do domínio aponta para ele.
resource "google_compute_global_address" "ingress" {
  name = "parceiros-${var.ambiente}-ingress"
}
