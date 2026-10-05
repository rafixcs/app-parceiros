locals {
  pool_gke = "${var.projeto}.svc.id.goog"
  # Conta de serviço do Kubernetes do External Secrets Operator (instalado pelo
  # Helm chart oficial no namespace external-secrets).
  principal_eso = "principal://iam.googleapis.com/projects/${data.google_project.atual.number}/locations/global/workloadIdentityPools/${local.pool_gke}/subject/ns/external-secrets/sa/external-secrets"
}

# External Secrets lê os segredos pelo Workload Identity, sem chave de conta de
# serviço. Listar é no projeto (a busca por rótulo); ler é segredo a segredo.
resource "google_project_iam_member" "eso_listar" {
  project    = var.projeto
  role       = "roles/secretmanager.viewer"
  member     = local.principal_eso
  depends_on = [google_container_cluster.gke]
}

resource "google_secret_manager_secret_iam_member" "eso_ler" {
  for_each   = merge(google_secret_manager_secret.manual, google_secret_manager_secret.gerado)
  secret_id  = each.value.id
  role       = "roles/secretmanager.secretAccessor"
  member     = local.principal_eso
  depends_on = [google_container_cluster.gke]
}

# GitHub Actions publica imagens no Artifact Registry por Workload Identity
# Federation: só este repositório e só a partir da branch main.
resource "google_iam_workload_identity_pool" "github" {
  workload_identity_pool_id = "github"
  display_name              = "GitHub Actions"
  depends_on                = [google_project_service.apis]
}

resource "google_iam_workload_identity_pool_provider" "github" {
  workload_identity_pool_id          = google_iam_workload_identity_pool.github.workload_identity_pool_id
  workload_identity_pool_provider_id = "github"
  display_name                       = "GitHub OIDC"

  attribute_mapping = {
    "google.subject"       = "assertion.sub"
    "attribute.repository" = "assertion.repository"
    "attribute.ref"        = "assertion.ref"
  }
  attribute_condition = "assertion.repository == \"${var.repositorio_github}\" && assertion.ref == \"refs/heads/main\""

  oidc {
    issuer_uri = "https://token.actions.githubusercontent.com"
  }
}

resource "google_artifact_registry_repository_iam_member" "github_publicar" {
  location   = google_artifact_registry_repository.imagens.location
  repository = google_artifact_registry_repository.imagens.name
  role       = "roles/artifactregistry.writer"
  member     = "principalSet://iam.googleapis.com/${google_iam_workload_identity_pool.github.name}/attribute.repository/${var.repositorio_github}"
}
