output "cluster" {
  description = "Para o kubectl: gcloud container clusters get-credentials <cluster> --region <regiao>"
  value       = google_container_cluster.gke.name
}

output "ip_ingress" {
  description = "IP do load balancer. Crie o registro A do domínio apontando para ele."
  value       = google_compute_global_address.ingress.address
}

output "nome_ip_ingress" {
  description = "Nome do IP fixo, usado na anotação do Ingress."
  value       = google_compute_global_address.ingress.name
}

output "registro_imagens" {
  description = "Prefixo das imagens no Artifact Registry."
  value       = "${var.regiao}-docker.pkg.dev/${var.projeto}/${google_artifact_registry_repository.imagens.repository_id}"
}

output "provedor_wif_github" {
  description = "Valor da variável GCP_WIF_PROVIDER do repositório no GitHub."
  value       = google_iam_workload_identity_pool_provider.github.name
}

output "segredos_a_preencher" {
  description = "Segredos que precisam de um valor à mão antes do primeiro deploy."
  value       = sort([for s in google_secret_manager_secret.manual : s.secret_id])
}
