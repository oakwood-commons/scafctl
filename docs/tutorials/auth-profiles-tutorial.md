---
title: "Auth Profiles Tutorial"
weight: 41
---

# Auth Profiles Tutorial

This tutorial covers multi-profile authentication in scafctl. Profiles let you maintain multiple sets of credentials for the same auth handler -- for example, separate GitHub accounts for work and personal use, or different Entra tenants for staging and production.

## Prerequisites

- scafctl installed and available in your PATH
- At least one auth handler configured (see [Authentication Tutorial](../auth-tutorial/))
- A web browser for completing interactive login flows

## Table of Contents

1. [Why Profiles?](#why-profiles)
2. [Logging In with a Profile](#logging-in-with-a-profile)
3. [Checking Profile Status](#checking-profile-status)
4. [Switching the Active Profile](#switching-the-active-profile)
5. [Getting Tokens for a Profile](#getting-tokens-for-a-profile)
6. [Using Profiles in Solutions](#using-profiles-in-solutions)
7. [Global Profile Override](#global-profile-override)
8. [Environment Variable](#environment-variable)
9. [Profile Precedence](#profile-precedence)
10. [Configuration](#configuration)
11. [Logging Out of a Profile](#logging-out-of-a-profile)
12. [Different Flows Per Profile](#different-flows-per-profile)
13. [Putting It All Together](#putting-it-all-together)

---

## Why Profiles?

Without profiles, each auth handler stores a single set of credentials. This works until you need:

- **Multiple GitHub accounts** (personal vs. work organization)
- **Multiple Entra tenants** (dev vs. staging vs. production)
- **Multiple GCP projects** with different service accounts
- **CI/CD pipelines** that authenticate differently than local development

Profiles solve this by namespacing credentials under a profile name. Each profile stores its own tokens independently.

---

## Logging In with a Profile

Use the `--profile` flag to authenticate under a named profile:

{{< tabs "profiles-login" >}}
{{% tab "Bash" %}}
```bash
# Login to GitHub with a "work" profile
scafctl auth login github --profile work

# Login to GitHub with a "personal" profile (different scopes)
scafctl auth login github --profile personal --scope repo --scope read:org

# Login to Entra with a "staging" profile targeting a specific tenant
scafctl auth login entra --profile staging --tenant-id <staging-tenant-guid>

# Login to GCP with a "deploy" profile
scafctl auth login gcp --profile deploy --scope https://www.googleapis.com/auth/cloud-platform
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# Login to GitHub with a "work" profile
scafctl auth login github --profile work

# Login to GitHub with a "personal" profile (different scopes)
scafctl auth login github --profile personal --scope repo --scope read:org

# Login to Entra with a "staging" profile targeting a specific tenant
scafctl auth login entra --profile staging --tenant-id <staging-tenant-guid>

# Login to GCP with a "deploy" profile
scafctl auth login gcp --profile deploy --scope https://www.googleapis.com/auth/cloud-platform
```
{{% /tab %}}
{{< /tabs >}}

Profile names must be ASCII alphanumeric, hyphens, or underscores, and must start with an alphanumeric character. Examples: `work`, `personal`, `staging-eu`, `ci_deploy`.

> **Note:** Logging in without `--profile` uses the default (unnamed) profile. The default profile and named profiles are independent -- they don't share credentials.

---

## Checking Profile Status

Check the authentication status for a specific profile:

{{< tabs "profiles-status" >}}
{{% tab "Bash" %}}
```bash
# Check status for the "work" profile
scafctl auth status github --profile work

# Check status for the active profile (or built-in if none configured)
scafctl auth status github
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# Check status for the "work" profile
scafctl auth status github --profile work

# Check status for the active profile (or built-in if none configured)
scafctl auth status github
```
{{% /tab %}}
{{< /tabs >}}

---

## Switching the Active Profile

The active profile is used by default when no explicit profile is specified. Use `auth switch` to change it:

{{< tabs "profiles-switch" >}}
{{% tab "Bash" %}}
```bash
# List configured profiles for a handler (shows * next to active)
scafctl auth switch github

# Set "work" as the active profile for GitHub
scafctl auth switch github work

# Reset to the built-in (unnamed) profile
scafctl auth switch github built-in
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# List configured profiles for a handler (shows * next to active)
scafctl auth switch github

# Set "work" as the active profile for GitHub
scafctl auth switch github work

# Reset to the built-in (unnamed) profile
scafctl auth switch github built-in
```
{{% /tab %}}
{{< /tabs >}}

The active profile is persisted in your config file (`~/.config/scafctl/config.yaml`).

---

## Getting Tokens for a Profile

Retrieve an access token for a specific profile:

{{< tabs "profiles-token" >}}
{{% tab "Bash" %}}
```bash
# Get a token for the "work" profile
scafctl auth token github --profile work

# Get a token for the active profile (no flag needed)
scafctl auth token github

# Use in scripts
export GITHUB_TOKEN=$(scafctl auth token github --profile work)
curl -H "Authorization: Bearer $GITHUB_TOKEN" https://api.github.com/user
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# Get a token for the "work" profile
scafctl auth token github --profile work

# Get a token for the active profile (no flag needed)
scafctl auth token github

# Use in scripts
$env:GITHUB_TOKEN = scafctl auth token github --profile work
Invoke-RestMethod -Uri "https://api.github.com/user" -Headers @{ Authorization = "Bearer $env:GITHUB_TOKEN" }
```
{{% /tab %}}
{{< /tabs >}}

---

## Using Profiles in Solutions

In solution YAML files, reference a profile using the `handler@profile` syntax in the `auth` field:

```yaml
apiVersion: scafctl.dev/v1
kind: Solution
metadata:
  name: multi-account-example
spec:
  resolvers:
    work-repos:
      provider: http
      inputs:
        url: https://api.github.com/user/repos?type=owner
        authProvider: github@work
        method: GET

    personal-repos:
      provider: http
      inputs:
        url: https://api.github.com/user/repos?type=owner
        authProvider: github@personal
        method: GET

    staging-resources:
      provider: http
      inputs:
        url: https://management.azure.com/subscriptions?api-version=2022-12-01
        authProvider: entra@staging
        method: GET
```

When a solution uses `authProvider: github@work`, scafctl authenticates that specific request using the "work" profile's credentials, regardless of the active profile.

---

## Global Profile Override

The `--auth-profile` flag overrides the active profile for all auth operations in a single command:

{{< tabs "profiles-global" >}}
{{% tab "Bash" %}}
```bash
# Run a solution using the "work" profile for all auth
scafctl run solution -f ./my-solution.yaml --auth-profile work

# Check status across all handlers using the "staging" profile
scafctl --auth-profile staging auth status entra
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# Run a solution using the "work" profile for all auth
scafctl run solution -f ./my-solution.yaml --auth-profile work

# Check status across all handlers using the "staging" profile
scafctl --auth-profile staging auth status entra
```
{{% /tab %}}
{{< /tabs >}}

> **Note:** The `handler@profile` syntax in solution YAML takes precedence over `--auth-profile`. This lets solutions pin specific operations to specific profiles while allowing a general override.

---

## Environment Variable

Set `SCAFCTL_AUTH_PROFILE` to apply a profile across multiple commands without repeating the flag:

{{< tabs "profiles-env" >}}
{{% tab "Bash" %}}
```bash
# Set for the current shell session
export SCAFCTL_AUTH_PROFILE=work

scafctl auth status github    # uses "work" profile
scafctl auth token github     # uses "work" profile
scafctl run solution -f ./my-solution.yaml  # uses "work" for all auth

# Override per-command with the flag
scafctl --auth-profile personal auth token github  # uses "personal" despite env var
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# Set for the current shell session
$env:SCAFCTL_AUTH_PROFILE = "work"

scafctl auth status github    # uses "work" profile
scafctl auth token github     # uses "work" profile
scafctl run solution -f ./my-solution.yaml  # uses "work" for all auth

# Override per-command with the flag
scafctl --auth-profile personal auth token github  # uses "personal" despite env var
```
{{% /tab %}}
{{< /tabs >}}

---

## Profile Precedence

When multiple profile sources are set, scafctl resolves them in this order (highest wins):

| Priority | Source | Example |
|----------|--------|---------|
| 1 (highest) | Per-command `--profile` flag or solution `handler@profile` | `--profile work`, `authProvider: github@work` |
| 2 | Global `--auth-profile` flag | `--auth-profile work` |
| 3 | `SCAFCTL_AUTH_PROFILE` env var | `export SCAFCTL_AUTH_PROFILE=work` |
| 4 (lowest) | Config `activeProfile` | `scafctl auth switch github work` |

If none are set, the default (unnamed) profile is used.

---

## Configuration

Profiles can be pre-configured in `~/.config/scafctl/config.yaml` with per-profile overrides:

```yaml
auth:
  github:
    clientId: "Iv1.default-app-id"
    activeProfile: work
    profiles:
      work:
        clientId: "Iv1.work-org-app-id"
        hostname: "github.example.com"
        defaultScopes:
          - repo
          - read:org
      personal:
        clientId: "Iv1.personal-app-id"
        defaultScopes:
          - repo

  entra:
    tenantId: "common"
    activeProfile: production
    profiles:
      staging:
        tenantId: "staging-tenant-guid"
        defaultScopes:
          - https://graph.microsoft.com/.default
      production:
        tenantId: "prod-tenant-guid"
        defaultScopes:
          - https://graph.microsoft.com/.default

  gcp:
    profiles:
      deploy:
        project: "my-prod-project"
        impersonateServiceAccount: "deployer@my-prod-project.iam.gserviceaccount.com"
```

Profile configs inherit from the top-level handler config and override specific fields. In the example above, the `work` GitHub profile uses `clientId: Iv1.work-org-app-id` but would inherit any other top-level GitHub settings.

---

## Logging Out of a Profile

Remove credentials for a specific profile:

{{< tabs "profiles-logout" >}}
{{% tab "Bash" %}}
```bash
# Logout from the "work" profile
scafctl auth logout github --profile work

# Logout from the default (unnamed) profile
scafctl auth logout github
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# Logout from the "work" profile
scafctl auth logout github --profile work

# Logout from the default (unnamed) profile
scafctl auth logout github
```
{{% /tab %}}
{{< /tabs >}}

---

## Different Flows Per Profile

Each profile can use a different authentication flow. This lets you mix interactive logins with non-interactive credentials for different use cases.

### GitHub: Device Code + PAT

Use device code for your personal account and a PAT for CI/automation:

{{< tabs "profiles-flows-github" >}}
{{% tab "Bash" %}}
```bash
# Personal: interactive device code flow (opens browser)
scafctl auth login github

# Work: PAT from environment variable (non-interactive)
GITHUB_TOKEN=ghp_your_fine_grained_pat scafctl auth login github --profile work --flow pat

# CI: GitHub Actions token (non-interactive)
GITHUB_TOKEN=$GITHUB_TOKEN scafctl auth login github --profile ci --flow pat

# Check both profiles -- note the different flows
scafctl auth status github
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# Personal: interactive device code flow (opens browser)
scafctl auth login github

# Work: PAT from environment variable (non-interactive)
$env:GITHUB_TOKEN = "ghp_your_fine_grained_pat"
scafctl auth login github --profile work --flow pat

# CI: GitHub Actions token (non-interactive)
$env:GITHUB_TOKEN = $env:GITHUB_TOKEN
scafctl auth login github --profile ci --flow pat

# Check both profiles -- note the different flows
scafctl auth status github
```
{{% /tab %}}
{{< /tabs >}}

Example output:

```text
#  Handler  Kind  Status         Flow         User               Expires  Profile
1  github   auth  authenticated  device_code  abaker9@gmail.com           built-in
2  github   auth  authenticated  pat          abaker9@ford.com            work
```

> **Tip:** Fine-grained PATs let you scope access to specific repositories and permissions. Use them for profiles that only need access to a subset of your org's repos.

### Entra: Interactive + Service Principal

Use interactive login for development and a service principal for CI pipelines:

{{< tabs "profiles-flows-entra" >}}
{{% tab "Bash" %}}
```bash
# Dev: interactive browser login
scafctl auth login entra --profile dev

# CI: service principal (non-interactive)
AZURE_CLIENT_ID=<app-id> AZURE_CLIENT_SECRET=<secret> \
  scafctl auth login entra --profile ci --flow service-principal \
  --tenant-id <tenant-guid>
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# Dev: interactive browser login
scafctl auth login entra --profile dev

# CI: service principal (non-interactive)
$env:AZURE_CLIENT_ID = "<app-id>"
$env:AZURE_CLIENT_SECRET = "<secret>"
scafctl auth login entra --profile ci --flow service-principal `
  --tenant-id <tenant-guid>
```
{{% /tab %}}
{{< /tabs >}}

### GCP: Interactive + Service Account

Use gcloud ADC for development and a service account for deployment:

{{< tabs "profiles-flows-gcp" >}}
{{% tab "Bash" %}}
```bash
# Dev: use existing gcloud credentials
scafctl auth login gcp --flow gcloud-adc

# Deploy: service account key
GOOGLE_APPLICATION_CREDENTIALS=/path/to/sa-key.json \
  scafctl auth login gcp --profile deploy --flow service-principal
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# Dev: use existing gcloud credentials
scafctl auth login gcp --flow gcloud-adc

# Deploy: service account key
$env:GOOGLE_APPLICATION_CREDENTIALS = "C:\path\to\sa-key.json"
scafctl auth login gcp --profile deploy --flow service-principal
```
{{% /tab %}}
{{< /tabs >}}

### Using Flow-Specific Profiles in Solutions

Solutions can reference profiles that use different flows. The flow is determined at login time, not at solution execution time:

```yaml
apiVersion: scafctl.dev/v1
kind: Solution
metadata:
  name: cross-account-deploy
spec:
  resolvers:
    # Uses PAT profile -- scoped to org repos only
    org-repos:
      provider: http
      inputs:
        url: https://api.github.com/orgs/my-org/repos
        authProvider: github@work
        method: GET

    # Uses device-code profile -- broader personal access
    personal-repos:
      provider: http
      inputs:
        url: https://api.github.com/user/repos?type=owner
        authProvider: github
        method: GET

    # Uses service principal -- CI/CD credentials
    azure-resources:
      provider: http
      inputs:
        url: https://management.azure.com/subscriptions?api-version=2022-12-01
        authProvider: entra@ci
        method: GET
```

---

## Putting It All Together

Here's a typical workflow for a developer with separate work and personal GitHub accounts:

{{< tabs "profiles-workflow" >}}
{{% tab "Bash" %}}
```bash
# 1. Login with different flows per profile
scafctl auth login github                                    # built-in: device code
GITHUB_TOKEN=ghp_xxx scafctl auth login github --profile work --flow pat  # work: PAT

# 2. Check all profiles
scafctl auth status github
# => built-in (device_code, gmail), work (pat, ford)

# 3. Set "work" as the default
scafctl auth switch github work

# 4. Normal commands now use "work" automatically
scafctl run solution -f ./deploy.yaml   # authProvider: github -> uses "work" PAT

# 5. Override when needed (use built-in profile for personal backup)
scafctl --auth-profile built-in run solution -f ./personal-backup.yaml

# 6. Solutions can pin specific operations
# In YAML: authProvider: github@work -> always uses "work" PAT regardless of active profile
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# 1. Login with different flows per profile
scafctl auth login github                                    # built-in: device code
$env:GITHUB_TOKEN = "ghp_xxx"
scafctl auth login github --profile work --flow pat          # work: PAT

# 2. Check all profiles
scafctl auth status github
# => built-in (device_code, gmail), work (pat, ford)

# 3. Set "work" as the default
scafctl auth switch github work

# 4. Normal commands now use "work" automatically
scafctl run solution -f ./deploy.yaml   # authProvider: github -> uses "work" PAT

# 5. Override when needed (use built-in profile for personal backup)
scafctl --auth-profile built-in run solution -f ./personal-backup.yaml

# 6. Solutions can pin specific operations
# In YAML: authProvider: github@work -> always uses "work" PAT regardless of active profile
```
{{% /tab %}}
{{< /tabs >}}
