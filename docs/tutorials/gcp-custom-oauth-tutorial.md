---
title: "GCP Custom OAuth Client Setup"
weight: 41
---

# GCP Custom OAuth Client Setup

By default, `scafctl auth login gcp` runs a native browser OAuth flow using Google's well-known ADC client credentials — the same ones used by `gcloud auth application-default login`. This works out of the box with **no gcloud installation required**.

You may want to set up your own OAuth client if:

- Your organization restricts which OAuth client IDs are allowed
- You need specific OAuth consent screen branding or approval
- You want full control over the client lifecycle and scopes

> **Note:** If you were previously seeing `invalid_grant - reauth related error (invalid_rapt)` errors, upgrading scafctl to the latest version resolves this — the native browser OAuth flow is no longer affected by gcloud's RAPT token expiry.

## Prerequisites

- [Google Cloud SDK (`gcloud`)](https://cloud.google.com/sdk/docs/install) installed and authenticated
- A GCP project where you have permissions to create OAuth credentials
- `scafctl` installed and available in your PATH

## Step 1: Select or Create a GCP Project

Pick an existing project or create a new one to host the OAuth client:

```bash
# List existing projects
gcloud projects list

# Or create a new one
gcloud projects create my-scafctl-project --name="scafctl OAuth"

# Set your working project
gcloud config set project my-scafctl-project
```

## Step 2: Configure the OAuth Consent Screen

Before creating an OAuth client, you must configure the consent screen. This defines what users see when they authenticate.

```bash
# Enable the required API
gcloud services enable iap.googleapis.com

# For an internal app (only users in your org):
gcloud alpha iap oauth-brands create \
  --application_title="scafctl" \
  --support_email="your-email@example.com"
```

> **Note:** If your project has never had an OAuth consent screen configured, you may need to set it up via the [Google Cloud Console](https://console.cloud.google.com/apis/credentials/consent) the first time. Choose **Internal** (org-only) or **External** (any Google account) depending on your use case.

### Console Alternative

If the `gcloud` commands above don't work for your setup (the consent screen API has limited CLI support), configure it in the Console:

1. Go to [APIs & Services > OAuth consent screen](https://console.cloud.google.com/apis/credentials/consent)
2. Select **Internal** (recommended for org use) or **External**
3. Set the app name to `scafctl`
4. Add your support email
5. Add scopes:
   - `openid`
   - `https://www.googleapis.com/auth/cloud-platform`
   - `https://www.googleapis.com/auth/userinfo.email`
6. Save

## Step 3: Create the OAuth Client ID

Create a **Desktop application** OAuth client:

```bash
gcloud alpha iap oauth-clients create \
  "projects/$(gcloud config get-value project)/brands/$(gcloud config get-value project)" \
  --display_name="scafctl-cli"
```

> **Note:** The `gcloud alpha iap oauth-clients` commands may have limited availability. The recommended approach is to use the Console.

### Console Approach (Recommended)

1. Go to [APIs & Services > Credentials](https://console.cloud.google.com/apis/credentials)
2. Click **Create Credentials** > **OAuth client ID**
3. Set **Application type** to **Desktop app**
4. Set **Name** to `scafctl-cli`
5. Click **Create**
6. Note the **Client ID** and **Client Secret**

The output will look like:

```
Client ID:     123456789-abc123def456.apps.googleusercontent.com
Client Secret: GOCSPX-xxxxxxxxxxxxxxxxxxxxxxxxxx
```

> **Note:** Despite the name, the client secret for desktop OAuth apps is **not confidential** — it is embedded in the application. This is standard for public OAuth clients (see [Google's documentation](https://developers.google.com/identity/protocols/oauth2/native-app)).

## Step 4: Configure scafctl

### Option A: Config File (Recommended)

Add the OAuth client to your scafctl configuration file. The config file is typically located at `~/.config/scafctl/config.yaml`:

```yaml
auth:
  gcp:
    clientId: "123456789-abc123def456.apps.googleusercontent.com"
    clientSecret: "GOCSPX-xxxxxxxxxxxxxxxxxxxxxxxxxx"
    defaultScopes:
      - "openid"
      - "https://www.googleapis.com/auth/cloud-platform"
      - "https://www.googleapis.com/auth/userinfo.email"
```

Then log in normally:

```bash
scafctl auth login gcp
```

### Option B: CLI Flag (One-off)

Pass the client ID directly on the command line:

```bash
scafctl auth login gcp --client-id "123456789-abc123def456.apps.googleusercontent.com"
```

> **Note:** When using the `--client-id` flag without a config file, the client secret will be empty. This works for OAuth clients configured as public clients but may fail if Google requires the secret. The config file approach is preferred because it allows setting both `clientId` and `clientSecret`.

## Step 5: Authenticate

With the custom client configured, run:

```bash
scafctl auth login gcp
```

This will:

1. Start a local HTTP server on a random port
2. Open your browser to Google's OAuth consent page
3. After you approve, exchange the authorization code (with PKCE) for tokens
4. Store the **refresh token** in your system's secret store (Keychain on macOS, Credential Manager on Windows, Secret Service on Linux)
5. Cache the **access token** for immediate use

### Verify Authentication

```bash
scafctl auth status gcp
```

Expected output:

```
Handler   Status          Identity              IdentityType   Expires
gcp       Authenticated   user@example.com      user           2026-02-20 11:35:00
```

## How It Differs from the Default

| Feature | Default (built-in ADC client) | Custom OAuth Client | gcloud ADC (`--flow gcloud-adc`) |
|---------|-------------------------------|---------------------|----------------------------------|
| Requires gcloud installed | No | No | Yes |
| Refresh token storage | scafctl's secret store | scafctl's secret store | gcloud's ADC file |
| Affected by RAPT expiry | No | No | Yes |
| Custom OAuth consent screen | No | Yes | No |
| Custom scopes | Yes | Yes | Limited |
| Service account impersonation | Via flag | Via flag or config | Via flag |

## Service Account Impersonation

You can combine a custom OAuth client with service account impersonation. This lets you authenticate as yourself but act as a service account:

```yaml
auth:
  gcp:
    clientId: "123456789-abc123def456.apps.googleusercontent.com"
    clientSecret: "GOCSPX-xxxxxxxxxxxxxxxxxxxxxxxxxx"
    impersonateServiceAccount: "deploy@my-project.iam.gserviceaccount.com"
    defaultScopes:
      - "openid"
      - "https://www.googleapis.com/auth/cloud-platform"
```

Or via the CLI:

```bash
scafctl auth login gcp --impersonate-service-account deploy@my-project.iam.gserviceaccount.com
```

## Configuration Reference

| Field | Description | Default |
|-------|-------------|---------|
| `auth.gcp.clientId` | OAuth 2.0 client ID for interactive auth | *(empty — uses gcloud ADC)* |
| `auth.gcp.clientSecret` | OAuth 2.0 client secret | *(empty)* |
| `auth.gcp.defaultScopes` | Scopes requested during login | `openid`, `cloud-platform` |
| `auth.gcp.impersonateServiceAccount` | Service account email to impersonate | *(empty)* |
| `auth.gcp.project` | Default GCP project ID | *(empty)* |

## Troubleshooting

### `invalid_grant - reauth related error (invalid_rapt)`

This error only occurs when using `--flow gcloud-adc` (gcloud's ADC file). Fix by either:

1. Re-authenticating gcloud: `gcloud auth application-default login`, then `scafctl auth login gcp --flow gcloud-adc`
2. Using the default flow instead (no `--flow` flag needed): `scafctl auth login gcp`

### `invalid_client` Error

The client ID or secret is incorrect. Verify them in the [Google Cloud Console](https://console.cloud.google.com/apis/credentials).

### `redirect_uri_mismatch` Error

This typically means the OAuth client is not configured as a **Desktop app**. Verify the application type in the Console — it must be "Desktop" for the local redirect flow to work.

### `access_denied` Error

The user declined consent, or the OAuth consent screen is not configured. Ensure:

- The consent screen is set up (Step 2)
- The required scopes are added to the consent screen
- If the app is **Internal**, the user is in the org

### Browser Doesn't Open

If the browser doesn't open automatically, check the terminal output for a URL you can copy and paste manually. You can also try setting the `BROWSER` environment variable.

## Next Steps

- [Authentication Tutorial](auth-tutorial.md) — Full authentication guide for all providers
- [Provider Reference](provider-reference.md) — Using auth tokens in HTTP providers
- [Config Tutorial](config-tutorial.md) — Managing scafctl configuration
