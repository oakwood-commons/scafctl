# Auth Examples

This directory contains examples and cheat-sheets for the `scafctl auth` commands.

---

## Quick Reference

### Login

```bash
# Entra ID (browser OAuth + PKCE — default)
scafctl auth login entra

# GitHub (browser OAuth + PKCE — default)
scafctl auth login github

# GCP (browser OAuth)
scafctl auth login gcp

# Non-interactive flows
scafctl auth login entra --flow device-code          # headless / SSH fallback
scafctl auth login entra --flow service-principal   # requires AZURE_* env vars
scafctl auth login github --flow device-code        # headless / SSH fallback
scafctl auth login github --flow pat                # requires GITHUB_TOKEN or GH_TOKEN
scafctl auth login github --flow github-app         # requires GitHub App credentials
scafctl auth login gcp --flow service-principal     # requires GOOGLE_APPLICATION_CREDENTIALS
scafctl auth login gcp --flow gcloud-adc            # uses existing gcloud ADC file

# Idempotent — skip if already authenticated (safe for scripts and CI)
scafctl auth login entra --skip-if-authenticated
scafctl auth login github --skip-if-authenticated
scafctl auth login gcp --skip-if-authenticated

# GitHub App flow with environment variables
export SCAFCTL_GITHUB_APP_ID="12345"
export SCAFCTL_GITHUB_APP_INSTALLATION_ID="67890"
export SCAFCTL_GITHUB_APP_PRIVATE_KEY_PATH="/path/to/private-key.pem"
scafctl auth login github --flow github-app

# GitHub interactive with custom callback port
scafctl auth login github --callback-port 8400
```

---

## Diagnosing Auth Problems

Run `auth diagnose` (alias: `auth doctor`) first when things go wrong.
It checks everything in one shot:

```bash
scafctl auth diagnose
```

Output example:
```
✅ [ok]   auth registry: registered handlers: [entra gcp github]
⚠️  [warn] config file: config file not found — using built-in defaults
✅ [ok]   env GITHUB_TOKEN: GitHub personal access token — set
✅ [ok]   entra: authenticated: authenticated as "user@example.com", expires in 58m
⚠️  [warn] entra: token cache: 3 cached token(s), 1 expired
⚠️  [warn] github: not authenticated — run 'scafctl auth login github'

⚠️ Diagnostics complete: 3 warning(s), 5 ok (no failures)
```

Scope checks to a single handler (faster when you only care about one provider):

```bash
scafctl auth diagnose entra
scafctl auth diagnose github
```

Also perform a live token fetch to confirm end-to-end:

```bash
scafctl auth diagnose --live-token

# Scope live-token check to one handler
scafctl auth diagnose entra --live-token
```

Get structured output for CI pipelines:

```bash
scafctl auth diagnose -o json
```

> The `clock-skew` check compares your system clock against an external time source and warns if the skew exceeds 5 minutes — a common but easy-to-miss cause of token validation failures.

---

## Checking Status with Hints

When a handler is not authenticated, `auth status` now includes a `hint`:

```bash
scafctl auth status
```

```
handler: github  authenticated: false  hint: run 'scafctl auth login github' to authenticate
```

Exit non-zero if not authenticated (for CI pre-flight):

```bash
scafctl auth status entra --exit-code
```

Exit non-zero if any token expires within a given window (`--warn-within`):

```bash
# Warn if any token expires within 10 minutes
scafctl auth status --warn-within 10m

# Combine with --exit-code for a single full pre-flight check
scafctl auth status --exit-code --warn-within 15m
```

---

## Listing and Sorting Cached Tokens

```bash
# List all cached tokens across all handlers
scafctl auth list

# Only expired tokens
scafctl auth list --expired-only

# Valid tokens only, sorted soonest-expiring first
scafctl auth list --valid-only --sort expires-at

# Sort by handler name
scafctl auth list --sort handler

# JSON output for scripting
scafctl auth list -o json

# Remove expired access tokens from the cache (keeps valid tokens and the refresh token)
scafctl auth list --purge-expired
scafctl auth list entra --purge-expired
```

The `getTokenCommand` column shows the exact `scafctl auth token` command to
retrieve each access token — copy-paste it directly into your terminal.

---

## Token Debugging

### Get a raw token (for scripting)

```bash
# Assign to a shell variable
TOKEN=$(scafctl auth token entra --scope "https://graph.microsoft.com/.default" --raw)

# Use directly inline
curl -H "Authorization: Bearer $(scafctl auth token github --raw)" https://api.github.com/user
```

### Export to the current shell (eval-compatible)

```bash
eval $(scafctl auth token gcp --scope "https://www.googleapis.com/auth/cloud-platform" --export)
echo $GCP_TOKEN      # variable is named <HANDLER>_TOKEN

eval $(scafctl auth token entra --scope "https://management.azure.com/.default" --export)
echo $ENTRA_TOKEN
```

### Emit a ready-to-run curl command

No jq or variable assignment needed:

```bash
scafctl auth token entra --scope "https://graph.microsoft.com/.default" \
    --curl --curl-url "https://graph.microsoft.com/v1.0/me"
# Prints: curl -H "Authorization: Bearer eyJ..." "https://graph.microsoft.com/v1.0/me"

# Run it immediately
scafctl auth token entra --scope "https://graph.microsoft.com/.default" \
    --curl --curl-url "https://graph.microsoft.com/v1.0/me" | bash

# No URL — uses <URL> placeholder for inspection
scafctl auth token github --curl
```

### Decode JWT header + payload (no external tools needed)

`--decode` shows both the JWT **header** (algorithm, key ID) and the **payload** (claims):

```bash
# Table format — immediately readable
scafctl auth token entra --scope "https://graph.microsoft.com/.default" --decode

# JSON format — pipe to jq for filtering
scafctl auth token entra --scope "https://graph.microsoft.com/.default" --decode -o json \
    | jq '{alg: .header.alg, kid: .header.kid, upn: .payload.upn, expires: .payload.exp_human, roles: .payload.roles}'
```

Unix timestamp fields (`exp`, `iat`, `nbf`, `auth_time`) automatically get a
`_human` companion in RFC 3339 format.

### Copy to clipboard (no terminal echo)

```bash
scafctl auth token entra --scope "https://management.azure.com/.default" --clip
# ✓ Token copied to clipboard (expires in 58m42s).
```

---

## Logging Out Safely

```bash
# Preview what would be removed (dry run)
scafctl auth logout entra --dry-run

# Preview across all handlers
scafctl auth logout --all --dry-run

# Actually log out
scafctl auth logout entra

# Log out from everything at once (prompts for confirmation)
scafctl auth logout --all

# Skip the confirmation prompt (for scripts and CI)
scafctl auth logout --all --yes
scafctl auth logout --all -y

# Force clear even if not authenticated
scafctl auth logout entra --force
```

---

## Related

- [Auth Tutorial](../../docs/tutorials/auth-tutorial.md) — full walkthrough
- [HTTP Provider with Entra](../providers/http-entra.yaml) — example HTTP call with Entra auth
- [GitHub API Provider](../providers/github-api.yaml) — example GitHub API call
