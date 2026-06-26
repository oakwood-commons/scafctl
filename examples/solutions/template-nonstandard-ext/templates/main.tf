resource "app_service" "main" {
  name   = "{{ .appName }}"
  region = "{{ .region }}"
}
