---
title: "Authentication Tutorial"
weight: 40
---

# Authentication Tutorial

This tutorial walks you through setting up and using authentication in scafctl. You'll learn how to authenticate with Microsoft Entra ID, GitHub, and Google Cloud Platform, manage credentials, and use authenticated HTTP requests in your solutions.

## Prerequisites

- scafctl installed and available in your PATH
- For Entra: Access to a Microsoft Entra ID tenant
- For GitHub: A GitHub account (or GitHub Enterprise Server instance)
- For GCP: A Google Cloud account
- A web browser for completing the device code flow

## Table of Contents

1. [Understanding Auth in scafctl](#understanding-auth-in-scafctl)
2. [Logging In](#logging-in)
   - [Entra Device Code Flow](#example-output)
   - [Entra Service Principal](#service-principal-authentication-cicd)
   - [Entra Workload Identity](#workload-identity-authentication-kubernetes)
   - [GitHub Device Code Flow](#github-device-code-flow)
   - [GitHub PAT Flow](#github-pat-authentication-cicd)
   - [GitHub Interactive Flow (Browser OAuth + PKCE)](#github-interactive-flow-browser-oauth--pkce)
   - [GitHub App Installation Token](#github-app-installation-token)
   - [GCP Interactive Login](#gcp-interactive-login-browser-oauth)
   - [GCP Service Account Key](#gcp-service-account-key-authentication-cicd)
   - [GCP Workload Identity Federation](#gcp-workload-identity-federation)
   - [GCP Metadata Server](#gcp-metadata-server-gcegkecloud-run)
3. [Checking Auth Status](#checking-auth-status)
4. [Listing and Sorting Cached Tokens](#listing-and-sorting-cached-tokens)
5. [Using Auth in HTTP Providers](#using-auth-in-http-providers)
6. [Getting Tokens for Debugging](#getting-tokens-for-debugging)
7. [Configuration](#configuration)
8. [Logging Out](#logging-out)
9. [Auth Diagnostics](#auth-diagnostics)
10. [Troubleshooting](#troubleshooting)

---

## Understanding Auth in scafctl

Authentication in scafctl follows these principles:

- **Providers declare auth requirements**, not credentials
- **Token acquisition is separated** from provider execution
- **Refresh tokens are stored securely** using your system's secret store
- **Access tokens are short-lived** and cached for performance
- **Secrets never appear** in solution files or logs
- **Auth tokens are visible** via `scafctl secrets list --all` and `scafctl secrets get <name> --all`

scafctl currently supports the following auth handlers:

| Handler | Description | Flows |
|---------|-------------|-------|
| `entra` | Microsoft Entra ID (Azure AD) | Interactive (Browser OAuth + PKCE), Device Code, Service Principal, Workload Identity |
| `github` | GitHub (github.com and GHES) | Interactive (Browser OAuth + PKCE), Device Code, PAT (Personal Access Token), GitHub App |
| `gcp` | Google Cloud Platform | Interactive (Browser OAuth), Service Account Key, Workload Identity Federation, Metadata Server |

You can always discover registered handlers and their capabilities at runtime:

{{< tabs "auth-tutorial-cmd-1" >}}
{{% tab "Bash" %}}
```bash
# List all handlers with flows and capabilities
scafctl auth list

# Output as JSON for scripting
scafctl auth list -o json
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# List all handlers with flows and capabilities
scafctl auth list

# Output as JSON for scripting
scafctl auth list -o json
```
{{% /tab %}}
{{< /tabs >}}

---

## Logging In

To authenticate with Microsoft Entra ID, use the `auth login` command:

{{< tabs "auth-tutorial-cmd-2" >}}
{{% tab "Bash" %}}
```bash
scafctl auth login entra
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl auth login entra
```
{{% /tab %}}
{{< /tabs >}}

By default, this opens your browser for an OAuth authorization code flow with PKCE -- the same approach used by `az login`, `gh auth login`, and `gcloud auth login`:

1. scafctl starts a local HTTP server on an ephemeral port
2. Your browser opens to the Microsoft login page
3. Sign in and grant consent
4. The browser redirects back to the local server with an authorization code
5. scafctl exchanges the code for tokens and stores your refresh token securely

### Example Output

```
→ Opening browser for authentication...
  If the browser does not open, visit: https://login.microsoftonline.com/...

✓ Successfully authenticated as user@example.com
  Tenant: contoso.onmicrosoft.com
```

### Device Code Flow (Headless / SSH Fallback)

If you are in a headless environment, over SSH, or the browser cannot open, use the device code flow:

{{< tabs "auth-tutorial-cmd-3" >}}
{{% tab "Bash" %}}
```bash
scafctl auth login entra --flow device-code
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl auth login entra --flow device-code
```
{{% /tab %}}
{{< /tabs >}}

This displays a code and URL for you to enter manually:

```
To sign in, use a web browser to open the page https://microsoft.com/devicelogin
and enter the code ABCD1234 to authenticate.

Waiting for authentication...

✓ Successfully authenticated as user@example.com
  Tenant: contoso.onmicrosoft.com
```

### Idempotent Login for Scripts (--skip-if-authenticated)

Use `--skip-if-authenticated` to skip re-authentication if you're already logged in. The command exits `0` without prompting, making it safe to call at the start of scripts or CI jobs without disrupting an active session:

{{< tabs "auth-tutorial-cmd-4" >}}
{{% tab "Bash" %}}
```bash
# Only login if not already authenticated
scafctl auth login entra --skip-if-authenticated
scafctl auth login github --skip-if-authenticated
scafctl auth login gcp --skip-if-authenticated
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# Only login if not already authenticated
scafctl auth login entra --skip-if-authenticated
scafctl auth login github --skip-if-authenticated
scafctl auth login gcp --skip-if-authenticated
```
{{% /tab %}}
{{< /tabs >}}

When already authenticated this prints a message and exits `0`. Without the flag, the command prompts for re-authentication (or warns if already authenticated but continues).

---

### Specifying a Tenant

By default, scafctl uses the "organizations" tenant (any work/school account). To authenticate with a specific tenant:

{{< tabs "auth-tutorial-cmd-5" >}}
{{% tab "Bash" %}}
```bash
# Use a specific tenant ID
scafctl auth login entra --tenant 08e70e8e-d05c-4449-a2c2-67bd0a9c4e79

# Use a tenant domain
scafctl auth login entra --tenant contoso.onmicrosoft.com
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# Use a specific tenant ID
scafctl auth login entra --tenant 08e70e8e-d05c-4449-a2c2-67bd0a9c4e79

# Use a tenant domain
scafctl auth login entra --tenant contoso.onmicrosoft.com
```
{{% /tab %}}
{{< /tabs >}}

### Custom Client ID

By default, scafctl uses the Azure CLI's public client ID (`04b07795-8ddb-461a-bbee-02f9e1bf7b46`). If your organization requires a custom app registration (e.g., for specific permissions or conditional access policies), use the `--client-id` flag:

{{< tabs "auth-tutorial-cmd-6" >}}
{{% tab "Bash" %}}
```bash
scafctl auth login entra --client-id 12345678-abcd-1234-abcd-123456789abc
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl auth login entra --client-id 12345678-abcd-1234-abcd-123456789abc
```
{{% /tab %}}
{{< /tabs >}}

The client ID used during login is persisted in your credential metadata so that subsequent token refreshes use the same client ID, even if your configuration file specifies a different one. This prevents token minting failures caused by a mismatch between the login client ID and the refresh client ID.

You can also set a default client ID via the scafctl configuration file under `auth.entra.clientId`. Note that the `--client-id` flag at login time always takes precedence, and the stored client ID from login will be used for all future token refreshes.

> [!WARNING]
> **Important -- Redirect URI registration:** When using a custom client ID with the
> interactive (browser) login flow, the app registration must have `http://localhost`
> registered as a redirect URI. Without it, Entra returns AADSTS500113 and the CLI
> times out. In the Azure portal, go to **App registrations → your app →
> Authentication → Add a platform → Mobile and desktop applications** and add
> `http://localhost`.
> 
> If you cannot modify the app registration, use `--flow device-code` instead
> (device code does not require a redirect URI), or use `--callback-port` to bind
> the callback server to a specific port that matches a registered redirect URI:
> 
> ```bash
> # If the app registration has http://localhost:8400 as a redirect URI
> scafctl auth login entra --client-id 12345678-abcd-1234-abcd-123456789abc --callback-port 8400
> ```

### Setting a Callback Port

By default, the interactive login flow starts a local HTTP server on a random
(ephemeral) port. Some app registrations only allow specific redirect URIs. Use
`--callback-port` to bind to a predictable port:

{{< tabs "auth-tutorial-cmd-7" >}}
{{% tab "Bash" %}}
```bash
scafctl auth login entra --callback-port 8400
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl auth login entra --callback-port 8400
```
{{% /tab %}}
{{< /tabs >}}

This makes the redirect URI `http://localhost:8400`, which must be registered in
the app registration's **Authentication** settings.

### Setting a Timeout

The interactive login flow has a 5-minute default timeout. To extend it:

{{< tabs "auth-tutorial-cmd-8" >}}
{{% tab "Bash" %}}
```bash
scafctl auth login entra --timeout 10m
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl auth login entra --timeout 10m
```
{{% /tab %}}
{{< /tabs >}}

### Requesting Specific Scopes

By default, login requests only basic scopes (`openid`, `profile`, `offline_access`). If your resolvers need access to specific APIs (e.g., Microsoft Graph), include the required scope during login to establish consent:

{{< tabs "auth-tutorial-cmd-9" >}}
{{% tab "Bash" %}}
```bash
# Login with Microsoft Graph scope
scafctl auth login entra --scope https://graph.microsoft.com/.default

# Login with Azure Resource Manager scope
scafctl auth login entra --scope https://management.azure.com/.default
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# Login with Microsoft Graph scope
scafctl auth login entra --scope https://graph.microsoft.com/.default

# Login with Azure Resource Manager scope
scafctl auth login entra --scope https://management.azure.com/.default
```
{{% /tab %}}
{{< /tabs >}}

This ensures your authentication session has consent for that API resource, preventing "consent required" errors when resolvers run. The refresh token obtained at login can then be used to mint access tokens for the consented resource.

> [!NOTE]
> **Note:** Login should target a single API resource at a time. If you need
> tokens for multiple API resources, use separate `scafctl auth login` calls,
> or rely on the refresh token to mint tokens for additional resources at
> runtime via `scafctl auth token`.

### Service Principal Authentication (CI/CD)

For non-interactive scenarios like CI/CD pipelines, use service principal authentication:

{{< tabs "auth-tutorial-cmd-10" >}}
{{% tab "Bash" %}}
```bash
# Set credentials in environment variables
export AZURE_CLIENT_ID="your-app-client-id"
export AZURE_TENANT_ID="your-tenant-id"
export AZURE_CLIENT_SECRET="your-client-secret"

# Login with service principal (auto-detected from env vars)
scafctl auth login entra

# Or explicitly specify the flow
scafctl auth login entra --flow service-principal
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# Set credentials in environment variables
$env:AZURE_CLIENT_ID = "your-app-client-id"
$env:AZURE_TENANT_ID = "your-tenant-id"
$env:AZURE_CLIENT_SECRET = "your-client-secret"

# Login with service principal (auto-detected from env vars)
scafctl auth login entra

# Or explicitly specify the flow
scafctl auth login entra --flow service-principal
```
{{% /tab %}}
{{< /tabs >}}

**Environment Variables:**

| Variable | Description |
|----------|-------------|
| `AZURE_CLIENT_ID` | Application (client) ID of the service principal |
| `AZURE_TENANT_ID` | Directory (tenant) ID |
| `AZURE_CLIENT_SECRET` | Client secret value |

**Note:** When `AZURE_CLIENT_SECRET` is set, scafctl automatically uses the service principal flow.

### Workload Identity Authentication (Kubernetes)

Workload Identity enables secretless authentication for workloads running on Kubernetes. This is the recommended approach for AKS and other Kubernetes environments as it eliminates the need to manage secrets.

#### Quick Start (In-Cluster)

When running inside a properly configured AKS pod, the Azure Workload Identity webhook automatically injects the required environment variables:

{{< tabs "auth-tutorial-cmd-11" >}}
{{% tab "Bash" %}}
```bash
# Auto-detected when running in a configured pod
scafctl auth login entra

# Or explicitly specify the flow
scafctl auth login entra --flow workload-identity
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# Auto-detected when running in a configured pod
scafctl auth login entra

# Or explicitly specify the flow
scafctl auth login entra --flow workload-identity
```
{{% /tab %}}
{{< /tabs >}}

**Environment Variables (auto-injected by webhook):**

| Variable | Description |
|----------|-------------|
| `AZURE_CLIENT_ID` | Client ID of the managed identity or app registration |
| `AZURE_TENANT_ID` | Directory (tenant) ID |
| `AZURE_FEDERATED_TOKEN_FILE` | Path to the projected service account token |
| `AZURE_AUTHORITY_HOST` | (Optional) Azure AD authority URL |

#### Testing Workload Identity Locally

For development and testing outside of AKS, you can manually configure workload identity federation. This involves:

1. Creating an Entra App Registration with federated credentials
2. Generating a Kubernetes service account token
3. Using the token with scafctl

##### Step 1: Create an Entra App Registration

1. **Register a new application** in the [Azure Portal](https://portal.azure.com/#view/Microsoft_AAD_RegisteredApps/ApplicationsListBlade):

{{< tabs "auth-tutorial-cmd-12" >}}
{{% tab "Bash" %}}
```bash
   # Using Azure CLI
   az ad app create --display-name "scafctl-workload-identity-test"
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
   # Using Azure CLI
   az ad app create --display-name "scafctl-workload-identity-test"
```
{{% /tab %}}
{{< /tabs >}}

2. **Note the Application (client) ID** - you'll need this for `AZURE_CLIENT_ID`

3. **Create a service principal** for the application:

   ```bash
   az ad sp create --id <application-id>
   ```

4. **Grant API permissions** as needed (e.g., Microsoft Graph, Azure Resource Manager)

##### Step 2: Configure Federated Identity Credential

The federated identity credential tells Entra ID to trust tokens from your Kubernetes cluster's OIDC issuer.

1. **Get your Kubernetes cluster's OIDC issuer URL**:

   {{< tabs "auth-oidc-issuer" >}}
   {{% tab "Bash" %}}
   ```bash
   # For AKS
   az aks show --name <cluster-name> --resource-group <rg-name> \
     --query "oidcIssuerProfile.issuerUrl" -o tsv

   # For other clusters (e.g., kind, minikube with OIDC enabled)
   kubectl get --raw /.well-known/openid-configuration | jq -r '.issuer'
   ```
   {{% /tab %}}
   {{% tab "PowerShell" %}}
   ```powershell
   az aks show --name <cluster-name> --resource-group <rg-name> `
     --query "oidcIssuerProfile.issuerUrl" -o tsv

   # For other clusters
   (kubectl get --raw /.well-known/openid-configuration | ConvertFrom-Json).issuer
   ```
   {{% /tab %}}
   {{< /tabs >}}

2. **Create the federated credential** via Azure Portal or CLI:

   ```bash
   # Using Azure CLI
   az ad app federated-credential create --id <application-id> --parameters '{
     "name": "kubernetes-federated-credential",
     "issuer": "<your-oidc-issuer-url>",
     "subject": "system:serviceaccount:<namespace>:<service-account-name>",
     "audiences": ["api://AzureADTokenExchange"]
   }'
   ```

   **Important fields:**
   - `issuer`: Your Kubernetes cluster's OIDC issuer URL
   - `subject`: Must match the service account in format `system:serviceaccount:<namespace>:<name>`
   - `audiences`: Must be `["api://AzureADTokenExchange"]` for Azure workload identity

   **Example for a service account named `scafctl-sa` in the `default` namespace:**

{{< tabs "auth-tutorial-cmd-13" >}}
{{% tab "Bash" %}}
```bash
   az ad app federated-credential create --id <application-id> --parameters '{
     "name": "scafctl-test-credential",
     "issuer": "https://oidc.example.com/abc123",
     "subject": "system:serviceaccount:default:scafctl-sa",
     "audiences": ["api://AzureADTokenExchange"]
   }'
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
   az ad app federated-credential create --id <application-id> --parameters '{
     "name": "scafctl-test-credential",
     "issuer": "https://oidc.example.com/abc123",
     "subject": "system:serviceaccount:default:scafctl-sa",
     "audiences": ["api://AzureADTokenExchange"]
   }'
```
{{% /tab %}}
{{< /tabs >}}

##### Step 3: Create a Kubernetes Service Account and Generate Token

1. **Create a service account** (if it doesn't exist):

   ```yaml
   # service-account.yaml
   apiVersion: v1
   kind: ServiceAccount
   metadata:
     name: scafctl-sa
     namespace: default
   ```

   ```bash
   kubectl apply -f service-account.yaml
   ```

2. **Generate a token** using `kubectl create token`:

{{< tabs "auth-tutorial-cmd-14" >}}
{{% tab "Bash" %}}
```bash
   # Generate a token with the correct audience
   kubectl create token scafctl-sa \
     --namespace default \
     --audience "api://AzureADTokenExchange" \
     --duration 1h
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
   # Generate a token with the correct audience
   kubectl create token scafctl-sa `
     --namespace default `
     --audience "api://AzureADTokenExchange" `
     --duration 1h
```
{{% /tab %}}
{{< /tabs >}}

   **Important:** The `--audience` must match what you configured in the federated credential.

3. **Save the token** to an environment variable or file:

{{< tabs "auth-tutorial-cmd-15" >}}
{{% tab "Bash" %}}
```bash
   # Save to environment variable
   export FEDERATED_TOKEN=$(kubectl create token scafctl-sa \
     --namespace default \
     --audience "api://AzureADTokenExchange" \
     --duration 1h)

   # Or save to a file
   kubectl create token scafctl-sa \
     --namespace default \
     --audience "api://AzureADTokenExchange" \
     --duration 1h > /tmp/federated-token.txt
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
   # Save to environment variable
   $env:FEDERATED_TOKEN = $(kubectl create token scafctl-sa `
     --namespace default `
     --audience "api://AzureADTokenExchange" `
     --duration 1h)

   # Or save to a file
   kubectl create token scafctl-sa `
     --namespace default `
     --audience "api://AzureADTokenExchange" `
     --duration 1h > /tmp/federated-token.txt
```
{{% /tab %}}
{{< /tabs >}}

##### Step 4: Authenticate with scafctl

**Option A: Using the `--federated-token` flag (recommended for testing):**

{{< tabs "auth-tutorial-cmd-16" >}}
{{% tab "Bash" %}}
```bash
export AZURE_CLIENT_ID="<your-application-client-id>"
export AZURE_TENANT_ID="<your-tenant-id>"

# Pass the token directly
scafctl auth login entra --flow workload-identity --federated-token "$FEDERATED_TOKEN"
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
$env:AZURE_CLIENT_ID = "<your-application-client-id>"
$env:AZURE_TENANT_ID = "<your-tenant-id>"

# Pass the token directly
scafctl auth login entra --flow workload-identity --federated-token "$FEDERATED_TOKEN"
```
{{% /tab %}}
{{< /tabs >}}

**Option B: Using the `AZURE_FEDERATED_TOKEN` environment variable:**

{{< tabs "auth-tutorial-cmd-17" >}}
{{% tab "Bash" %}}
```bash
export AZURE_CLIENT_ID="<your-application-client-id>"
export AZURE_TENANT_ID="<your-tenant-id>"
export AZURE_FEDERATED_TOKEN="$FEDERATED_TOKEN"

scafctl auth login entra --flow workload-identity
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
$env:AZURE_CLIENT_ID = "<your-application-client-id>"
$env:AZURE_TENANT_ID = "<your-tenant-id>"
$env:AZURE_FEDERATED_TOKEN = "$FEDERATED_TOKEN"

scafctl auth login entra --flow workload-identity
```
{{% /tab %}}
{{< /tabs >}}

**Option C: Using a token file (simulates in-cluster behavior):**

{{< tabs "auth-tutorial-cmd-18" >}}
{{% tab "Bash" %}}
```bash
export AZURE_CLIENT_ID="<your-application-client-id>"
export AZURE_TENANT_ID="<your-tenant-id>"
export AZURE_FEDERATED_TOKEN_FILE="/tmp/federated-token.txt"

scafctl auth login entra --flow workload-identity
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
$env:AZURE_CLIENT_ID = "<your-application-client-id>"
$env:AZURE_TENANT_ID = "<your-tenant-id>"
$env:AZURE_FEDERATED_TOKEN_FILE = "/tmp/federated-token.txt"

scafctl auth login entra --flow workload-identity
```
{{% /tab %}}
{{< /tabs >}}

##### Step 5: Verify Authentication

{{< tabs "auth-tutorial-cmd-19" >}}
{{% tab "Bash" %}}
```bash
# Check auth status
scafctl auth status entra

# Output for workload identity:
# Handler   Status         Identity                          IdentityType         TokenFile
# entra     Authenticated  Workload Identity (12345678...)   workload-identity    (direct token)
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# Check auth status
scafctl auth status entra

# Output for workload identity:
# Handler   Status         Identity                          IdentityType         TokenFile
# entra     Authenticated  Workload Identity (12345678...)   workload-identity    (direct token)
```
{{% /tab %}}
{{< /tabs >}}

##### Complete Example Script

Here's a complete script for testing workload identity locally:

{{< tabs "auth-tutorial-cmd-20" >}}
{{% tab "Bash" %}}
```bash
#!/bin/bash
set -e

# Configuration - update these values
APP_CLIENT_ID="12345678-1234-1234-1234-123456789012"
TENANT_ID="your-tenant-id"
NAMESPACE="default"
SERVICE_ACCOUNT="scafctl-sa"

# Generate a fresh token
echo "Generating service account token..."
FEDERATED_TOKEN=$(kubectl create token "$SERVICE_ACCOUNT" \
  --namespace "$NAMESPACE" \
  --audience "api://AzureADTokenExchange" \
  --duration 1h)

# Set environment variables
export AZURE_CLIENT_ID="$APP_CLIENT_ID"
export AZURE_TENANT_ID="$TENANT_ID"

# Authenticate
echo "Authenticating with workload identity..."
scafctl auth login entra --flow workload-identity --federated-token "$FEDERATED_TOKEN"

# Verify
echo ""
echo "Authentication status:"
scafctl auth status entra

# Test token acquisition
echo ""
echo "Getting access token for Azure Resource Manager..."
scafctl auth token entra --scope "https://management.azure.com/.default"
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
$ErrorActionPreference = "Stop"

# Configuration - update these values
$APP_CLIENT_ID = "12345678-1234-1234-1234-123456789012"
$TENANT_ID = "your-tenant-id"
$NAMESPACE = "default"
$SERVICE_ACCOUNT = "scafctl-sa"

# Generate a fresh token
Write-Output "Generating service account token..."
$FEDERATED_TOKEN = $(kubectl create token $SERVICE_ACCOUNT `
  --namespace $NAMESPACE `
  --audience "api://AzureADTokenExchange" `
  --duration 1h)

# Set environment variables
$env:AZURE_CLIENT_ID = $APP_CLIENT_ID
$env:AZURE_TENANT_ID = $TENANT_ID

# Authenticate
Write-Output "Authenticating with workload identity..."
scafctl auth login entra --flow workload-identity --federated-token $FEDERATED_TOKEN

# Verify
Write-Output ""
Write-Output "Authentication status:"
scafctl auth status entra

# Test token acquisition
Write-Output ""
Write-Output "Getting access token for Azure Resource Manager..."
scafctl auth token entra --scope "https://management.azure.com/.default"
```
{{% /tab %}}
{{< /tabs >}}

##### Flow Priority and Interaction with Stored Credentials

The Entra handler selects which flow to use based on what is available at runtime, in this order:

| Priority | Flow | What triggers it |
|----------|------|------------------|
| 1 (highest) | Workload Identity | `AZURE_FEDERATED_TOKEN_FILE` or `AZURE_FEDERATED_TOKEN` is set |
| 2 | Service Principal | `AZURE_CLIENT_SECRET` is set |
| 3 (lowest) | Device Code / Refresh Token | A refresh token is stored in the system secret store |

**WIF does not touch the stored refresh token.**

When WIF is active, the stored device-code refresh token (if any) is bypassed but **not deleted**. The two credential types live in completely separate storage:

- WIF is entirely env-var driven -- no reads or writes to `scafctl.auth.entra.refresh_token`
- A prior device-code session silently coexists in the secret store while WIF is active
- `scafctl auth list` will display both the WIF-sourced access tokens and any stored refresh token

**Fallback behavior**: removing WIF env vars causes the handler to automatically fall through to the next available flow. If you have a stored refresh token, it resumes being used with no reconfiguration needed.

**Stale stored credentials**: if you want a clean slate after switching to WIF, explicitly remove the stored device-code session:

{{< tabs "auth-tutorial-cmd-21" >}}
{{% tab "Bash" %}}
```bash
scafctl auth logout entra
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl auth logout entra
```
{{% /tab %}}
{{< /tabs >}}

This removes the refresh token and cached access tokens without affecting WIF, which is entirely driven by environment variables.

---

##### Troubleshooting Workload Identity

**Error: "AADSTS70021: No matching federated identity record found"**

This means the token's claims don't match the federated credential configuration:
- Verify the `issuer` matches your cluster's OIDC issuer URL exactly
- Verify the `subject` matches your service account (`system:serviceaccount:<namespace>:<name>`)
- Verify the `audience` in both the federated credential and `kubectl create token` command

{{< tabs "auth-decode-token" >}}
{{% tab "Bash" %}}
```bash
# Decode the token to inspect its claims
echo "$FEDERATED_TOKEN" | cut -d. -f2 | base64 -d 2>/dev/null | jq .
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# Decode the token to inspect its claims
$parts = $env:FEDERATED_TOKEN -split '\.'
[System.Text.Encoding]::UTF8.GetString([Convert]::FromBase64String($parts[1] + '=' * (4 - $parts[1].Length % 4))) | ConvertFrom-Json
```
{{% /tab %}}
{{< /tabs >}}

Check these claims match your federated credential:
- `iss` (issuer)
- `sub` (subject)
- `aud` (audience)

**Error: "AADSTS700024: Client assertion is not within its valid time range"**

The token has expired. Generate a new one:

{{< tabs "auth-tutorial-cmd-22" >}}
{{% tab "Bash" %}}
```bash
kubectl create token scafctl-sa --audience "api://AzureADTokenExchange" --duration 1h
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
kubectl create token scafctl-sa --audience "api://AzureADTokenExchange" --duration 1h
```
{{% /tab %}}
{{< /tabs >}}

**Error: "workload identity not configured"**

Ensure all required environment variables are set:

```bash
echo "AZURE_CLIENT_ID: $AZURE_CLIENT_ID"
echo "AZURE_TENANT_ID: $AZURE_TENANT_ID"
echo "AZURE_FEDERATED_TOKEN: ${AZURE_FEDERATED_TOKEN:0:20}..." # First 20 chars
```

**Checking the OIDC Discovery Document**

Verify your cluster's OIDC configuration is accessible:

{{< tabs "auth-oidc-discovery" >}}
{{% tab "Bash" %}}
```bash
# Get the OIDC configuration
curl -s "$(kubectl get --raw /.well-known/openid-configuration | jq -r '.issuer')/.well-known/openid-configuration" | jq .

# Get the JWKS (signing keys)
curl -s "$(kubectl get --raw /.well-known/openid-configuration | jq -r '.jwks_uri')" | jq .
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
$issuer = (kubectl get --raw /.well-known/openid-configuration | ConvertFrom-Json).issuer
Invoke-RestMethod "$issuer/.well-known/openid-configuration"
```
{{% /tab %}}
{{< /tabs >}}

---

## GitHub Interactive Flow (Browser OAuth + PKCE)

The default GitHub login flow opens your browser for OAuth Authorization Code + PKCE authentication -- the same approach used by `gh auth login` and the Entra handler:

{{< tabs "auth-tutorial-cmd-23" >}}
{{% tab "Bash" %}}
```bash
scafctl auth login github
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl auth login github
```
{{% /tab %}}
{{< /tabs >}}

This initiates a browser-based login:

1. scafctl starts a local HTTP server on an ephemeral port
2. Your browser opens to the GitHub authorization page
3. Authorize the scafctl OAuth App
4. GitHub redirects back to the local server with an authorization code
5. scafctl exchanges the code for tokens using PKCE verification
6. Credentials are stored securely

### Example Output

```
→ Opening browser for authentication...
  If the browser does not open, visit: https://github.com/login/oauth/authorize?...

✓ Authentication successful!
  Name:     The Octocat
  Username: octocat
  Email:    octocat@github.com
  Flow:     Interactive
```

### Setting a Callback Port

By default, the interactive flow starts a local HTTP server on a random (ephemeral) port. Use `--callback-port` to bind to a predictable port:

{{< tabs "auth-tutorial-cmd-24" >}}
{{% tab "Bash" %}}
```bash
scafctl auth login github --callback-port 8400
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl auth login github --callback-port 8400
```
{{% /tab %}}
{{< /tabs >}}

---

## GitHub Device Code Flow

For headless environments, SSH sessions, or when the browser cannot open, use the device code flow:

{{< tabs "auth-tutorial-cmd-25" >}}
{{% tab "Bash" %}}
```bash
scafctl auth login github --flow device-code
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl auth login github --flow device-code
```
{{% /tab %}}
{{< /tabs >}}

1. scafctl displays a code and URL
2. Open the URL in your browser
3. Enter the code when prompted
4. Authorize the scafctl OAuth App
5. scafctl stores your credentials securely

### Example Output

```
To sign in, use a web browser to open the page:
  https://github.com/login/device

Enter the code: ABCD-1234

Waiting for authentication...

✓ Authentication successful!
  Name:     The Octocat
  Username: octocat
  Email:    octocat@github.com
  Flow:     Device Code
```

### GitHub Enterprise Server

To authenticate with a GitHub Enterprise Server instance:

{{< tabs "auth-tutorial-cmd-26" >}}
{{% tab "Bash" %}}
```bash
scafctl auth login github --hostname github.example.com
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl auth login github --hostname github.example.com
```
{{% /tab %}}
{{< /tabs >}}

This adjusts all OAuth and API endpoints to use your GHES instance.

### Custom Client ID

By default, scafctl uses its own OAuth App client ID (`Ov23li6xn492GhPmt4YG`). If your organization requires a custom OAuth App:

{{< tabs "auth-tutorial-cmd-27" >}}
{{% tab "Bash" %}}
```bash
scafctl auth login github --client-id your-custom-client-id
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl auth login github --client-id your-custom-client-id
```
{{% /tab %}}
{{< /tabs >}}

### Requesting Specific Scopes

By default, login requests `gist`, `read:org`, `repo`, and `workflow` scopes (matching the `gh` CLI). To request different scopes:

{{< tabs "auth-tutorial-cmd-28" >}}
{{% tab "Bash" %}}
```bash
# Login with additional scopes
scafctl auth login github --scope repo --scope read:org --scope write:packages

# Or comma-separated
scafctl auth login github --scope "repo,read:org,write:packages"
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# Login with additional scopes
scafctl auth login github --scope repo --scope read:org --scope write:packages

# Or comma-separated
scafctl auth login github --scope "repo,read:org,write:packages"
```
{{% /tab %}}
{{< /tabs >}}

> [!WARNING]
> **Important:** GitHub scopes are fixed at login time. Unlike Entra ID, GitHub's
> OAuth token refresh does not support changing scopes per-request. The `--scope`
> flag on `scafctl auth token github` is not supported. If you need different
> scopes, you must log out and log in again with the desired scopes.

### Setting a Timeout

The device code flow has a 5-minute default timeout. To extend it:

{{< tabs "auth-tutorial-cmd-29" >}}
{{% tab "Bash" %}}
```bash
scafctl auth login github --timeout 10m
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl auth login github --timeout 10m
```
{{% /tab %}}
{{< /tabs >}}

---

## GitHub PAT Authentication (CI/CD)

For non-interactive scenarios like CI/CD pipelines, use a Personal Access Token:

{{< tabs "auth-tutorial-cmd-30" >}}
{{% tab "Bash" %}}
```bash
# Set token in environment (GITHUB_TOKEN takes precedence)
export GITHUB_TOKEN="ghp_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"

# Login with PAT (auto-detected from env vars)
scafctl auth login github

# Or explicitly specify the flow
scafctl auth login github --flow pat
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# Set token in environment (GITHUB_TOKEN takes precedence)
$env:GITHUB_TOKEN = "ghp_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"

# Login with PAT (auto-detected from env vars)
scafctl auth login github

# Or explicitly specify the flow
scafctl auth login github --flow pat
```
{{% /tab %}}
{{< /tabs >}}

**Environment Variables:**

| Variable | Description | Priority |
|----------|-------------|----------|
| `GITHUB_TOKEN` | GitHub personal access token or Actions token | 1 (highest) |
| `GH_TOKEN` | GitHub personal access token (gh CLI convention) | 2 |
| `GH_HOST` | GitHub hostname for Enterprise Server | -- |

**Notes:**
- In GitHub Actions, `GITHUB_TOKEN` is automatically injected
- When either token env var is set, scafctl automatically uses the PAT flow
- PATs don't have a defined expiry, so status shows as authenticated until the token is revoked
- The PAT identity type shows as `service-principal` since it acts like a non-interactive credential

---

## GitHub App Installation Token

For service-to-service automation and CI/CD without user tokens, use a GitHub App installation token. This is the recommended approach for automated workflows that need repository access.

### Prerequisites

1. Create a GitHub App in your organization or account
2. Generate a private key for the App
3. Install the App on the target repository/organization
4. Note the App ID and Installation ID

### Configuration

Set the GitHub App credentials via config file, environment variables, or a combination:

**Config file (`~/.config/scafctl/config.yaml`):**

```yaml
auth:
  github:
    appId: "12345"
    installationId: "67890"
    privateKeyPath: "/path/to/private-key.pem"
```

**Environment variables:**

```bash
export SCAFCTL_GITHUB_APP_ID="12345"
export SCAFCTL_GITHUB_APP_INSTALLATION_ID="67890"
export SCAFCTL_GITHUB_APP_PRIVATE_KEY_PATH="/path/to/private-key.pem"
```

### Login

{{< tabs "auth-tutorial-cmd-31" >}}
{{% tab "Bash" %}}
```bash
# Login with GitHub App (requires App credentials configured)
scafctl auth login github --flow github-app
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# Login with GitHub App (requires App credentials configured)
scafctl auth login github --flow github-app
```
{{% /tab %}}
{{< /tabs >}}

### Example Output

```
✓ Authentication successful!
  App:             my-automation-app
  Installation ID: 67890
  Flow:            GitHub App
  Identity Type:   service-principal
```

### Private Key Sources

The private key can be provided from multiple sources (checked in priority order):

| Source | Config Field | Env Var | Security |
|--------|-------------|---------|----------|
| Inline PEM | `privateKey` | `SCAFCTL_GITHUB_APP_PRIVATE_KEY` | ⚠️ Least secure -- key visible in config/env |
| File path | `privateKeyPath` | `SCAFCTL_GITHUB_APP_PRIVATE_KEY_PATH` | ✅ Good -- key in a file with restricted permissions |
| Secret store | `privateKeySecretName` | -- | ✅ Best -- key encrypted by OS keychain |

> [!WARNING]
> **Security recommendation:** Prefer `privateKeySecretName` (encrypted secret store) or `privateKeyPath` (file with `chmod 600`) over inline `privateKey`. When the inline method is used, scafctl logs a warning recommending a more secure alternative.

**Inline PEM (CI/CD secret):**

{{< tabs "auth-tutorial-cmd-32" >}}
{{% tab "Bash" %}}
```bash
export SCAFCTL_GITHUB_APP_PRIVATE_KEY="-----BEGIN RSA PRIVATE KEY-----
MIIEpAIBAAKCAQEA...
-----END RSA PRIVATE KEY-----"
scafctl auth login github --flow github-app
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
$env:SCAFCTL_GITHUB_APP_PRIVATE_KEY = ""-----BEGIN"
MIIEpAIBAAKCAQEA...
-----END RSA PRIVATE KEY-----"
scafctl auth login github --flow github-app
```
{{% /tab %}}
{{< /tabs >}}

**File path:**

{{< tabs "auth-tutorial-cmd-33" >}}
{{% tab "Bash" %}}
```bash
export SCAFCTL_GITHUB_APP_PRIVATE_KEY_PATH="/path/to/private-key.pem"
scafctl auth login github --flow github-app
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
$env:SCAFCTL_GITHUB_APP_PRIVATE_KEY_PATH = "/path/to/private-key.pem"
scafctl auth login github --flow github-app
```
{{% /tab %}}
{{< /tabs >}}

**Secret store:**

```yaml
# config.yaml
auth:
  github:
    appId: "12345"
    installationId: "67890"
    privateKeySecretName: "github-app-private-key"
```

### GitHub Enterprise Server

The GitHub App flow works with GHES by setting `--hostname`:

{{< tabs "auth-tutorial-cmd-34" >}}
{{% tab "Bash" %}}
```bash
scafctl auth login github --flow github-app --hostname github.example.com
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl auth login github --flow github-app --hostname github.example.com
```
{{% /tab %}}
{{< /tabs >}}

### Notes
- Installation tokens expire after 1 hour and are automatically cached
- The identity type is `service-principal` (no user context)
- The App must be installed on the target organization/repository
- Both PKCS#1 (`BEGIN RSA PRIVATE KEY`) and PKCS#8 (`BEGIN PRIVATE KEY`) PEM formats are supported

---

## GCP Interactive Login (Browser OAuth)

For local development, use the interactive browser OAuth flow:

{{< tabs "auth-tutorial-cmd-35" >}}
{{% tab "Bash" %}}
```bash
# Login with GCP using browser OAuth (default -- no gcloud required)
scafctl auth login gcp

# Login with specific scopes
scafctl auth login gcp --scope https://www.googleapis.com/auth/bigquery

# Login with service account impersonation
scafctl auth login gcp --impersonate-service-account my-sa@my-project.iam.gserviceaccount.com

# Login with a custom OAuth client ID (overrides the built-in default)
scafctl auth login gcp --client-id YOUR_CLIENT_ID
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# Login with GCP using browser OAuth (default -- no gcloud required)
scafctl auth login gcp

# Login with specific scopes
scafctl auth login gcp --scope https://www.googleapis.com/auth/bigquery

# Login with service account impersonation
scafctl auth login gcp --impersonate-service-account my-sa@my-project.iam.gserviceaccount.com

# Login with a custom OAuth client ID (overrides the built-in default)
scafctl auth login gcp --client-id YOUR_CLIENT_ID
```
{{% /tab %}}
{{< /tabs >}}

This will:
1. Start a local HTTP server
2. Open your browser to Google's OAuth consent page
3. Exchange the authorization code for refresh + access tokens
4. Store the refresh token in your system's secret store

> [!NOTE]
> **Note:** scafctl uses Google's well-known ADC client credentials by default -- the same ones used by `gcloud auth application-default login`. No gcloud installation is required. To use a custom OAuth client, see [GCP Custom OAuth Client Setup](gcp-custom-oauth-tutorial.md).

## GCP gcloud ADC Fallback

If you already have gcloud configured and prefer to use its existing credentials:

{{< tabs "auth-tutorial-cmd-36" >}}
{{% tab "Bash" %}}
```bash
# Use existing gcloud Application Default Credentials
scafctl auth login gcp --flow gcloud-adc
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# Use existing gcloud Application Default Credentials
scafctl auth login gcp --flow gcloud-adc
```
{{% /tab %}}
{{< /tabs >}}

This reads the refresh token from `~/.config/gcloud/application_default_credentials.json` (produced by `gcloud auth application-default login`). Note that this flow is subject to your organization's RAPT re-authentication policies.

## GCP Service Account Key Authentication (CI/CD)

For non-interactive scenarios, use a service account key file:

{{< tabs "auth-tutorial-cmd-37" >}}
{{% tab "Bash" %}}
```bash
# Point to your service account key JSON file
export GOOGLE_APPLICATION_CREDENTIALS="/path/to/sa-key.json"

# Login with service account (auto-detected from env var)
scafctl auth login gcp

# Or choose the flow explicitly
scafctl auth login gcp --flow service-principal
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# Point to your service account key JSON file
$env:GOOGLE_APPLICATION_CREDENTIALS = "/path/to/sa-key.json"

# Login with service account (auto-detected from env var)
scafctl auth login gcp

# Or choose the flow explicitly
scafctl auth login gcp --flow service-principal
```
{{% /tab %}}
{{< /tabs >}}

## GCP Workload Identity Federation

For Kubernetes and other cloud platforms:

{{< tabs "auth-tutorial-cmd-38" >}}
{{% tab "Bash" %}}
```bash
# Point to external account JSON config
export GOOGLE_EXTERNAL_ACCOUNT="/path/to/external-account.json"

# Login (auto-detected)
scafctl auth login gcp

# Or explicitly
scafctl auth login gcp --flow workload-identity
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# Point to external account JSON config
$env:GOOGLE_EXTERNAL_ACCOUNT = "/path/to/external-account.json"

# Login (auto-detected)
scafctl auth login gcp

# Or explicitly
scafctl auth login gcp --flow workload-identity
```
{{% /tab %}}
{{< /tabs >}}

## GCP Metadata Server (GCE/GKE/Cloud Run)

On Google Compute Engine, GKE, and Cloud Run:

{{< tabs "auth-tutorial-cmd-39" >}}
{{% tab "Bash" %}}
```bash
# Login via metadata server (auto-detected on GCE)
scafctl auth login gcp --flow metadata
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# Login via metadata server (auto-detected on GCE)
scafctl auth login gcp --flow metadata
```
{{% /tab %}}
{{< /tabs >}}

---

## Checking Auth Status

To see your current authentication status:

{{< tabs "auth-tutorial-cmd-40" >}}
{{% tab "Bash" %}}
```bash
# Show status for all handlers
scafctl auth status

# Show status for a specific handler
scafctl auth status entra

# Show GitHub auth status
scafctl auth status github

# Output as JSON
scafctl auth status -o json
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# Show status for all handlers
scafctl auth status

# Show status for a specific handler
scafctl auth status entra

# Show GitHub auth status
scafctl auth status github

# Output as JSON
scafctl auth status -o json
```
{{% /tab %}}
{{< /tabs >}}

### Example Output

**Device Code (User) Authentication:**
```
Handler   Status         Identity                Tenant                     Expires
entra     Authenticated  user@example.com        contoso.onmicrosoft.com    2026-05-04 15:30:00
```

**Service Principal Authentication:**
```
Handler   Status         Identity                          IdentityType        Tenant       ClientId
entra     Authenticated  Service Principal (12345678...)   service-principal   tenant-id    12345678-1234-...
```

**GitHub Device Code Authentication:**
```
Handler   Status         Identity   Username   Hostname      Scopes
github    Authenticated  octocat    octocat    github.com    gist, read:org, repo, workflow
```

**GitHub PAT Authentication:**
```
Handler   Status         Identity   Username   IdentityType        Scopes
github    Authenticated  octocat    octocat    service-principal   gist, read:org, repo, workflow
```

When not authenticated, the `hint` field tells you the exact command to run:

```
Handler   Status            Identity   Tenant   Expires   Hint
entra     Not Authenticated -          -        -         run 'scafctl auth login entra' to authenticate
github    Not Authenticated -          -        -         run 'scafctl auth login github' to authenticate
gcp       Not Authenticated -          -        -         run 'scafctl auth login gcp' to authenticate
```

### Scripting with --exit-code

Use `--exit-code` to make the command exit non-zero when any handler is not authenticated -- handy in CI pre-flight checks:

{{< tabs "auth-tutorial-cmd-41" >}}
{{% tab "Bash" %}}
```bash
# Fail the script if not authenticated
scafctl auth status entra --exit-code || { echo "Not authenticated. Run: scafctl auth login entra"; exit 1; }
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# Fail the script if not authenticated
scafctl auth status entra --exit-code
if ($LASTEXITCODE -ne 0) { Write-Output "Not authenticated. Run: scafctl auth login entra"; exit 1; }
```
{{% /tab %}}
{{< /tabs >}}

### Proactive Expiry Warnings (--warn-within)

Use `--warn-within <duration>` to exit non-zero if any authenticated token will expire within the given window. This catches near-expiry tokens **before** they cause a mid-job failure:

{{< tabs "auth-tutorial-cmd-42" >}}
{{% tab "Bash" %}}
```bash
# Exit non-zero if any token expires within 10 minutes
scafctl auth status --warn-within 10m

# Check a specific handler
scafctl auth status entra --warn-within 1h

# Combine with --exit-code for a full CI pre-flight check
scafctl auth status --exit-code --warn-within 15m || {
  echo "Auth pre-flight failed -- not authenticated or token expiring soon"
  exit 1
}
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# Exit non-zero if any token expires within 10 minutes
scafctl auth status --warn-within 10m

# Check a specific handler
scafctl auth status entra --warn-within 1h

# Combine with --exit-code for a full CI pre-flight check
scafctl auth status --exit-code --warn-within 15m
if ($LASTEXITCODE -ne 0) {
  Write-Output "Auth pre-flight failed -- not authenticated or token expiring soon"
  exit 1
}
```
{{% /tab %}}
{{< /tabs >}}

The JSON output includes a `cachedTokens` field showing how many access tokens are in the cache for each handler -- useful for verifying token cache health:

{{< tabs "auth-cached-tokens" >}}
{{% tab "Bash" %}}
```bash
scafctl auth status -o json | jq '.[].cachedTokens'
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
(scafctl auth status -o json | ConvertFrom-Json).cachedTokens
```
{{% /tab %}}
{{< /tabs >}}

---

## Listing and Sorting Cached Tokens

The `auth list` command shows metadata for all cached tokens (refresh tokens and access tokens) without revealing the actual token values:

{{< tabs "auth-tutorial-cmd-43" >}}
{{% tab "Bash" %}}
```bash
# Show all cached tokens for all handlers
scafctl auth list

# Show cached tokens for a single handler
scafctl auth list entra
scafctl auth list github
scafctl auth list gcp

# Show only expired tokens
scafctl auth list --expired-only

# Show only valid tokens
scafctl auth list --valid-only

# Sort by expiry (soonest expiring first -- useful for spotting tokens about to expire)
scafctl auth list --sort expires-at

# Sort by handler name
scafctl auth list --sort handler

# Sort by scope
scafctl auth list --sort scope

# Output as JSON for scripting
scafctl auth list -o json

# Remove all expired access tokens from the cache (refresh tokens and valid tokens are preserved)
scafctl auth list --purge-expired

# Purge expired tokens for a specific handler
scafctl auth list entra --purge-expired
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# Show all cached tokens for all handlers
scafctl auth list

# Show cached tokens for a single handler
scafctl auth list entra
scafctl auth list github
scafctl auth list gcp

# Show only expired tokens
scafctl auth list --expired-only

# Show only valid tokens
scafctl auth list --valid-only

# Sort by expiry (soonest expiring first -- useful for spotting tokens about to expire)
scafctl auth list --sort expires-at

# Sort by handler name
scafctl auth list --sort handler

# Sort by scope
scafctl auth list --sort scope

# Output as JSON for scripting
scafctl auth list -o json

# Remove all expired access tokens from the cache (refresh tokens and valid tokens are preserved)
scafctl auth list --purge-expired

# Purge expired tokens for a specific handler
scafctl auth list entra --purge-expired
```
{{% /tab %}}
{{< /tabs >}}

**Sort fields:**

| Flag value | Sorted by |
|------------|-----------|
| `handler` | Auth handler name |
| `kind` | Token kind (`refresh` / `access`) |
| `scope` | OAuth scope string |
| `expires-at` | Expiry time (soonest first) |
| `cached-at` | When the token was cached (oldest first) |

The `getTokenCommand` column in the output shows the exact `scafctl auth token` command to retrieve each access token, making it easy to copy-paste for debugging.

### Purging Expired Tokens (--purge-expired)

Over time the token cache can accumulate expired access tokens. Use `--purge-expired` to clean them up. Refresh tokens and still-valid access tokens are **not** affected:

{{< tabs "auth-tutorial-cmd-44" >}}
{{% tab "Bash" %}}
```bash
# Remove expired cache entries across all handlers
scafctl auth list --purge-expired
# Output:
# ✓ Purged 2 expired access token(s) from entra.
# ✓ Purged 0 expired access token(s) from github.

# Purge only for a specific handler
scafctl auth list gcp --purge-expired
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# Remove expired cache entries across all handlers
scafctl auth list --purge-expired
# Output:
# ✓ Purged 2 expired access token(s) from entra.
# ✓ Purged 0 expired access token(s) from github.

# Purge only for a specific handler
scafctl auth list gcp --purge-expired
```
{{% /tab %}}
{{< /tabs >}}

> `--purge-expired` cannot be combined with `--expired-only` or `--valid-only` (it exits early without listing).

---

## Using Auth in HTTP Providers

The HTTP provider supports automatic authentication via the `authProvider` and `scope` properties.

### Basic Example

Create a file called `graph-example.yaml`:

```yaml
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: graph-example
  version: 1.0.0

spec:
  resolvers:
    me:
      type: object
      resolve:
        with:
          - provider: http
            inputs:
              url: "https://graph.microsoft.com/v1.0/me"
              method: GET
              authProvider: entra
              scope: "https://graph.microsoft.com/.default"
```

Run it (requires prior authentication via `scafctl auth login entra`):

{{< tabs "auth-tutorial-cmd-45" >}}
{{% tab "Bash" %}}
```bash
# Login with Microsoft Graph scope for consent
scafctl auth login entra --scope https://graph.microsoft.com/User.Read

# Then run the resolver
scafctl run resolver -f graph-example.yaml -o json
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# Login with Microsoft Graph scope for consent
scafctl auth login entra --scope https://graph.microsoft.com/User.Read

# Then run the resolver
scafctl run resolver -f graph-example.yaml -o json
```
{{% /tab %}}
{{< /tabs >}}

> [!CAUTION]
> **Note:** If you see a "consent required" error, it means your login session
> doesn't have consent for the requested API scope. Re-login with the `--scope`
> flag to grant consent:
> 
> ```bash
> scafctl auth login entra --scope https://graph.microsoft.com/User.Read
> ```

When you run this, scafctl:

1. Retrieves a cached token (or fetches a new one)
2. Adds the `Authorization: Bearer <token>` header
3. Executes the HTTP request
4. Returns the response data

### How It Works

| Property | Description |
|----------|-------------|
| `authProvider` | The auth handler to use (e.g., `entra`) |
| `scope` | The OAuth scope required for the API |

The token is validated to ensure it will remain valid for the duration of the request (timeout + 60 seconds buffer).

### Automatic 401 Retry

If the API returns HTTP 401 (Unauthorized), scafctl automatically:

1. Requests a fresh token (bypassing the cache)
2. Retries the request once with the new token

This handles cases where a cached token has been revoked.

### Azure Resource Manager Example

Add the following resolver to your solution's `spec.resolvers` section:

```yaml
spec:
  resolvers:
    subscription:
      type: object
      resolve:
        with:
          - provider: http
            inputs:
              url: "https://management.azure.com/subscriptions?api-version=2022-12-01"
              method: GET
              authProvider: entra
              scope: "https://management.azure.com/.default"
```

### Key Vault Example

Add the following resolver to your solution's `spec.resolvers` section:

```yaml
spec:
  resolvers:
    secret:
      type: object
      resolve:
        with:
          - provider: http
            inputs:
              url: "https://myvault.vault.azure.net/secrets/mysecret?api-version=7.4"
              method: GET
              authProvider: entra
              scope: "https://vault.azure.net/.default"
```

### GitHub API Example

Use the GitHub auth handler to authenticate API requests. Note that `scope` is not
needed for GitHub -- scopes are fixed at login time:

```yaml
spec:
  resolvers:
    repos:
      type: object
      resolve:
        with:
          - provider: http
            inputs:
              url: "https://api.github.com/user/repos?per_page=10&sort=updated"
              method: GET
              authProvider: github
```

Run it (requires prior authentication):

{{< tabs "auth-tutorial-cmd-46" >}}
{{% tab "Bash" %}}
```bash
# Login with GitHub
scafctl auth login github

# Run the resolver
scafctl run resolver -f github-repos.yaml -o json
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# Login with GitHub
scafctl auth login github

# Run the resolver
scafctl run resolver -f github-repos.yaml -o json
```
{{% /tab %}}
{{< /tabs >}}

### GitHub Enterprise Server API Example

```yaml
spec:
  resolvers:
    ghesRepos:
      type: object
      resolve:
        with:
          - provider: http
            inputs:
              url: "https://github.example.com/api/v3/user/repos"
              method: GET
              authProvider: github
```

---

## Getting Tokens for Debugging

The `auth token` command retrieves a valid access token for debugging:

{{< tabs "auth-tutorial-cmd-47" >}}
{{% tab "Bash" %}}
```bash
# Get a token for Microsoft Graph (Entra supports per-request scopes)
scafctl auth token entra --scope "https://graph.microsoft.com/.default"

# Get a GitHub token (uses scopes from login; --scope is not supported)
scafctl auth token github

# Get a GCP token
scafctl auth token gcp --scope "https://www.googleapis.com/auth/cloud-platform"
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# Get a token for Microsoft Graph (Entra supports per-request scopes)
scafctl auth token entra --scope "https://graph.microsoft.com/.default"

# Get a GitHub token (uses scopes from login; --scope is not supported)
scafctl auth token github

# Get a GCP token
scafctl auth token gcp --scope "https://www.googleapis.com/auth/cloud-platform"
```
{{% /tab %}}
{{< /tabs >}}

> [!NOTE]
> **Note:** The `--scope` flag is only supported on `auth token` for handlers
> with the `scopes-on-token-request` capability (e.g., Entra ID and GCP). GitHub scopes
> are fixed at login time -- use `scafctl auth login github --scope <scope>` to
> change them.

### Example Output

```
Handler   Scope                                       Token                Expires
entra     https://graph.microsoft.com/.default        eyJ0eXAi...****      2026-02-04 16:30:00
```

The token is masked in table output for security. Use JSON output to get the full token:

{{< tabs "auth-tutorial-cmd-48" >}}
{{% tab "Bash" %}}
```bash
scafctl auth token entra --scope "https://graph.microsoft.com/.default" -o json
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl auth token entra --scope "https://graph.microsoft.com/.default" -o json
```
{{% /tab %}}
{{< /tabs >}}

### Token Caching

Access tokens are cached to disk (encrypted) and reused if they have sufficient remaining validity:

{{< tabs "auth-tutorial-cmd-49" >}}
{{% tab "Bash" %}}
```bash
# Get a token valid for at least 5 minutes
scafctl auth token entra --scope "https://graph.microsoft.com/.default" --min-valid-for 5m
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# Get a token valid for at least 5 minutes
scafctl auth token entra --scope "https://graph.microsoft.com/.default" --min-valid-for 5m
```
{{% /tab %}}
{{< /tabs >}}

### Force Refresh

If you need a fresh token regardless of cache state (e.g., after permission changes), use `--force-refresh`:

{{< tabs "auth-tutorial-cmd-50" >}}
{{% tab "Bash" %}}
```bash
# Force acquiring a new token, bypassing the cache
scafctl auth token entra --scope "https://graph.microsoft.com/.default" --force-refresh
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# Force acquiring a new token, bypassing the cache
scafctl auth token entra --scope "https://graph.microsoft.com/.default" --force-refresh
```
{{% /tab %}}
{{< /tabs >}}

### Printing the Raw Token (Scripting)

Use `--raw` to print just the token value -- ideal for shell scripting:

{{< tabs "auth-tutorial-cmd-51" >}}
{{% tab "Bash" %}}
```bash
# Assign to a variable
export TOKEN=$(scafctl auth token gcp --scope "https://www.googleapis.com/auth/cloud-platform" --raw)

# Use directly with curl
curl -H "Authorization: Bearer $(scafctl auth token github --raw)" https://api.github.com/user
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# Assign to a variable
$env:TOKEN = $(scafctl auth token gcp --scope "https://www.googleapis.com/auth/cloud-platform" --raw)

# Use directly with curl
curl -H "Authorization: Bearer $(scafctl auth token github --raw)" https://api.github.com/user
```
{{% /tab %}}
{{< /tabs >}}

### Shell Export (eval-compatible)

Use `--export` to get a shell export statement you can `eval` into the current shell. The variable is named `<HANDLER>_TOKEN`:

{{< tabs "auth-tutorial-cmd-52" >}}
{{% tab "Bash" %}}
```bash
# Add a GCP token to the current shell environment
eval $(scafctl auth token gcp --scope "https://www.googleapis.com/auth/cloud-platform" --export)
echo $GCP_TOKEN

# Other handlers follow the same pattern
eval $(scafctl auth token github --export)
echo $GITHUB_TOKEN

eval $(scafctl auth token entra --scope "https://management.azure.com/.default" --export)
echo $ENTRA_TOKEN
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# Add a GCP token to the current shell environment
Invoke-Expression $(scafctl auth token gcp --scope "https://www.googleapis.com/auth/cloud-platform" --export)
Write-Output $GCP_TOKEN

# Other handlers follow the same pattern
Invoke-Expression $(scafctl auth token github --export)
Write-Output $GITHUB_TOKEN

Invoke-Expression $(scafctl auth token entra --scope "https://management.azure.com/.default" --export)
Write-Output $ENTRA_TOKEN
```
{{% /tab %}}
{{< /tabs >}}

### Emitting a Ready-to-Run curl Command

Use `--curl` to print a `curl` command with the `Authorization` header already populated -- great for quick API call reproduction without any `jq` piping:

{{< tabs "auth-tutorial-cmd-53" >}}
{{% tab "Bash" %}}
```bash
# Emit a curl one-liner for Microsoft Graph
scafctl auth token entra --scope "https://graph.microsoft.com/.default" \
  --curl --curl-url "https://graph.microsoft.com/v1.0/me"
# Output:
# curl -H "Authorization: Bearer eyJ..." "https://graph.microsoft.com/v1.0/me"

# Emit a curl one-liner for GCP
scafctl auth token gcp --scope "https://www.googleapis.com/auth/cloud-platform" \
  --curl --curl-url "https://storage.googleapis.com/storage/v1/b?project=my-project"

# Emit without a URL (useful to inspect -- fills in a placeholder)
scafctl auth token github --curl
# Output:
# curl -H "Authorization: Bearer ghp_..." "<URL>"
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# Emit a curl one-liner for Microsoft Graph
scafctl auth token entra --scope "https://graph.microsoft.com/.default" `
  --curl --curl-url "https://graph.microsoft.com/v1.0/me"
# Output:
# curl -H "Authorization: Bearer eyJ..." "https://graph.microsoft.com/v1.0/me"

# Emit a curl one-liner for GCP
scafctl auth token gcp --scope "https://www.googleapis.com/auth/cloud-platform" `
  --curl --curl-url "https://storage.googleapis.com/storage/v1/b?project=my-project"

# Emit without a URL (useful to inspect -- fills in a placeholder)
scafctl auth token github --curl
# Output:
# curl -H "Authorization: Bearer ghp_..." "<URL>"
```
{{% /tab %}}
{{< /tabs >}}

### Decoding the JWT (Header + Payload)

Use `--decode` to inspect the full JWT structure -- both the **header** and the **payload** -- without needing an external decoder tool. Signature validation is intentionally skipped; this is for debugging only:

{{< tabs "auth-jwt-decode" >}}
{{% tab "Bash" %}}
```bash
# Decode and display the full JWT (header and payload)
scafctl auth token entra --scope "https://graph.microsoft.com/.default" --decode

# Output as JSON -- filter with jq
scafctl auth token entra --scope "https://graph.microsoft.com/.default" --decode -o json \
  | jq '{alg: .header.alg, audience: .payload.aud, upn: .payload.upn, expires: .payload.exp_human}'
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl auth token entra --scope "https://graph.microsoft.com/.default" --decode

# Output as JSON -- use ConvertFrom-Json to filter
$decoded = scafctl auth token entra --scope "https://graph.microsoft.com/.default" --decode -o json | ConvertFrom-Json
$decoded | Select-Object @{N='alg';E={$_.header.alg}}, @{N='audience';E={$_.payload.aud}}, @{N='upn';E={$_.payload.upn}}, @{N='expires';E={$_.payload.exp_human}}
```
{{% /tab %}}
{{< /tabs >}}

Example output (table):

```
Key                   Value
header.alg            RS256
header.typ            JWT
header.kid            abc123...
payload.aud           https://graph.microsoft.com
payload.iss           https://login.microsoftonline.com/...
payload.sub           A3ECB230-...
payload.oid           12345678-...
payload.upn           user@example.com
payload.roles         ["Directory.Read.All"]
payload.exp           1740000000
payload.exp_human     2026-02-19T22:13:20Z
payload.iat_human     2026-02-19T21:13:20Z
```

The header section tells you the signing algorithm (`alg`), key ID (`kid`), and token type (`typ`) -- useful for confirming which key was used and troubleshooting signature or algorithm policy issues.

Unix timestamp fields (`exp`, `iat`, `nbf`, `auth_time`) are automatically augmented with a `_human` counterpart in RFC 3339 format.

This is the single fastest way to check:
- Which audience (`aud`) the token is for
- Which roles or scopes are included (`roles`, `scp`)
- Whether the token's expiry (`exp_human`) matches what you expect
- Tenant, OID, and UPN for identity verification

### Copying to Clipboard

Use `--clip` to copy the token directly to your clipboard without it appearing in your terminal (useful when pasting into browser DevTools or Postman):

{{< tabs "auth-tutorial-cmd-54" >}}
{{% tab "Bash" %}}
```bash
scafctl auth token entra --scope "https://management.azure.com/.default" --clip
# Output: ✓ Token copied to clipboard (expires in 58m42s).
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl auth token entra --scope "https://management.azure.com/.default" --clip
# Output: ✓ Token copied to clipboard (expires in 58m42s).
```
{{% /tab %}}
{{< /tabs >}}

### Inspecting Scoped Token Claims in Resolvers

The `identity` provider's `scope` input lets you mint a fresh access token for a
specific OAuth scope inside a resolver and inspect the claims or metadata parsed
from its JWT -- without ever exposing the token value. This is useful for
preflight checks, per-API identity auditing, and debugging consent errors.

```yaml
# Mint a token for an API scope and surface the caller's identity claims
resolve:
  with:
    - provider: identity
      inputs:
        operation: claims
        scope: api://my-app/.default

# Check token expiry and flow for a management-plane scope
resolve:
  with:
    - provider: identity
      inputs:
        operation: status
        scope: https://management.azure.com/.default
        handler: entra
```

**Key differences from `auth token`:**

| | `auth token` | `identity` provider (scoped) |
|-|---|---|
| Returns the raw token | ✅ | ❌ (token is never exposed) |
| Parses JWT claims | ❌ | ✅ |
| Usable inside a resolver pipeline | ❌ | ✅ |
| Dry-run support | N/A | ✅ |

When the access token is opaque (not a decodable JWT -- common with Microsoft
Graph tokens), `claims` will be `null` and a warning is emitted. Token metadata
such as expiry and type is still returned.

> [!CAUTION]
> **Scope restriction:** The `scope` input is only valid for `claims` and
> `status` operations. Using it with `groups` or `list` returns an error.

### Using the Token Directly

Get the full token for use with other tools:

{{< tabs "auth-token-usage" >}}
{{% tab "Bash" %}}
```bash
# Approach 1: --raw (simplest)
curl -H "Authorization: Bearer $(scafctl auth token entra --scope 'https://graph.microsoft.com/.default' --raw)" \
  https://graph.microsoft.com/v1.0/me

# Approach 2: --curl (no jq required)
scafctl auth token entra --scope "https://graph.microsoft.com/.default" \
  --curl --curl-url "https://graph.microsoft.com/v1.0/me" | bash

# Approach 3: JSON output + jq (most flexible)
TOKEN=$(scafctl auth token entra --scope "https://graph.microsoft.com/.default" -o json | jq -r '.accessToken')
curl -H "Authorization: Bearer $TOKEN" https://graph.microsoft.com/v1.0/me

# GitHub API example
curl -H "Authorization: Bearer $(scafctl auth token github --raw)" https://api.github.com/user/repos
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# Approach 1: --raw (simplest)
$token = scafctl auth token entra --scope 'https://graph.microsoft.com/.default' --raw
Invoke-RestMethod -Uri 'https://graph.microsoft.com/v1.0/me' -Headers @{ Authorization = "Bearer $token" }

# Approach 2: JSON output + ConvertFrom-Json
$token = (scafctl auth token entra --scope 'https://graph.microsoft.com/.default' -o json | ConvertFrom-Json).accessToken
Invoke-RestMethod -Uri 'https://graph.microsoft.com/v1.0/me' -Headers @{ Authorization = "Bearer $token" }
```
{{% /tab %}}
{{< /tabs >}}

---

## Configuration

You can configure authentication defaults in your config file (`~/.config/scafctl/config.yaml` or `~/.scafctl/config.yaml`):

```yaml
auth:
  entra:
    # Default tenant (use "organizations" for any work/school account)
    tenantId: "08e70e8e-d05c-4449-a2c2-67bd0a9c4e79"
    
    # Custom application ID (optional - uses scafctl's public client by default)
    clientId: "your-app-client-id"
    
    # Default scopes requested during login
    defaultScopes:
      - "openid"
      - "profile"
      - "offline_access"
```

### Configuration Reference

| Field | Description | Default |
|-------|-------------|---------|
| `auth.entra.tenantId` | Default Azure tenant ID | `organizations` |
| `auth.entra.clientId` | Azure application (client) ID | scafctl public client |
| `auth.entra.defaultScopes` | Scopes requested during login | `openid`, `profile`, `offline_access` |

### Using a Custom Application

If you need to use your own Azure application registration:

1. Register an application in Azure Entra ID
2. Configure it as a public client (mobile/desktop)
3. Add the required API permissions
4. Set `auth.entra.clientId` in your config

### GCP Configuration

You can configure a custom OAuth client for GCP to avoid depending on gcloud ADC:

```yaml
auth:
  gcp:
    # Custom OAuth 2.0 client (bypasses gcloud ADC)
    clientId: "123456789-abc123.apps.googleusercontent.com"
    clientSecret: "GOCSPX-xxxxxxxxxxxxxxxxxxxxxxxxxx"

    # Default scopes requested during login
    defaultScopes:
      - "openid"
      - "https://www.googleapis.com/auth/cloud-platform"

    # Optional: impersonate a service account
    impersonateServiceAccount: "deploy@my-project.iam.gserviceaccount.com"

    # Optional: default project
    project: "my-project-123"
```

| Field | Description | Default |
|-------|-------------|---------|
| `auth.gcp.clientId` | OAuth 2.0 client ID | *(empty -- uses gcloud ADC)* |
| `auth.gcp.clientSecret` | OAuth 2.0 client secret | *(empty)* |
| `auth.gcp.defaultScopes` | Scopes requested during login | `openid`, `cloud-platform` |
| `auth.gcp.impersonateServiceAccount` | Service account to impersonate | *(empty)* |
| `auth.gcp.project` | Default GCP project ID | *(empty)* |

> For a complete guide on creating the OAuth client with `gcloud` commands, see [GCP Custom OAuth Client Setup](gcp-custom-oauth-tutorial.md).

---

## Logging Out

To clear stored credentials:

{{< tabs "auth-tutorial-cmd-55" >}}
{{% tab "Bash" %}}
```bash
# Logout from Entra ID
scafctl auth logout entra

# Logout from GitHub
scafctl auth logout github

# Logout from GCP
scafctl auth logout gcp

# Logout from all registered handlers at once (prompts for confirmation)
scafctl auth logout --all

# Skip the confirmation prompt (for scripts and CI)
scafctl auth logout --all --yes
scafctl auth logout --all -y

# Force clear credentials even if not currently logged in
scafctl auth logout entra --force
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# Logout from Entra ID
scafctl auth logout entra

# Logout from GitHub
scafctl auth logout github

# Logout from GCP
scafctl auth logout gcp

# Logout from all registered handlers at once (prompts for confirmation)
scafctl auth logout --all

# Skip the confirmation prompt (for scripts and CI)
scafctl auth logout --all --yes
scafctl auth logout --all -y

# Force clear credentials even if not currently logged in
scafctl auth logout entra --force
```
{{% /tab %}}
{{< /tabs >}}

This removes:

- The stored refresh token (or access token for GitHub)
- All cached access tokens
- Token metadata

### Dry Run

Use `--dry-run` to see which credentials would be removed without actually removing them. Useful before running `--all` in a shared or production environment:

{{< tabs "auth-tutorial-cmd-56" >}}
{{% tab "Bash" %}}
```bash
# Preview what would be removed for Entra
scafctl auth logout entra --dry-run
# Output: [dry-run] Would log out from Microsoft Entra ID (cached tokens and refresh token would be removed).

# Preview across all handlers
scafctl auth logout --all --dry-run
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# Preview what would be removed for Entra
scafctl auth logout entra --dry-run
# Output: [dry-run] Would log out from Microsoft Entra ID (cached tokens and refresh token would be removed).

# Preview across all handlers
scafctl auth logout --all --dry-run
```
{{% /tab %}}
{{< /tabs >}}

### Example Output

```
✓ Successfully logged out from Microsoft Entra ID.
```

---

## Auth Diagnostics

The `auth diagnose` command (alias: `auth doctor`) runs a series of health checks
and reports any issues with your auth configuration. It's the first command to run
when troubleshooting auth problems:

{{< tabs "auth-tutorial-cmd-57" >}}
{{% tab "Bash" %}}
```bash
# Run all checks and print a human-readable report
scafctl auth diagnose

# Alias
scafctl auth doctor

# Scope checks to a single handler (skips checks for the others)
scafctl auth diagnose entra
scafctl auth diagnose github
scafctl auth diagnose gcp

# Also attempt a live token fetch for each authenticated handler
scafctl auth diagnose --live-token

# Scope live-token check to one handler
scafctl auth diagnose entra --live-token

# Output as JSON for automated pipelines
scafctl auth diagnose -o json
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# Run all checks and print a human-readable report
scafctl auth diagnose

# Alias
scafctl auth doctor

# Scope checks to a single handler (skips checks for the others)
scafctl auth diagnose entra
scafctl auth diagnose github
scafctl auth diagnose gcp

# Also attempt a live token fetch for each authenticated handler
scafctl auth diagnose --live-token

# Scope live-token check to one handler
scafctl auth diagnose entra --live-token

# Output as JSON for automated pipelines
scafctl auth diagnose -o json
```
{{% /tab %}}
{{< /tabs >}}

### What It Checks

| Category | What is checked |
|----------|-----------------|
| `registry` | Auth handlers are registered and available |
| `config` | Config file presence; `auth.entra`, `auth.github`, `auth.gcp` sections |
| `env` | Relevant environment variables (`AZURE_*`, `GITHUB_TOKEN`, `GOOGLE_*`) |
| `clock-skew` | System clock is validated against an external time source; warns if skew exceeds 5 minutes (clock skew causes token validation failures) |
| `handler` | Each handler's authentication status; hints for unauthenticated handlers |
| `cache` | Token cache health -- count and number of expired cached tokens |
| `live` | *(Only with `--live-token`)* Performs an actual `GetToken` call to confirm end-to-end flow |

### Example Output

```
✅ [ok]   auth registry: registered handlers: [entra gcp github]
⚠️  [warn] config file: config file not found -- using built-in defaults
✅ [ok]   env GITHUB_TOKEN: GitHub personal access token -- set
✅ [ok]   env gcp: gcloud ADC: gcloud Application Default Credentials file found
✅ [ok]   entra: authenticated: authenticated as "user@example.com", expires in 58m
⚠️  [warn] entra: token cache: 3 cached token(s), 1 expired
✅ [ok]   gcp: authenticated: authenticated as "gcloud ADC (application default credentials)"
✅ [ok]   gcp: token cache: 1 cached token(s)
⚠️  [warn] github: authenticated: not authenticated -- run 'scafctl auth login github'

⚠️ Diagnostics complete: 3 warning(s), 5 ok (no failures)
```

### Exit Codes

| Condition | Exit code |
|-----------|-----------|
| All checks pass or warn | `0` |
| Any check **fails** | non-zero |

> Warnings (unauthenticated handler, expired tokens, missing config file) do **not** produce a
> non-zero exit code on their own. Only hard failures (registry empty, handler init error) do.

### Using in CI Preflight

{{< tabs "auth-tutorial-cmd-58" >}}
{{% tab "Bash" %}}
```bash
# Verify end-to-end auth before running a pipeline
scafctl auth diagnose --live-token
if [ $? -ne 0 ]; then
  echo "Auth health check failed. Check 'scafctl auth diagnose' output."
  exit 1
fi
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# Verify end-to-end auth before running a pipeline
scafctl auth diagnose --live-token
if ($? -ne 0) {
  Write-Output "Auth health check failed. Check 'scafctl auth diagnose' output."
  exit 1
}
```
{{% /tab %}}
{{< /tabs >}}

---

## Troubleshooting

### "Not authenticated" Error

If you see this error when running a solution:

```
not authenticated: please run 'scafctl auth login entra'
```

Solution: Run `scafctl auth login entra` to authenticate.

For GitHub:

```
not authenticated: please run 'scafctl auth login github'
```

Solution: Run `scafctl auth login github` or set `GITHUB_TOKEN` environment variable.

### Token Expired

If you see:

```
credentials expired: please run 'scafctl auth login entra'
```

Your refresh token has expired (typically after 90 days of inactivity). Log in again:

{{< tabs "auth-tutorial-cmd-59" >}}
{{% tab "Bash" %}}
```bash
scafctl auth login entra
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl auth login entra
```
{{% /tab %}}
{{< /tabs >}}

### Consent Required

If you see:

```
consent required: please login with the required scope
```

Your login session does not have consent for the API scope your resolver is requesting. Re-login with the `--scope` flag to grant consent:

{{< tabs "auth-tutorial-cmd-60" >}}
{{% tab "Bash" %}}
```bash
# For Microsoft Graph APIs
scafctl auth login entra --scope https://graph.microsoft.com/User.Read

# For Azure Resource Manager APIs
scafctl auth login entra --scope https://management.azure.com/user_impersonation
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# For Microsoft Graph APIs
scafctl auth login entra --scope https://graph.microsoft.com/User.Read

# For Azure Resource Manager APIs
scafctl auth login entra --scope https://management.azure.com/user_impersonation
```
{{% /tab %}}
{{< /tabs >}}

The `--scope` flag tells Azure to request user consent for that specific API during the login flow.

### AADSTS500113: No Reply Address Registered

If the browser shows this error during interactive login:

```
AADSTS500113: No reply address is registered for the application.
```

This means the app registration does not have a redirect URI matching `http://localhost`. The default Azure CLI client ID already has this registered; this error only occurs with custom `--client-id` values.

**Fix options:**

1. **Register the redirect URI** (recommended): In the Azure portal, go to **App registrations → your app → Authentication → Add a platform → Mobile and desktop applications**, then add `http://localhost`.

2. **Use a specific port**: If the app registration only allows a specific URI like `http://localhost:8400`:

{{< tabs "auth-tutorial-cmd-61" >}}
{{% tab "Bash" %}}
```bash
   scafctl auth login entra --client-id YOUR_CLIENT_ID --callback-port 8400
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
   scafctl auth login entra --client-id YOUR_CLIENT_ID --callback-port 8400
```
{{% /tab %}}
{{< /tabs >}}

3. **Use device code flow**: Device code does not require a redirect URI:

{{< tabs "auth-tutorial-cmd-62" >}}
{{% tab "Bash" %}}
```bash
   scafctl auth login entra --client-id YOUR_CLIENT_ID --flow device-code
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
   scafctl auth login entra --client-id YOUR_CLIENT_ID --flow device-code
```
{{% /tab %}}
{{< /tabs >}}

### Login Times Out With No Error

If `scafctl auth login entra` times out after 5 minutes with no error in the
terminal, the most common cause is the AADSTS500113 error above -- check the
browser tab for an error message. The improved timeout message will now suggest
checking redirect URI registration.

### Wrong Tenant

If you're getting 401 errors but you're authenticated, you may be authenticated to the wrong tenant:

{{< tabs "auth-tutorial-cmd-63" >}}
{{% tab "Bash" %}}
```bash
# Check current auth status
scafctl auth status entra

# Log out and log in to the correct tenant
scafctl auth logout entra
scafctl auth login entra --tenant correct-tenant-id
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# Check current auth status
scafctl auth status entra

# Log out and log in to the correct tenant
scafctl auth logout entra
scafctl auth login entra --tenant correct-tenant-id
```
{{% /tab %}}
{{< /tabs >}}

### Scope Issues

If you're getting 403 (Forbidden) errors, the token may not have the required permissions:

1. Ensure you're using the correct scope (e.g., `https://graph.microsoft.com/.default`)
2. Verify your Azure application has the required API permissions
3. For some APIs, admin consent may be required

### Checking Token Claims

Use `--decode` on `auth token` to inspect JWT claims directly without needing an
external tool or manual base64 decoding:

{{< tabs "auth-troubleshoot-claims" >}}
{{% tab "Bash" %}}
```bash
# Decode and display claims in table format (no signature validation)
scafctl auth token entra --scope "https://graph.microsoft.com/.default" --decode

# Output as JSON for further processing (e.g., with jq)
scafctl auth token entra --scope "https://graph.microsoft.com/.default" --decode -o json | jq '.aud,.upn,.roles'
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl auth token entra --scope "https://graph.microsoft.com/.default" --decode

# Output as JSON for further processing
$decoded = scafctl auth token entra --scope "https://graph.microsoft.com/.default" --decode -o json | ConvertFrom-Json
$decoded.aud, $decoded.upn, $decoded.roles
```
{{% /tab %}}
{{< /tabs >}}

Unix timestamp fields (`exp`, `iat`, `nbf`, `auth_time`) are automatically augmented
with a `_human` RFC 3339 counterpart so you can read them without converting.

Useful things to verify:
- `aud` -- correct audience for the API you're calling
- `scp` / `roles` -- scopes or app roles granted
- `exp_human` -- actual token expiry in human-readable form
- `upn` / `unique_name` / `preferred_username` -- the authenticated identity

### Debug Logging

Enable debug logging to see detailed auth information:

{{< tabs "auth-tutorial-cmd-64" >}}
{{% tab "Bash" %}}
```bash
scafctl --log-level -1 auth status entra
scafctl --log-level -1 run solution -f mysolution.yaml
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl --log-level -1 auth status entra
scafctl --log-level -1 run solution -f mysolution.yaml
```
{{% /tab %}}
{{< /tabs >}}

### Secret Store Issues

scafctl uses your system's secret store (Keychain on macOS, Windows Credential Manager, or Secret Service on Linux). If you're having issues:

- **macOS**: Check Keychain Access for `scafctl.auth.entra.*` or `scafctl.auth.github.*` entries
- **Windows**: Check Credential Manager for `scafctl.auth.entra.*` or `scafctl.auth.github.*` entries
- **Linux**: Ensure `gnome-keyring` or `kwallet` is running

### GitHub: "Bad credentials" Error

If you see a 401 error with "Bad credentials" when using GitHub auth:

1. Your PAT may have expired or been revoked
2. Generate a new PAT at https://github.com/settings/tokens
3. Set the new token: `export GITHUB_TOKEN="ghp_..."`
4. Re-login: `scafctl auth login github`

### GitHub: Insufficient Scopes

If you get 403 (Forbidden) errors on GitHub API calls:

1. Check what scopes your token has: `scafctl auth status github`
2. GitHub scopes are fixed at login time and cannot be changed per-request. Login again with the required scopes:
{{< tabs "auth-tutorial-cmd-65" >}}
{{% tab "Bash" %}}
```bash
   scafctl auth logout github
   scafctl auth login github --scope repo --scope read:org
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
   scafctl auth logout github
   scafctl auth login github --scope repo --scope read:org
```
{{% /tab %}}
{{< /tabs >}}
3. For PATs, ensure the token was created with sufficient scopes

### GitHub Enterprise Server: Connection Issues

If you can't connect to your GHES instance:

1. Verify the hostname is correct: `scafctl auth status github`
2. Ensure the GHES instance has the device flow enabled
3. Check network connectivity to the GHES instance
4. Try with explicit hostname: `scafctl auth login github --hostname github.example.com`

---

## Custom OAuth2 Handlers

You can add OAuth2 handlers for any service by configuring them in your
`~/.config/scafctl/config.yaml`:

```yaml
auth:
  customOAuth2:
    - name: quay
      displayName: "Quay.io"
      authorizeURL: "https://quay.io/oauth/authorize"
      tokenURL: "https://quay.io/oauth/access_token"
      clientID: "your-app-client-id"
      defaultFlow: interactive
      responseType: token    # Quay only supports implicit grant (response_type=token)
      scopes:
        - "repo:read"
      registry: "quay.io"
      registryUsername: "$oauthtoken"
```

Once configured, custom handlers work exactly like built-in handlers:

```bash
# Login
scafctl auth login quay

# Check status
scafctl auth status quay

# Logout
scafctl auth logout quay

# Auto-detected for catalog login (via the 'registry' field)
scafctl catalog login quay.io
```

Custom handlers support all three OAuth2 flows:
- **Interactive** (authorization code + PKCE) -- requires `authorizeURL`. Set `responseType: token` for implicit grant servers. Set `disablePKCE: true` for servers that support auth code but reject PKCE parameters
- **Device code** (RFC 8628) -- requires `deviceAuthURL`  
- **Client credentials** -- requires `clientSecret`

For advanced configurations including token exchange and identity
verification, see [examples/auth/custom-oauth2-config.md](https://github.com/oakwood-commons/scafctl/blob/main/examples/auth/custom-oauth2-config.md).

---

## Next Steps

- [CEL Expressions Tutorial](cel-tutorial.md) -- Master CEL expressions and extension functions
- [Go Templates Tutorial](go-templates-tutorial.md) -- Generate files with Go template rendering
- [Resolver Tutorial](resolver-tutorial.md) -- More HTTP examples in resolver pipelines
- [Provider Reference](provider-reference.md) -- Complete provider documentation
