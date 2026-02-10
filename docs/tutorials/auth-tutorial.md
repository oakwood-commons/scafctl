---
title: "Authentication Tutorial"
weight: 40
---

# Authentication Tutorial

This tutorial walks you through setting up and using authentication in scafctl. You'll learn how to authenticate with Microsoft Entra ID, manage credentials, and use authenticated HTTP requests in your solutions.

## Prerequisites

- scafctl installed and available in your PATH
- Access to a Microsoft Entra ID tenant
- A web browser for completing the device code flow

## Table of Contents

1. [Understanding Auth in scafctl](#understanding-auth-in-scafctl)
2. [Logging In](#logging-in)
   - [Device Code Flow](#example-output)
   - [Service Principal](#service-principal-authentication-cicd)
   - [Workload Identity](#workload-identity-authentication-kubernetes)
3. [Checking Auth Status](#checking-auth-status)
4. [Using Auth in HTTP Providers](#using-auth-in-http-providers)
5. [Getting Tokens for Debugging](#getting-tokens-for-debugging)
6. [Configuration](#configuration)
7. [Logging Out](#logging-out)
8. [Troubleshooting](#troubleshooting)

---

## Understanding Auth in scafctl

Authentication in scafctl follows these principles:

- **Providers declare auth requirements**, not credentials
- **Token acquisition is separated** from provider execution
- **Refresh tokens are stored securely** using your system's secret store
- **Access tokens are short-lived** and cached for performance
- **Secrets never appear** in solution files or logs

scafctl currently supports the following auth handlers:

| Handler | Description | Flows |
|---------|-------------|-------|
| `entra` | Microsoft Entra ID (Azure AD) | Device Code, Service Principal, Workload Identity |

---

## Logging In

To authenticate with Microsoft Entra ID, use the `auth login` command:

```bash
scafctl auth login entra
```

This initiates a device code flow:

1. scafctl displays a code and URL
2. Open the URL in your browser
3. Enter the code when prompted
4. Sign in with your Microsoft account
5. scafctl stores your refresh token securely

### Example Output

```
To sign in, use a web browser to open the page https://microsoft.com/devicelogin
and enter the code ABCD1234 to authenticate.

Waiting for authentication...

✓ Successfully authenticated as user@example.com
  Tenant: contoso.onmicrosoft.com
```

### Specifying a Tenant

By default, scafctl uses the "organizations" tenant (any work/school account). To authenticate with a specific tenant:

```bash
# Use a specific tenant ID
scafctl auth login entra --tenant 08e70e8e-d05c-4449-a2c2-67bd0a9c4e79

# Use a tenant domain
scafctl auth login entra --tenant contoso.onmicrosoft.com
```

### Setting a Timeout

The device code flow has a 5-minute default timeout. To extend it:

```bash
scafctl auth login entra --timeout 10m
```

### Service Principal Authentication (CI/CD)

For non-interactive scenarios like CI/CD pipelines, use service principal authentication:

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

```bash
# Auto-detected when running in a configured pod
scafctl auth login entra

# Or explicitly specify the flow
scafctl auth login entra --flow workload-identity
```

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

   ```bash
   # Using Azure CLI
   az ad app create --display-name "scafctl-workload-identity-test"
   ```

2. **Note the Application (client) ID** - you'll need this for `AZURE_CLIENT_ID`

3. **Create a service principal** for the application:

   ```bash
   az ad sp create --id <application-id>
   ```

4. **Grant API permissions** as needed (e.g., Microsoft Graph, Azure Resource Manager)

##### Step 2: Configure Federated Identity Credential

The federated identity credential tells Entra ID to trust tokens from your Kubernetes cluster's OIDC issuer.

1. **Get your Kubernetes cluster's OIDC issuer URL**:

   ```bash
   # For AKS
   az aks show --name <cluster-name> --resource-group <rg-name> \
     --query "oidcIssuerProfile.issuerUrl" -o tsv

   # For other clusters (e.g., kind, minikube with OIDC enabled)
   kubectl get --raw /.well-known/openid-configuration | jq -r '.issuer'
   ```

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

   ```bash
   az ad app federated-credential create --id <application-id> --parameters '{
     "name": "scafctl-test-credential",
     "issuer": "https://oidc.example.com/abc123",
     "subject": "system:serviceaccount:default:scafctl-sa",
     "audiences": ["api://AzureADTokenExchange"]
   }'
   ```

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

   ```bash
   # Generate a token with the correct audience
   kubectl create token scafctl-sa \
     --namespace default \
     --audience "api://AzureADTokenExchange" \
     --duration 1h
   ```

   **Important:** The `--audience` must match what you configured in the federated credential.

3. **Save the token** to an environment variable or file:

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

##### Step 4: Authenticate with scafctl

**Option A: Using the `--federated-token` flag (recommended for testing):**

```bash
export AZURE_CLIENT_ID="<your-application-client-id>"
export AZURE_TENANT_ID="<your-tenant-id>"

# Pass the token directly
scafctl auth login entra --flow workload-identity --federated-token "$FEDERATED_TOKEN"
```

**Option B: Using the `AZURE_FEDERATED_TOKEN` environment variable:**

```bash
export AZURE_CLIENT_ID="<your-application-client-id>"
export AZURE_TENANT_ID="<your-tenant-id>"
export AZURE_FEDERATED_TOKEN="$FEDERATED_TOKEN"

scafctl auth login entra --flow workload-identity
```

**Option C: Using a token file (simulates in-cluster behavior):**

```bash
export AZURE_CLIENT_ID="<your-application-client-id>"
export AZURE_TENANT_ID="<your-tenant-id>"
export AZURE_FEDERATED_TOKEN_FILE="/tmp/federated-token.txt"

scafctl auth login entra --flow workload-identity
```

##### Step 5: Verify Authentication

```bash
# Check auth status
scafctl auth status entra

# Output for workload identity:
# Handler   Status         Identity                          IdentityType         TokenFile
# entra     Authenticated  Workload Identity (12345678...)   workload-identity    (direct token)
```

##### Complete Example Script

Here's a complete script for testing workload identity locally:

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

##### Troubleshooting Workload Identity

**Error: "AADSTS70021: No matching federated identity record found"**

This means the token's claims don't match the federated credential configuration:
- Verify the `issuer` matches your cluster's OIDC issuer URL exactly
- Verify the `subject` matches your service account (`system:serviceaccount:<namespace>:<name>`)
- Verify the `audience` in both the federated credential and `kubectl create token` command

```bash
# Decode the token to inspect its claims
echo "$FEDERATED_TOKEN" | cut -d. -f2 | base64 -d 2>/dev/null | jq .
```

Check these claims match your federated credential:
- `iss` (issuer)
- `sub` (subject)
- `aud` (audience)

**Error: "AADSTS700024: Client assertion is not within its valid time range"**

The token has expired. Generate a new one:

```bash
kubectl create token scafctl-sa --audience "api://AzureADTokenExchange" --duration 1h
```

**Error: "workload identity not configured"**

Ensure all required environment variables are set:

```bash
echo "AZURE_CLIENT_ID: $AZURE_CLIENT_ID"
echo "AZURE_TENANT_ID: $AZURE_TENANT_ID"
echo "AZURE_FEDERATED_TOKEN: ${AZURE_FEDERATED_TOKEN:0:20}..." # First 20 chars
```

**Checking the OIDC Discovery Document**

Verify your cluster's OIDC configuration is accessible:

```bash
# Get the OIDC configuration
curl -s "$(kubectl get --raw /.well-known/openid-configuration | jq -r '.issuer')/.well-known/openid-configuration" | jq .

# Get the JWKS (signing keys)
curl -s "$(kubectl get --raw /.well-known/openid-configuration | jq -r '.jwks_uri')" | jq .
```

---

## Checking Auth Status

To see your current authentication status:

```bash
# Show status for all handlers
scafctl auth status

# Show status for a specific handler
scafctl auth status entra

# Output as JSON
scafctl auth status -o json
```

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

When not authenticated:

```
Handler   Status            Identity   Tenant   Expires
entra     Not Authenticated -          -        -
```

---

## Using Auth in HTTP Providers

The HTTP provider supports automatic authentication via the `authProvider` and `scope` properties.

### Basic Example

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

When you run this solution, scafctl:

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

---

## Getting Tokens for Debugging

The `auth token` command retrieves a valid access token for debugging:

```bash
# Get a token for Microsoft Graph
scafctl auth token entra --scope "https://graph.microsoft.com/.default"
```

### Example Output

```
Handler   Scope                                       Token                Expires
entra     https://graph.microsoft.com/.default        eyJ0eXAi...****      2026-02-04 16:30:00
```

The token is masked in table output for security. Use JSON output to get the full token:

```bash
scafctl auth token entra --scope "https://graph.microsoft.com/.default" -o json
```

### Token Caching

Access tokens are cached to disk (encrypted) and reused if they have sufficient remaining validity:

```bash
# Get a token valid for at least 5 minutes
scafctl auth token entra --scope "https://graph.microsoft.com/.default" --min-valid-for 5m
```

### Using the Token Directly

You can use the token in other tools:

```bash
# Use with curl
TOKEN=$(scafctl auth token entra --scope "https://graph.microsoft.com/.default" -o json | jq -r '.accessToken')
curl -H "Authorization: Bearer $TOKEN" https://graph.microsoft.com/v1.0/me

# Use with httpie
scafctl auth token entra --scope "https://graph.microsoft.com/.default" -o json | \
  jq -r '"Bearer " + .accessToken' | \
  http GET https://graph.microsoft.com/v1.0/me Authorization:@-
```

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

---

## Logging Out

To clear stored credentials:

```bash
scafctl auth logout entra
```

This removes:

- The stored refresh token
- All cached access tokens
- Token metadata

### Example Output

```
✓ Successfully logged out from entra
```

---

## Troubleshooting

### "Not authenticated" Error

If you see this error when running a solution:

```
not authenticated: please run 'scafctl auth login entra'
```

Solution: Run `scafctl auth login entra` to authenticate.

### Token Expired

If you see:

```
credentials expired: please run 'scafctl auth login entra'
```

Your refresh token has expired (typically after 90 days of inactivity). Log in again:

```bash
scafctl auth login entra
```

### Wrong Tenant

If you're getting 401 errors but you're authenticated, you may be authenticated to the wrong tenant:

```bash
# Check current auth status
scafctl auth status entra

# Log out and log in to the correct tenant
scafctl auth logout entra
scafctl auth login entra --tenant correct-tenant-id
```

### Scope Issues

If you're getting 403 (Forbidden) errors, the token may not have the required permissions:

1. Ensure you're using the correct scope (e.g., `https://graph.microsoft.com/.default`)
2. Verify your Azure application has the required API permissions
3. For some APIs, admin consent may be required

### Checking Token Claims

To debug token issues, get a token and decode it:

```bash
# Get the token
TOKEN=$(scafctl auth token entra --scope "https://graph.microsoft.com/.default" -o json | jq -r '.accessToken')

# Decode the token (using jwt-cli or online decoder)
echo $TOKEN | jwt decode -

# Or decode just the payload (base64)
echo $TOKEN | cut -d. -f2 | base64 -d 2>/dev/null | jq .
```

### Debug Logging

Enable debug logging to see detailed auth information:

```bash
scafctl --log-level -1 auth status entra
scafctl --log-level -1 run solution -f mysolution.yaml
```

### Secret Store Issues

scafctl uses your system's secret store (Keychain on macOS, Windows Credential Manager, or Secret Service on Linux). If you're having issues:

- **macOS**: Check Keychain Access for `scafctl.auth.entra.*` entries
- **Windows**: Check Credential Manager for `scafctl.auth.entra.*` entries
- **Linux**: Ensure `gnome-keyring` or `kwallet` is running

---

## Next Steps

- Explore the [HTTP provider documentation](resolver-tutorial.md#working-with-http-apis) for more HTTP examples
- See [examples/resolvers/](../examples/resolvers/) for sample solutions
- Read the [design document](design/auth.md) for architecture details
