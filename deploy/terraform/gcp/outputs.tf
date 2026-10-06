output "cluster" {
  description = "For kubectl: gcloud container clusters get-credentials <cluster> --region <region>"
  value       = google_container_cluster.gke.name
}

output "ingress_ip" {
  description = "IP of the load balancer. Point the domain's A record to it."
  value       = google_compute_global_address.ingress.address
}

output "ingress_ip_name" {
  description = "Name of the static IP, used in the Ingress annotation."
  value       = google_compute_global_address.ingress.name
}

output "image_registry" {
  description = "Prefix of the images in Artifact Registry."
  value       = "${var.region}-docker.pkg.dev/${var.project}/${google_artifact_registry_repository.images.repository_id}"
}

output "github_wif_provider" {
  description = "Value of the GCP_WIF_PROVIDER variable of the GitHub repository."
  value       = google_iam_workload_identity_pool_provider.github.name
}

output "secrets_to_fill" {
  description = "Secrets that need a value by hand before the first deploy."
  value       = sort([for s in google_secret_manager_secret.manual : s.secret_id])
}
