# {{ .platformAppName }} - {{ .environment.name }} backend
terraform {
  backend "gcs" {
    bucket = "{{ .environment.bucket }}"
    prefix = "{{ .platformAppName }}/{{ .environment.name }}"
  }
}

# Region: {{ .environment.region }}
