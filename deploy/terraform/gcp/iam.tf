locals {
  gke_pool = "${var.project}.svc.id.goog"
  # Kubernetes service account of the External Secrets Operator (installed by
  # the official Helm chart in the external-secrets namespace).
  eso_principal = "principal://iam.googleapis.com/projects/${data.google_project.current.number}/locations/global/workloadIdentityPools/${local.gke_pool}/subject/ns/external-secrets/sa/external-secrets"
}

# External Secrets reads the secrets through Workload Identity, without a
# service account key. Listing is on the project (the search by label); reading
# is secret by secret.
resource "google_project_iam_member" "eso_list" {
  project    = var.project
  role       = "roles/secretmanager.viewer"
  member     = local.eso_principal
  depends_on = [google_container_cluster.gke]
}

resource "google_secret_manager_secret_iam_member" "eso_read" {
  for_each   = merge(google_secret_manager_secret.manual, google_secret_manager_secret.generated)
  secret_id  = each.value.id
  role       = "roles/secretmanager.secretAccessor"
  member     = local.eso_principal
  depends_on = [google_container_cluster.gke]
}

# GitHub Actions pushes images to Artifact Registry through Workload Identity
# Federation: only this repository and only from the main branch.
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
  attribute_condition = "assertion.repository == \"${var.github_repository}\" && assertion.ref == \"refs/heads/main\""

  oidc {
    issuer_uri = "https://token.actions.githubusercontent.com"
  }
}

resource "google_artifact_registry_repository_iam_member" "github_push" {
  location   = google_artifact_registry_repository.images.location
  repository = google_artifact_registry_repository.images.name
  role       = "roles/artifactregistry.writer"
  member     = "principalSet://iam.googleapis.com/${google_iam_workload_identity_pool.github.name}/attribute.repository/${var.github_repository}"
}
