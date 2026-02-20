---
title: "GCP Auth Handler"
weight: 7
---

# GCP Auth Handler Implementation Plan

## Overview

Implement a builtin GCP auth handler (`gcp`) following the established patterns from the Entra and GitHub handlers. The handler will support four authentication flows: Application Default Credentials (ADC) for interactive use, Service Account Key for CI/CD, Workload Identity Federation for GKE/cross-cloud, and GCE Metadata Server for cloud-hosted workloads. Service account impersonation will be supported across all flows.

---

## Design Decisions

### Authentication Flows

| Flow | Use Case | Mechanism |
|------|----------|-----------|
| **ADC (Application Default Credentials)** | Interactive CLI use | OAuth 2.0 authorization code + PKCE via browser, with gcloud ADC fallback |
| **Service Account Key** | CI/CD pipelines, automation | JWT assertion from JSON key file via `GOOGLE_APPLICATION_CREDENTIALS` |
| **Workload Identity Federation** | GKE, cross-cloud (AWS/Azure/OIDC) | STS token exchange for federated external tokens |
| **Metadata Server** | GCE, Cloud Run, GKE | Token from `http://metadata.google.internal/computeMetadata/v1/instance/service-accounts/default/token` |

**Flow auto-detection priority**: Workload Identity Federation > Metadata Server > Service Account Key > ADC (stored credentials).

