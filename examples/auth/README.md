# Auth Examples

This directory contains examples and cheat-sheets for the `scafctl auth` commands.

---

## Quick Reference

### Login

```bash
# Entra ID (browser OAuth + PKCE -- default)
scafctl auth login entra

# GitHub (browser OAuth + PKCE -- default)
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

# Idempotent -- skip if already authenticated (safe for scripts and CI)
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
⚠️  [warn] config file: config file not found -- using built-in defaults
✅ [ok]   env GITHUB_TOKEN: GitHub personal access token -- set
✅ [ok]   entra: authenticated: authenticated as "user@example.com", expires in 58m
⚠️  [warn] entra: token cache: 3 cached token(s), 1 expired
⚠️  [warn] github: not authenticated -- run 'scafctl auth login github'

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

> The `clock-skew` check compares your system clock against an external time source and warns if the skew exceeds 5 minutes -- a common but easy-to-miss cause of token validation failures.

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
retrieve each access token -- copy-paste it directly into your terminal.

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

# No URL -- uses <URL> placeholder for inspection
scafctl auth token github --curl
```

### Decode JWT header + payload (no external tools needed)

`--decode` shows both the JWT **header** (algorithm, key ID) and the **payload** (claims):

```bash
# Table format -- immediately readable
scafctl auth token entra --scope "https://graph.microsoft.com/.default" --decode

# JSON format -- pipe to jq for filtering
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

### Kubernetes ExecCredential (kubectl / oc)

Emit a client-go `ExecCredential` so kubectl/oc reuse your scafctl identity:

```bash
scafctl auth token entra --scope "<cluster-scope>/.default" --exec-credential
```

scafctl auto-detects `KUBERNETES_EXEC_INFO` and emits the envelope even without
the flag. See [kubectl-exec-credential.md](kubectl-exec-credential.md) for the
full kubeconfig wiring.

To automate the kubeconfig setup, use `scafctl kube login`, which runs the handler
login and writes the cluster/user/context entries for you:

```bash
scafctl kube login prod --handler oidc --current
```

See [kube-login.md](kube-login.md) for the full login/logout workflow.

> Note: `auth token` prints the **raw token by default**; `--raw` remains an
> explicit alias. Use `-o json`/`-o yaml` for the structured metadata object.

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

## Host-Aware Login and Hostname Aliases

Some auth handlers (those that advertise the `hostname` capability, e.g. an
OpenShift/Kubernetes handler) accept a `--hostname` selector at login. The host
resolves that selector into a concrete endpoint URL **before** authenticating,
using per-handler configuration under `auth.handlers.<name>.hostname`.

Resolution precedence (first match wins):

1. **Concrete URL** -- if `--hostname` is already a URL (`https://...`), it is
   used as-is.
2. **Static alias** -- a `hostname.aliases` entry mapping the selector to a URL.
3. **Dynamic resolver** -- `hostname.resolver` fetches a live inventory and
   normalizes it into `{name, url}` entries via a CEL transform.
4. Otherwise login fails with the list of available selectors.

### Managing static aliases

```bash
# Add or update an alias
scafctl auth alias set openshift prod https://api.prod.example.com:6443

# List a handler's aliases
scafctl auth alias list openshift
scafctl auth alias list openshift -o json

# Remove an alias
scafctl auth alias remove openshift prod

# Log in using the alias
scafctl auth login openshift --hostname prod
```

Aliases are stored in your main `config.yaml` under
`auth.handlers.<handler>.hostname.aliases`. Only handlers that declare the
`hostname` capability accept aliases.

### Dynamic hostname resolution

For fleets with many clusters, configure a resolver that fetches the endpoint
inventory at login time and caches it for a TTL. See
[hostname-resolution.md](hostname-resolution.md) for a full walkthrough
(including an authenticated inventory endpoint and the CEL transform contract).

---

## Handler Lifecycle Management

Official auth handler plugins are downloaded automatically on first use.
You can also manage them explicitly:

```bash
# List registered handlers with flows and capabilities
scafctl auth handlers
scafctl auth handlers -o json

# Show details for a single handler
scafctl auth handlers github
scafctl auth handlers github -o yaml

# Pre-install a handler (useful for air-gapped environments)
scafctl auth handlers install github
scafctl auth handlers install entra

# Force re-download
scafctl auth handlers install entra --force

# Remove cached handler binary (will be re-downloaded on next login)
scafctl auth handlers remove github
scafctl auth handlers remove entra
```

---

## Related

- [Auth Tutorial](../../docs/tutorials/auth-tutorial.md) -- full walkthrough
- [kubectl Exec Credential](kubectl-exec-credential.md) -- use scafctl as a Kubernetes credential plugin
- [Kubernetes login / logout](kube-login.md) -- automate kubeconfig setup with `scafctl kube login`
- [HTTP Provider with Entra](../providers/http-entra.yaml) -- example HTTP call with Entra auth
- [GitHub API Provider](../providers/github-api.yaml) -- example GitHub API call
