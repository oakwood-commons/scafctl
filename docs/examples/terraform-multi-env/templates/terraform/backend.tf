terraform {
  required_version = ">= 1.0"
  required_providers {
    google = {
      source  = "hashicorp/google"
      version = "~> 5.0"
    }
  }

  backend "gcs" {
    bucket  = "{{ .stateBucket }}"
    prefix  = "terraform/{{ .environment }}/state"
  }
}

provider "google" {
  project = "{{ .projectId }}"
  region  = "{{ .region }}"
}
