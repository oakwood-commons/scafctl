# Git HTTPS Credential Helper Examples

Quick-reference configurations for authenticating `git` over HTTPS with `scafctl git-credential`.

## Default Per-Host Setup

Authenticate once and register per-host helpers in `~/.gitconfig`:

~~~bash
scafctl auth login github
scafctl git-credential install
~~~

Generated `~/.gitconfig` section:

~~~ini
[credential "https://github.com"]
	helper = 
	helper = !"/usr/local/bin/scafctl" git-credential
	useHttpPath = true
~~~

## Multi-Org GitHub Profile Routing

Route `https://github.com/work-org/*` and `https://github.com/public-org/*` to isolated auth profiles in `~/.config/scafctl/config.yaml`:

~~~yaml
auth:
  github:
    activeProfile: personal
    profiles:
      work: {}
      personal: {}
      enterprise:
        hostname: ghes.acme-corp.example.com
  git:
    rules:
      - host: github.com
        pathPrefix: work-org
        handler: github
        profile: work
      - host: github.com
        pathPrefix: public-org
        handler: github
        profile: personal
~~~

Log in to each profile and install:

~~~bash
scafctl auth login github --profile work
scafctl auth login github --profile personal
scafctl auth login github --profile enterprise --hostname ghes.acme-corp.example.com
scafctl git-credential install
~~~

## Azure DevOps and Google Cloud Source Repositories

`dev.azure.com` / `*.visualstudio.com` map automatically to `entra` (using the Azure DevOps resource scope `499b84ac-1321-427f-aa17-267ca6975798/.default`), and `source.developers.google.com` maps automatically to `gcp`:

~~~bash
scafctl auth login entra
scafctl auth login gcp
scafctl git-credential install
~~~

## Self-Hosted GitLab / Gitea (`auth.customOAuth2`)

~~~bash
scafctl auth oauth2 add acme-gitlab \
  --display-name "Acme GitLab" \
  --authorize-url https://git.example.com/oauth/authorize \
  --token-url https://git.example.com/oauth/token \
  --client-id gitlab-cli-client \
  --default-flow interactive \
  --scopes read_repository,write_repository \
  --git-host git.example.com \
  --git-username oauth2

scafctl auth login acme-gitlab
scafctl git-credential install --host git.example.com
~~~

## Repository-Local Setup and Direct Protocol Testing

~~~bash
# Configure only the current repository (.git/config) with a fixed profile
scafctl git-credential install --local --host github.com --profile work

# Inspect structured install output
scafctl git-credential install --file /tmp/gitconfig.preview -o json

# Test protocol resolution directly
printf 'protocol=https\nhost=github.com\npath=work-org/service.git\n\n' | scafctl git-credential get

# Clean up managed entries without touching foreign helpers
scafctl git-credential uninstall --local
~~~
