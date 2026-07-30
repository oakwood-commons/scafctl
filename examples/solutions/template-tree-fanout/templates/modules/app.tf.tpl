module "app" {
  source = "../../modules/app"

  name        = "{{ .platformAppName }}-{{ .env.name }}"
  environment = "{{ .env.name }}"
  region      = "{{ .env.region }}"
}
