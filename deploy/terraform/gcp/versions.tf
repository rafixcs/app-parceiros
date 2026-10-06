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

  # Remote state in a private GCS bucket, created by hand once (see docs/lancamento.md).
  # The state holds the database password: the bucket must not be public.
  backend "gcs" {}
}

provider "google" {
  project = var.project
  region  = var.region
}
