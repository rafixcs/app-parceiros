variable "projeto" {
  description = "ID do projeto GCP."
  type        = string
}

variable "regiao" {
  description = "Região de tudo. São Paulo por padrão, perto dos usuários e da Shopee BR."
  type        = string
  default     = "southamerica-east1"
}

variable "ambiente" {
  description = "Nome do ambiente (prod ou staging). Vai nos nomes dos recursos e no namespace do Kubernetes."
  type        = string
  default     = "prod"

  validation {
    condition     = contains(["prod", "staging"], var.ambiente)
    error_message = "ambiente deve ser prod ou staging."
  }
}

variable "repositorio_github" {
  description = "Repositório que pode publicar imagens no Artifact Registry pelo GitHub Actions (dono/nome)."
  type        = string
  default     = "rafixcs/app-parceiros"
}

variable "db_tier" {
  description = "Máquina do Cloud SQL. db-custom-1-3840 = 1 vCPU e 3,75 GB."
  type        = string
  default     = "db-custom-1-3840"
}

variable "db_alta_disponibilidade" {
  description = "Cloud SQL regional (réplica em outra zona). Dobra o custo do banco."
  type        = bool
  default     = false
}

variable "redis_gb" {
  description = "Memória do Memorystore (Redis) em GB."
  type        = number
  default     = 1
}

variable "email_alertas" {
  description = "E-mail que recebe os alertas do uptime check. Vazio desliga os alertas."
  type        = string
  default     = ""
}

variable "dominio" {
  description = "Domínio público do app (ex.: app.parceiros.com.br). Usado no uptime check."
  type        = string
}
