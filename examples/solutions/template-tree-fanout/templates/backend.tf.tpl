terraform {
  backend "gcs" {
    bucket = "{{ .env.bucket }}"
    prefix = "{{ .platformAppName }}/{{ .env.name }}"
  }
}

# Environment: {{ .env.name }} (region {{ .env.region }})
