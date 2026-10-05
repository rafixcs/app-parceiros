terraform {
  required_version = ">= 1.9"

  required_providers {
    google = {
      source  = "hashicorp/google"
      version = "~> 8.5"
    }
    random = {
      source  = "hashicorp/random"
      version = "~> 3.9"
    }
  }

  # Estado remoto num bucket GCS privado, criado à mão uma vez (veja o README).
  # O estado guarda a senha do banco: o bucket não pode ser público.
  backend "gcs" {}
}

provider "google" {
  project = var.projeto
  region  = var.regiao
}