**Rationale**: This mirrors the Entra handler's priority pattern (most specific/restricted first) and aligns with Google's own [Application Default Credentials](https://cloud.google.com/docs/authentication/application-default-credentials) resolution order.

### ADC Strategy

**Decision**: Implement our own OAuth browser flow (authorization code + PKCE) as the primary interactive flow, using Google's well-known ADC client credentials (the same ones used by `gcloud auth application-default login`). A `--flow gcloud-adc` option allows explicitly using existing gcloud ADC credentials from `~/.config/gcloud/application_default_credentials.json`.

**Rationale**:
- Own OAuth flow gives full control over the experience, token caching, and removes the gcloud CLI dependency entirely
- Using Google's public ADC client ID means no custom OAuth app setup is required for the default flow
- gcloud ADC fallback (`--flow gcloud-adc`) provides backward compatibility for users who prefer to use gcloud's credentials
- Users can still configure a custom `clientId` in the config to override the built-in default
- This matches how the Entra handler works: its own device code flow is primary, with external credentials as an opt-in fallback

**Default client credentials**: When no `clientId` is configured, scafctl uses Google's well-known ADC OAuth client:
- Client ID: `764086051850-6qr4p6gpi6hn506pt8ejuq83di341hur.apps.googleusercontent.com`
- Client Secret: `d-FL95Q19q7MQmFpd7hHD0Ty`

These are the same public credentials embedded in the gcloud CLI source code.

**OAuth Flow Details**: Unlike GitHub and Entra (which use device code), GCP's recommended interactive flow for CLI tools is authorization code + PKCE with a local redirect URI:

```
1. Start local HTTP server on random port (e.g., http://localhost:PORT)
2. Open browser to https://accounts.google.com/o/oauth2/v2/auth
   ?client_id=<id>&redirect_uri=http://localhost:PORT
   &response_type=code&scope=<scopes>
   &code_challenge=<challenge>&code_challenge_method=S256
   &access_type=offline
3. User authenticates in browser, Google redirects to localhost with ?code=<code>
4. Exchange code for tokens: POST https://oauth2.googleapis.com/token
5. Receive: { access_token, refresh_token, expires_in, token_type, scope, id_token }
```

**gcloud ADC Fallback** (`--flow gcloud-adc`): When explicitly requested, or during token resolution as the lowest-priority fallback, check for gcloud ADC at:
- `$CLOUDSDK_CONFIG/application_default_credentials.json` (if `CLOUDSDK_CONFIG` set)
- `~/.config/gcloud/application_default_credentials.json` (Linux/macOS default)
- `%APPDATA%/gcloud/application_default_credentials.json` (Windows)

The ADC file contains a refresh token and client ID/secret that can be used to mint access tokens.

### New Flow Constant: `FlowMetadata`

**Decision**: Add `FlowMetadata Flow = "metadata"` to `pkg/auth/handler.go`.

**Rationale**: The GCE Metadata Server flow is semantically distinct from both workload identity federation and service account key:
- **Workload Identity Federation** exchanges an external OIDC/SAML token for a GCP token via the STS endpoint — an explicit token exchange
- **Metadata Server** returns a token for the VM's attached service account with no token exchange involved — it's implicit, ambient credential retrieval
- Users on GCE who also have a service account key in env need `--flow metadata` to force metadata-based auth
- This matches the existing pattern where Entra distinguishes `--flow workload-identity` from `--flow service-principal`

### OAuth Client ID

**Decision**: Ship with a default public OAuth client ID for interactive (ADC) flow. Allow override via `--client-id` flag and config.

**Rationale**: This matches industry practice:
- `gcloud` CLI ships with its own client ID
- `gh` CLI and Azure CLI ship with hardcoded default client IDs
- Requiring users to create their own OAuth credentials before login creates unacceptable friction

**Setup**: Create a Google Cloud OAuth 2.0 client ID (Desktop application type) for scafctl. The client ID and secret will be hardcoded as defaults in `config.go`. Note: Google's Desktop OAuth clients have a "secret" that is [not actually confidential](https://developers.google.com/identity/protocols/oauth2/native-app) and is expected to be embedded in distributed applications.

### Default Scopes

**Decision**: Default to `openid`, `email`, `profile`, and `https://www.googleapis.com/auth/cloud-platform`.

| Scope | Purpose |
|-------|---------|
| `openid` | OpenID Connect, enables ID token for claims extraction |
| `email` | Access user's email address for claims |
| `profile` | Access user's name for claims |
| `https://www.googleapis.com/auth/cloud-platform` | Full access to GCP APIs (standard for CLI tools) |

**Rationale**: `gcloud` requests `cloud-platform` scope by default, giving broad API access. The OIDC scopes (`openid`, `email`, `profile`) are needed to populate `auth.Claims` from the ID token. Users can override via `--scope` at login time.

### Service Account Impersonation

**Decision**: Include in v1.

Service account impersonation allows any authenticated identity (user, service account, workload identity) to generate short-lived tokens for a target service account via the [IAM Credentials API](https://cloud.google.com/iam/docs/reference/credentials/rest/v1/projects.serviceAccounts/generateAccessToken).

**Use cases**:
- Developer uses their own identity but needs to act as a service account for specific API calls
- CI/CD pipeline's SA impersonates a more privileged SA for specific operations
- Cross-project access where the calling identity impersonates an SA in the target project

**Mechanism**:
```
1. Acquire source token (from any flow: ADC, SA key, WI, metadata)
2. POST https://iamcredentials.googleapis.com/v1/projects/-/serviceAccounts/<target>:generateAccessToken
   Authorization: Bearer <source_token>
   Body: { scope: [...], lifetime: "3600s" }
3. Response: { accessToken, expireTime }
```

**CLI UX**:
```bash
scafctl auth login gcp --impersonate-service-account deploy@my-project.iam.gserviceaccount.com
scafctl auth token gcp --scope https://www.googleapis.com/auth/cloud-platform
# → returns a token for deploy@my-project.iam.gserviceaccount.com
```

The impersonation target is stored in metadata so subsequent `token` and `InjectAuth` calls automatically impersonate without re-specifying.

### Token Storage

**Decision**: Follow the Entra/GitHub pattern — refresh tokens and metadata in `secrets.Store`, access tokens cached per scope.

**Rationale**: The `secrets.Store` abstraction with OS keychain backing is already proven. GCP refresh tokens (from ADC flow) are long-lived and need secure persistent storage. Access tokens from all flows are cached for performance.

### Capabilities

| Capability | Supported | Notes |
|------------|-----------|-------|
| `scopes_on_login` | Yes | Scopes specified during OAuth consent |
| `scopes_on_token_request` | Yes | Can request different scopes per token via impersonation or scope-limited token exchange |
| `tenant_id` | No | GCP has no tenant concept |
| `hostname` | No | Only `googleapis.com` endpoints |
| `federated_token` | Yes | Workload identity federation |

---

## GCP OAuth / Token Endpoints

| Endpoint | URL |
|----------|-----|
| Authorization | `GET https://accounts.google.com/o/oauth2/v2/auth` |
| Token exchange | `POST https://oauth2.googleapis.com/token` |
| Token refresh | `POST https://oauth2.googleapis.com/token` (with `grant_type=refresh_token`) |
| Token revocation | `POST https://oauth2.googleapis.com/revoke` |
| Token info | `GET https://oauth2.googleapis.com/tokeninfo?access_token=<token>` |
| User info (claims) | `GET https://openidconnect.googleapis.com/v1/userinfo` |
| STS (WI Federation) | `POST https://sts.googleapis.com/v1/token` |
| IAM Credentials (impersonation) | `POST https://iamcredentials.googleapis.com/v1/projects/-/serviceAccounts/<sa>:generateAccessToken` |
| Metadata server | `GET http://metadata.google.internal/computeMetadata/v1/instance/service-accounts/default/token` |

### ADC Flow Sequence (Authorization Code + PKCE)

```
1. Generate PKCE code_verifier (43-128 char random string)
   Compute code_challenge = BASE64URL(SHA256(code_verifier))

2. Start local HTTP server on random available port

3. Open browser to:
   https://accounts.google.com/o/oauth2/v2/auth
     ?client_id=<id>
     &redirect_uri=http://localhost:<port>
     &response_type=code
     &scope=openid email profile https://www.googleapis.com/auth/cloud-platform
     &code_challenge=<challenge>
     &code_challenge_method=S256
     &access_type=offline
     &prompt=consent

4. User authenticates in browser → redirect to localhost with ?code=<auth_code>

5. POST https://oauth2.googleapis.com/token
   Body: code=<auth_code>&client_id=<id>&client_secret=<secret>
         &redirect_uri=http://localhost:<port>
         &grant_type=authorization_code&code_verifier=<verifier>
   Response: {
     access_token, refresh_token, expires_in, token_type,
     scope, id_token
   }

6. Parse ID token (JWT) for claims: sub, email, name
7. Store refresh token and metadata via secrets.Store
8. Cache access token
```

### gcloud ADC Fallback Sequence

```
1. Check for gcloud ADC file at well-known location
2. Parse JSON: { client_id, client_secret, refresh_token, type }
3. If type == "authorized_user":
   POST https://oauth2.googleapis.com/token
     Body: client_id=<adc_client_id>&client_secret=<adc_client_secret>
           &refresh_token=<adc_refresh_token>&grant_type=refresh_token
4. Response: { access_token, expires_in, token_type, scope, id_token }
5. Cache access token (do NOT store gcloud's refresh token — it stays in gcloud's file)
```

### Service Account Key Flow Sequence

```
1. Read JSON key from $GOOGLE_APPLICATION_CREDENTIALS
   Parse: { type, project_id, private_key_id, private_key, client_email, client_id, ... }

2. Create JWT assertion:
   Header: { "alg": "RS256", "typ": "JWT", "kid": "<private_key_id>" }
   Payload: {
     "iss": "<client_email>",
     "sub": "<client_email>",
     "aud": "https://oauth2.googleapis.com/token",
     "iat": <now>,
     "exp": <now + 3600>,
     "scope": "<requested_scopes>"
   }
   Sign with private_key (RSA SHA-256)

3. POST https://oauth2.googleapis.com/token
   Body: grant_type=urn:ietf:params:oauth:grant-type:jwt-bearer&assertion=<signed_jwt>
   Response: { access_token, expires_in, token_type }

4. Cache access token
```

### Workload Identity Federation Sequence

```
1. Read external credential config from $GOOGLE_EXTERNAL_ACCOUNT or detect GKE projected token
   Config contains: { type, audience, subject_token_type, token_url, credential_source }

2. Read subject token from credential_source (file, URL, or environment variable)

3. POST https://sts.googleapis.com/v1/token
   Body: {
     grant_type: "urn:ietf:params:oauth:grant-type:token-exchange",
     audience: "<workload_identity_pool_provider>",
     scope: "https://www.googleapis.com/auth/cloud-platform",
     requested_token_type: "urn:ietf:params:oauth:token-type:access_token",
     subject_token_type: "<type from config>",
     subject_token: "<external_token>"
   }
   Response: { access_token, issued_token_type, token_type, expires_in }

4. (Optional) If impersonation configured, exchange STS token for SA token:
   POST iamcredentials.googleapis.com/.../generateAccessToken

5. Cache access token
```

### Metadata Server Flow Sequence

```
1. GET http://metadata.google.internal/computeMetadata/v1/instance/service-accounts/default/token
   Header: Metadata-Flavor: Google
   Response: { access_token, expires_in, token_type }

2. (Optional) GET http://metadata.google.internal/computeMetadata/v1/instance/service-accounts/default/email
   Header: Metadata-Flavor: Google
   Response: <service-account-email>

3. Cache access token
```

---

## File Structure

```
pkg/auth/gcp/
├── handler.go              # Main handler implementing auth.Handler, flow routing, impersonation
├── handler_test.go         # Handler-level tests
├── config.go               # Config struct, defaults, validation
├── config_test.go
├── adc_flow.go             # Browser OAuth (authorization code + PKCE), local redirect server
├── adc_flow_test.go
├── gcloud_adc.go           # Detect/load existing gcloud ADC credentials
├── gcloud_adc_test.go
├── service_account.go      # Service account key JWT assertion flow
├── service_account_test.go
├── workload_identity.go    # STS token exchange for federated tokens
├── workload_identity_test.go
├── metadata.go             # GCE metadata server token acquisition
├── metadata_test.go
├── impersonation.go        # SA impersonation via IAM Credentials API
├── impersonation_test.go
├── token.go                # TokenMetadata, credentials storage, claims extraction, JWT helpers
├── token_test.go
├── cache.go                # Token caching (reuse Entra/GitHub pattern)
├── cache_test.go
├── http.go                 # HTTP client interface for testability
├── http_test.go
└── mock.go                 # Test mocks (MockHTTPClient)
```

---

## Implementation Tasks

### Phase 1: Core Handler Skeleton (Tasks 1-3)

| # | Task | Files | Description |
|---|------|-------|-------------|
| 1 | Create handler skeleton | `config.go`, `handler.go` | `Config` struct with defaults (client ID, scopes), `Handler` struct implementing `auth.Handler` interface, `New()` constructor with functional options pattern (`WithConfig`, `WithSecretStore`, `WithHTTPClient`) |
| 2 | Implement HTTP client abstraction | `http.go`, `mock.go` | Testable `HTTPClient` interface with `PostForm`, `Get`, `Do` methods. `DefaultHTTPClient` with 30s timeout. `MockHTTPClient` with request recording and response queue |
| 3 | Implement token cache | `cache.go`, `token.go` | Disk-based token caching via `secrets.Store` with `scafctl.auth.gcp.token.<base64url(scope)>` naming. `TokenMetadata` struct. Generic `getCachedOrAcquireToken` helper for cache-check → acquire → cache pattern |

### Phase 2: Authentication Flows (Tasks 4-7)

| # | Task | Files | Description |
|---|------|-------|-------------|
| 4 | Implement Service Account Key flow | `service_account.go` | Read JSON key from `GOOGLE_APPLICATION_CREDENTIALS`, create JWT assertion (RS256), exchange for access token. `HasServiceAccountCredentials()` detection. Uses `getCachedOrAcquireToken` |
| 5 | Implement Metadata Server flow | `metadata.go` | GET token from `http://metadata.google.internal/...` with `Metadata-Flavor: Google` header. Probe-based detection (HEAD request with short timeout). `GCE_METADATA_HOST` override for testing. Uses `getCachedOrAcquireToken` |
| 6 | Implement Workload Identity Federation flow | `workload_identity.go` | Read external credential config from `GOOGLE_EXTERNAL_ACCOUNT` or detect GKE projected token. STS token exchange via `sts.googleapis.com/v1/token`. Uses `getCachedOrAcquireToken` |
| 7 | Implement ADC (browser OAuth) flow | `adc_flow.go`, `gcloud_adc.go` | Authorization code + PKCE with local redirect server. PKCE code verifier/challenge generation. Browser launch via `open`/`xdg-open`. gcloud ADC file detection and refresh token extraction as fallback. ID token (JWT) parsing for claims |

### Phase 3: Impersonation (Task 8)

| # | Task | Files | Description |
|---|------|-------|-------------|
| 8 | Implement service account impersonation | `impersonation.go` | POST to `iamcredentials.googleapis.com/.../generateAccessToken`. Wraps any source token. Separate cache keys for impersonated tokens. Store impersonation target in `TokenMetadata` |

### Phase 4: CLI Wiring (Tasks 9-14)

| # | Task | Files | Description |
|---|------|-------|-------------|
| 9 | Add `FlowMetadata` flow constant | `pkg/auth/handler.go` | Add `FlowMetadata Flow = "metadata"` constant |
| 10 | Add GCP config to global config | `pkg/config/types.go` | Add `GCP *GCPAuthConfig` to `GlobalAuthConfig` with `ClientID`, `ClientSecret`, `DefaultScopes`, `ImpersonateServiceAccount` fields |
| 11 | Wire into root command | `pkg/cmd/scafctl/root.go` | Instantiate and register GCP handler alongside Entra and GitHub |
| 12 | Wire into run command | `pkg/cmd/scafctl/run/common.go` | Register GCP handler for provider auth injection |
| 13 | Wire into CLI auth commands | `pkg/cmd/scafctl/auth/handler.go` | Add `"gcp"` to `SupportedHandlers()`, add `getGCPHandler()` and `getGCPHandlerWithOverrides()` |
| 14 | Update login command | `pkg/cmd/scafctl/auth/login.go` | Route `gcp` handler, add `--impersonate-service-account` flag, handle `parseFlow()` for `metadata`, update help text and examples |

### Phase 5: Testing (Tasks 15-16)

| # | Task | Files | Description |
|---|------|-------|-------------|
| 15 | Write unit tests | `pkg/auth/gcp/*_test.go` | Mock HTTP for all flows, test cache, test claims extraction, test JWT assertion creation, test PKCE generation, test config validation, test impersonation chaining, test flow auto-detection priority |
| 16 | Write CLI integration tests | `tests/integration/cli_test.go` | Add `auth login gcp`, `auth status`, `auth logout gcp`, `auth token gcp` test cases |

### Phase 6: Documentation (Tasks 17-19)

| # | Task | Files | Description |
|---|------|-------|-------------|
| 17 | Update auth design doc | `docs/design/auth.md` | Mark `gcp` as implemented in handler table, add GCP-specific sections for all four flows, impersonation, and environment variables |
| 18 | Create auth tutorial | `docs/tutorials/gcp-auth-tutorial.md` | Step-by-step guide covering ADC login, service account key, workload identity, metadata server, impersonation, and token debugging |
| 19 | Add examples | `examples/` | Example configs using `authProvider: gcp`, impersonation examples |

---

## Configuration

### Config Struct

```go
type Config struct {
    // ClientID is the OAuth 2.0 client ID for the ADC browser flow.
    ClientID string `json:"clientId,omitempty" yaml:"clientId,omitempty"`

    // ClientSecret is the OAuth 2.0 client secret (not confidential for desktop apps).
    ClientSecret string `json:"clientSecret,omitempty" yaml:"clientSecret,omitempty"`

    // DefaultScopes are the default OAuth scopes requested during login.
    DefaultScopes []string `json:"defaultScopes,omitempty" yaml:"defaultScopes,omitempty"`

    // ImpersonateServiceAccount is the target service account email for impersonation.
    // When set, all token requests will impersonate this service account.
    ImpersonateServiceAccount string `json:"impersonateServiceAccount,omitempty" yaml:"impersonateServiceAccount,omitempty"`

    // Project is the default GCP project (informational, not used for auth).
    Project string `json:"project,omitempty" yaml:"project,omitempty"`
}
```

### Defaults

```go
func DefaultConfig() *Config {
    return &Config{
        ClientID:      "<scafctl-registered-client-id>",
        ClientSecret:  "<scafctl-registered-client-secret>",
        DefaultScopes: []string{
            "openid",
            "email",
            "profile",
            "https://www.googleapis.com/auth/cloud-platform",
        },
    }
}
```

### Global Config Addition

```go
type GlobalAuthConfig struct {
    Entra  *EntraAuthConfig  `json:"entra,omitempty" ...`
    GitHub *GitHubAuthConfig `json:"github,omitempty" ...`
    GCP    *GCPAuthConfig    `json:"gcp,omitempty" ...`
}

type GCPAuthConfig struct {
    ClientID                  string   `json:"clientId,omitempty" yaml:"clientId,omitempty" doc:"OAuth 2.0 client ID for interactive authentication" example:"123456789.apps.googleusercontent.com"`
    ClientSecret              string   `json:"clientSecret,omitempty" yaml:"clientSecret,omitempty" doc:"OAuth 2.0 client secret (not confidential for desktop apps)"`
    DefaultScopes             []string `json:"defaultScopes,omitempty" yaml:"defaultScopes,omitempty" doc:"Default OAuth scopes for GCP authentication" maxItems:"20"`
    ImpersonateServiceAccount string   `json:"impersonateServiceAccount,omitempty" yaml:"impersonateServiceAccount,omitempty" doc:"Service account email to impersonate" example:"deploy@my-project.iam.gserviceaccount.com"`
    Project                   string   `json:"project,omitempty" yaml:"project,omitempty" doc:"Default GCP project ID" example:"my-project-123"`
}
```

---

## Secret Naming Convention

Following the established pattern from the Entra and GitHub handlers:

```
scafctl.auth.gcp.<type>
```

| Secret Name | Description |
|-------------|-------------|
| `scafctl.auth.gcp.refresh_token` | OAuth refresh token (from ADC browser flow) |
| `scafctl.auth.gcp.metadata` | Token metadata (claims, flow type, impersonation target, client ID, expiry) |
| `scafctl.auth.gcp.token.<scope-hash>` | Cached access tokens by scope (base64url-encoded scope string) |

### TokenMetadata Struct

```go
type TokenMetadata struct {
    Claims                    *auth.Claims  `json:"claims"`
    RefreshTokenExpiresAt     time.Time     `json:"refreshTokenExpiresAt,omitempty"`
    Flow                      auth.Flow     `json:"flow"`
    ClientID                  string        `json:"clientId,omitempty"`
    Project                   string        `json:"project,omitempty"`
    ImpersonateServiceAccount string        `json:"impersonateServiceAccount,omitempty"`
    Scopes                    []string      `json:"scopes,omitempty"`
    ServiceAccountEmail       string        `json:"serviceAccountEmail,omitempty"`
}
```

---

## Environment Variables

### Service Account Key Flow

| Variable | Description |
|----------|-------------|
| `GOOGLE_APPLICATION_CREDENTIALS` | Path to service account JSON key file |

### Workload Identity Federation Flow

| Variable | Description |
|----------|-------------|
| `GOOGLE_EXTERNAL_ACCOUNT` | Path to external account credential configuration JSON |

### Metadata Server Flow

| Variable | Description |
|----------|-------------|
| `GCE_METADATA_HOST` | Override metadata server host (for testing). Defaults to `metadata.google.internal` |

### ADC gcloud Fallback

| Variable | Description |
|----------|-------------|
| `CLOUDSDK_CONFIG` | Custom gcloud config directory. Defaults to `~/.config/gcloud` |

---

## Claims Mapping

### From ID Token (ADC Browser Flow)

GCP ID tokens are standard OIDC JWTs. Claims are extracted the same way as Entra:

| Claims Field | JWT Claim | Example |
|-------------|-----------|---------|
| `Issuer` | `iss` | `"https://accounts.google.com"` |
| `Subject` | `sub` | `"110169484474386276334"` |
| `Email` | `email` | `"user@example.com"` |
| `Name` | `name` | `"John Doe"` |
| `Username` | `email` (before @) | `"user"` |
| `IssuedAt` | `iat` | `2026-02-17T10:00:00Z` |
| `ExpiresAt` | `exp` | `2026-02-17T11:00:00Z` |

### From Userinfo Endpoint (Fallback)

When an ID token is unavailable (e.g., gcloud ADC fallback):

| Claims Field | Userinfo Field | Example |
|-------------|----------------|---------|
| `Issuer` | `"https://accounts.google.com"` (static) | `"https://accounts.google.com"` |
| `Subject` | `sub` | `"110169484474386276334"` |
| `Email` | `email` | `"user@example.com"` |
| `Name` | `name` | `"John Doe"` |
| `Username` | `email` (before @) | `"user"` |

### From Service Account Key

| Claims Field | JSON Key Field | Example |
|-------------|----------------|---------|
| `Issuer` | `"https://accounts.google.com"` (static) | `"https://accounts.google.com"` |
| `Subject` | `client_email` | `"my-sa@my-project.iam.gserviceaccount.com"` |
| `Email` | `client_email` | `"my-sa@my-project.iam.gserviceaccount.com"` |
| `ClientID` | `client_id` | `"123456789"` |
| `ObjectID` | `client_id` | `"123456789"` |

### From Metadata Server

| Claims Field | Source | Example |
|-------------|--------|---------|
| `Issuer` | `"https://accounts.google.com"` (static) | `"https://accounts.google.com"` |
| `Subject` | `email` from metadata | `"123-compute@developer.gserviceaccount.com"` |
| `Email` | `email` from metadata | `"123-compute@developer.gserviceaccount.com"` |

---

## CLI UX

### Login

```bash
# Interactive login with browser OAuth (default)
scafctl auth login gcp

# Login with specific scopes
scafctl auth login gcp --scope https://www.googleapis.com/auth/cloud-platform --scope https://www.googleapis.com/auth/compute

# Login with service account key (auto-detected from env)
export GOOGLE_APPLICATION_CREDENTIALS=/path/to/key.json
scafctl auth login gcp

# Explicitly specify service account flow
scafctl auth login gcp --flow service-principal

# Login using GCE metadata server
scafctl auth login gcp --flow metadata

# Login with workload identity federation
scafctl auth login gcp --flow workload-identity

# Login with service account impersonation
scafctl auth login gcp --impersonate-service-account deploy@my-project.iam.gserviceaccount.com
```

### Status

```bash
scafctl auth status
# Handler: gcp
# Display Name: Google Cloud Platform
# Status: Authenticated
# Identity Type: user
# Email: user@example.com
# Name: John Doe
# Scopes: openid, email, profile, https://www.googleapis.com/auth/cloud-platform
# Impersonating: deploy@my-project.iam.gserviceaccount.com

scafctl auth status gcp -o json
```

### Token

```bash
# Get access token for debugging
scafctl auth token gcp --scope https://www.googleapis.com/auth/cloud-platform

# Get token with minimum validity
scafctl auth token gcp --scope https://www.googleapis.com/auth/cloud-platform --min-valid-for 5m

# Force refresh (bypass cache)
scafctl auth token gcp --scope https://www.googleapis.com/auth/cloud-platform --force-refresh
```

### Logout

```bash
# Revoke tokens and clear stored credentials
scafctl auth logout gcp
```

---

## Service Account Impersonation Details

Impersonation is implemented as a transparent wrapper layer that intercepts all `GetToken` and `InjectAuth` calls:

### Flow

```
Any source flow (ADC, SA Key, WI, Metadata)
  → Acquire source access token
  → POST iamcredentials.googleapis.com/.../generateAccessToken
    Authorization: Bearer <source_token>
    Body: {
      scope: [<requested_scopes>],
      lifetime: "3600s"
    }
  → Response: { accessToken, expireTime }
  → Return impersonated token to caller
```

### Caching

Impersonated tokens are cached with a distinct key pattern to avoid conflicts with direct tokens:

```
scafctl.auth.gcp.token.impersonate.<target-sa-hash>.<scope-hash>
```

### Claims for Impersonated Identity

When impersonation is active, `Status()` shows both the source identity and the impersonated identity:

| Field | Value |
|-------|-------|
| `IdentityType` | Source identity type (e.g., `user`) |
| `Email` | Source email (from source token claims) |
| `ClientID` | Impersonated SA email |

### IAM Permissions Required

The source identity must have `roles/iam.serviceAccountTokenCreator` on the target service account.

---

## Token Revocation (Logout)

Unlike Entra and GitHub, GCP supports explicit token revocation:

```
POST https://oauth2.googleapis.com/revoke?token=<refresh_token>
Content-Type: application/x-www-form-urlencoded
```

The `Logout()` method will:
1. Revoke the refresh token (if stored, ADC flow only)
2. Clear all cached access tokens matching `scafctl.auth.gcp.token.*`
3. Clear metadata from `scafctl.auth.gcp.metadata`

For SA key and metadata server flows, logout only clears cached tokens (no refresh token to revoke).

---

## Error Handling

| Error | Condition | User Message |
|-------|-----------|-------------|
| `ErrNotAuthenticated` | No stored credentials, no env vars, no metadata server | `not authenticated: please run 'scafctl auth login gcp'` |
| `ErrTokenExpired` | Refresh token expired / revoked | `credentials expired: please run 'scafctl auth login gcp'` |
| `ErrFlowNotSupported` | Invalid flow for GCP | `flow "<flow>" is not supported by the gcp handler` |
| `ErrTimeout` | Browser OAuth timed out (user didn't complete) | `authentication timed out: no response received from browser` |
| `ErrUserCancelled` | User closed browser / cancelled | `authentication cancelled by user` |
| `ErrInvalidScope` | Scope not available for handler | Per-request scopes only available with impersonation or STS |
| API error | GCP returns 401/403 | `authentication failed: <GCP error message>` |
| Metadata unavailable | Not running on GCE / metadata server unreachable | `metadata server not available: not running on Google Cloud?` |
| Impersonation denied | Missing `serviceAccountTokenCreator` role | `impersonation denied: ensure source identity has roles/iam.serviceAccountTokenCreator on <target>` |

---

## Differences from Entra and GitHub Handlers

| Aspect | Entra | GitHub | GCP |
|--------|-------|--------|-----|
| **Interactive flow** | Device code | Device code | Authorization code + PKCE (browser redirect) |
| **CI/CD flow** | Service principal (client_credentials) | PAT (env var) | Service account key (JWT assertion) |
| **Workload identity** | Federated token (client_assertion) | — | STS token exchange |
| **Cloud-native ambient** | — | — | Metadata server (new `FlowMetadata`) |
| **Token revocation** | Not supported | Not supported | Supported (explicit revoke endpoint) |
| **Claims extraction** | JWT ID token parsing | `/user` API call | JWT ID token parsing OR `/userinfo` API |
| **Impersonation** | — | — | SA impersonation via IAM Credentials API |
| **Client secret** | Not needed (public client) | Not needed (public client) | Needed but not confidential (desktop app) |
| **Scopes per request** | Yes (resource-based) | No (fixed at login) | Yes (scope-based + impersonation) |

---

## Dependencies

### No New External Dependencies (Preferred)

Following the Entra handler's approach of manual OAuth implementation with raw `net/http`:

- JWT assertion signing: use Go stdlib `crypto/rsa`, `crypto/x509`, `encoding/pem`
- PKCE: use Go stdlib `crypto/sha256`, `crypto/rand`, `encoding/base64`
- Browser launch: use `os/exec` with `open` (macOS), `xdg-open` (Linux), `rundll32` (Windows)
- Local HTTP server: use Go stdlib `net/http`

This keeps the zero-external-dependency pattern established by the Entra handler.

### If Dependencies Are Acceptable (Alternative)

- `golang.org/x/oauth2/google` — provides ADC resolution, JWT config, and service account auth
- This would significantly reduce implementation effort but break the zero-dependency pattern

**Recommendation**: Stay with manual implementation for consistency with the existing handlers.

---

## Testing Strategy

### Unit Tests (Mock HTTP)

Every flow will be tested with `MockHTTPClient` + `secrets.MockStore`:

- **ADC flow**: Mock OAuth endpoints, test PKCE generation, test local redirect server, test gcloud ADC file parsing
- **Service account flow**: Test JWT assertion generation (verify header, payload, signature), test token exchange
- **Workload identity flow**: Test STS token exchange, test credential config parsing, test projected token file reading
- **Metadata server flow**: Test token acquisition, test metadata server detection (probe), test `GCE_METADATA_HOST` override
- **Impersonation**: Test impersonation wrapping for all flows, test cache key separation, test IAM API error handling
- **Token cache**: Test scope-based caching, test expiry checking, test `MinValidFor`, test `ForceRefresh` bypass
- **Config**: Test defaults, test validation, test overrides
- **Flow auto-detection**: Test priority ordering with various env var combinations

### Integration Tests (httptest.Server)

`httptest.Server`-based tests simulating real GCP endpoints (same pattern as Entra handler).

### CLI Integration Tests

Non-destructive tests in `tests/integration/cli_test.go`:
- `TestIntegration_AuthStatusGCP` — runs `auth status gcp`, checks it doesn't crash
- `TestIntegration_AuthLogoutGCPNotLoggedIn` — runs `auth logout gcp` when not logged in
- `TestIntegration_AuthLoginGCPHelp` — runs `auth login gcp --help`, verifies flags

---

## Suggested Implementation Order

1. **Phase 1 (Tasks 1-3)**: Core skeleton — handler, HTTP client, cache. All subsequent phases depend on this.
2. **Phase 2 (Tasks 4-7)**: Authentication flows. Start with Service Account Key (simplest, easiest to test with mock JWT), then Metadata Server (simple HTTP GET), then Workload Identity (STS exchange), then ADC (most complex due to browser + PKCE).
3. **Phase 3 (Task 8)**: Impersonation — wraps the flows from Phase 2.
4. **Phase 4 (Tasks 9-14)**: CLI wiring — connects everything to the CLI commands.
5. **Phase 5 (Tasks 15-16)**: Testing — unit tests should be written alongside each phase, but dedicated test hardening goes here.
6. **Phase 6 (Tasks 17-19)**: Documentation and examples.

---

## Estimated Effort

| Phase | Files | Estimated LOC (prod) | Estimated LOC (test) |
|-------|-------|---------------------|---------------------|
| Phase 1: Core skeleton | 5 | ~400 | ~300 |
| Phase 2: Auth flows | 8 | ~1,200 | ~2,000 |
| Phase 3: Impersonation | 2 | ~250 | ~400 |
| Phase 4: CLI wiring | 6 (modified) | ~200 | ~100 |
| Phase 5: Testing | — | — | ~500 (hardening) |
| Phase 6: Documentation | 3 | ~300 (docs) | — |
| **Total** | **~24 files** | **~2,350** | **~3,300** |

This is comparable to the Entra handler (~1,500 LOC prod, ~3,500 LOC test) plus the impersonation layer.

---

## References

- [Google OAuth 2.0 for Desktop Apps](https://developers.google.com/identity/protocols/oauth2/native-app)
- [Google OAuth 2.0 PKCE](https://developers.google.com/identity/protocols/oauth2/native-app#step-2:-send-a-request-to-googles-oauth-2.0-server)
- [Application Default Credentials](https://cloud.google.com/docs/authentication/application-default-credentials)
- [Service Account Key Auth](https://cloud.google.com/iam/docs/keys-create-delete)
- [Workload Identity Federation](https://cloud.google.com/iam/docs/workload-identity-federation)
- [GCE Metadata Server](https://cloud.google.com/compute/docs/metadata/overview)
- [IAM Credentials API (Impersonation)](https://cloud.google.com/iam/docs/reference/credentials/rest/v1/projects.serviceAccounts/generateAccessToken)
- [Token Revocation](https://developers.google.com/identity/protocols/oauth2/web-server#tokenrevoke)
- [Google OpenID Connect](https://developers.google.com/identity/openid-connect/openid-connect)
- [Entra Handler (reference implementation)](../../pkg/auth/entra/)
- [GitHub Handler (reference implementation)](../../pkg/auth/github/)
