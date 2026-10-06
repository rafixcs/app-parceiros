variable "project" {
  description = "GCP project ID."
  type        = string
}

variable "region" {
  description = "Region of everything. São Paulo by default, close to the users and to Shopee BR."
  type        = string
  default     = "southamerica-east1"
}

variable "environment" {
  description = "Environment name (prod or staging). Goes into the resource names and the Kubernetes namespace."
  type        = string
  default     = "prod"

  validation {
    condition     = contains(["prod", "staging"], var.environment)
    error_message = "environment must be prod or staging."
  }
}

variable "github_repository" {
  description = "Repository allowed to push images to Artifact Registry from GitHub Actions (owner/name)."
  type        = string
  default     = "rafixcs/app-parceiros"
}

variable "db_tier" {
  description = "Cloud SQL machine. db-custom-1-3840 = 1 vCPU and 3.75 GB."
  type        = string
  default     = "db-custom-1-3840"
}

variable "db_high_availability" {
  description = "Regional Cloud SQL (replica in another zone). Doubles the database cost."
  type        = bool
  default     = false
}

variable "redis_gb" {
  description = "Memorystore (Redis) memory in GB."
  type        = number
  default     = 1
}

variable "alert_email" {
  description = "Email that gets the uptime check alerts. Empty turns the alerts off."
  type        = string
  default     = ""
}

variable "domain" {
  description = "Public domain of the app (e.g. app.parceiros.com.br). Used by the uptime check."
  type        = string
}
