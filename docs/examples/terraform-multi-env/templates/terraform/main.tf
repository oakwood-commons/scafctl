locals {
  environment = "{{ .environment }}"
  region      = "{{ .region }}"
  project_id = "{{ .projectId }}"

  common_labels = {
    environment = local.environment
    managed_by  = "terraform"
    created_by  = "scafctl"
  }
}

# Storage bucket for application data
resource "google_storage_bucket" "app_data" {
  name          = "${local.project_id}-app-data-${local.environment}"
  location      = local.region
  force_destroy = local.environment != "prod"

  uniform_bucket_level_access = true

  labels = local.common_labels
}

# Output for reference in other configurations
output "bucket_name" {
  value       = google_storage_bucket.app_data.name
  description = "Name of the application data bucket"
}

output "bucket_url" {
  value       = google_storage_bucket.app_data.url
  description = "URL of the application data bucket"
}
