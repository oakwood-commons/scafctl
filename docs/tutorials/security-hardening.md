---
title: Security Hardening
weight: 68
---

# Security Hardening Guide

This guide covers scafctl's built-in security protections and how to configure them for production environments.

## HTTP Provider Security

### Response Body Size Limits

The HTTP provider limits the amount of data it will read from any single response to prevent denial-of-service via unbounded responses. The default limit is **100 MB**.

```yaml
# config.yaml — adjust the limit
httpClient:
  maxResponseBodySize: 104857600  # 100 MB (default)
```

The limit applies to both direct requests **and** each page in paginated requests. If a response exceeds the limit, the provider returns an error rather than consuming unbounded memory.

### SSRF Protection

Requests to private, loopback, link-local, and CGNAT IP addresses are **blocked by default**. This prevents Server-Side Request Forgery (SSRF) attacks where a malicious solution file could probe internal network endpoints or cloud metadata services (e.g., `169.254.169.254`).

Blocked ranges:

- `10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16` (RFC 1918)
- `127.0.0.0/8` (loopback), `::1/128` (IPv6 loopback)
- `169.254.0.0/16` (link-local / cloud metadata)
- `100.64.0.0/10` (CGNAT)
- `fc00::/7`, `fe80::/10` (IPv6 private/link-local)

#### Enforcement happens when the connection is made

The check runs at **dial time**, against the IP address the connection is
actually opened to -- not against the text of the URL beforehand. This matters
because a URL-only check can be defeated by a hostname:
`https://internal.example.com/` is not an IP literal, so it passes a string
inspection, and only then does the HTTP client resolve it to `10.0.0.5` and
connect. Checking the resolved address closes that gap, and closes DNS
rebinding with it -- there is no window between the check and the connection
for an answer to change.

One consequence worth knowing: the same check applies on every redirect hop
and to every policy-protected request scafctl makes -- solution fetches, the
`http` provider, and parameter fetches -- because it lives in the dialer
rather than in any one call site. It does not govern every outbound request
scafctl makes: a few internal subsystems, such as catalog enumeration and the
OAuth handlers, use their own plain clients and are not fetching
caller-supplied URLs, so this policy does not apply to them.

#### Permitting specific destinations

Use `allowedPrivateCIDRs` to name the ranges an on-premises deployment actually
needs. This is the preferred control: it grants exactly the destinations you
list and nothing else.

```yaml
# config.yaml -- reach an internal artifact host, and nothing else private
httpClient:
  allowedPrivateCIDRs:
    - 10.42.0.0/16      # a CIDR block
    - 192.168.1.50      # a bare address means just that host
```

`allowPrivateIPs: true` remains available and unblocks **every** private range
at once. Prefer a narrow `allowedPrivateCIDRs` list: `allowPrivateIPs` is a
blunt instrument, and in a deployment that fetches caller-supplied URLs it
hands your entire internal network to whoever supplies them.

```yaml
# config.yaml -- broad access, for local development only
httpClient:
  allowPrivateIPs: true
```

#### Relaxing the policy for local development only

Every setting above is also readable from the environment, so a developer can
reach a service on `localhost` without hand-editing a configuration file:

```bash
# reach anything private, for one command
SCAFCTL_HTTPCLIENT_ALLOWPRIVATEIPS=true scafctl lint -f http://127.0.0.1:8080/solution.yaml

# or stay narrow, even locally -- separate multiple ranges with commas
SCAFCTL_HTTPCLIENT_ALLOWEDPRIVATECIDRS=127.0.0.0/8,10.42.0.0/16 scafctl run
```

Substitute your binary's own prefix for `SCAFCTL_` when embedding.

Note that an environment variable relaxes the policy for that process, but it is
not automatically temporary: `scafctl config set` rewrites the whole
configuration from the values currently loaded, so running it while the variable
is set bakes the relaxation into `config.yaml`, where it outlives the variable.
Check the file afterwards if you have both in play.

This is a **local-development** convenience. A deployed server should carry its
policy in configuration, where it is reviewable, rather than in the environment
of whoever happened to start the process. `scafctl serve` prints a warning to
stderr at startup when `allowPrivateIPs` is enabled, however it was set, so a
permissive setting inherited from a local config or a stray environment variable
cannot widen a deployment silently. The warning goes to stderr rather than
through the logger deliberately: the default logging level discards informational
messages, and an exposure warning must not be silenced by a setting that exists
to quiet operational noise. A narrow `allowedPrivateCIDRs` list is deliberate
by construction and does not warn. Note also that neither mechanism can reach
cloud metadata -- that remains blocked regardless.

**When both are set, `allowedPrivateCIDRs` wins** and the client reaches only
the ranges you listed. This is deliberate: adding an allowlist to an existing
`allowPrivateIPs: true` configuration should *tighten* it, not be silently
ignored. A present but empty list (`allowedPrivateCIDRs: []`) means "no
exceptions" and also overrides `allowPrivateIPs`; omit the field entirely to
leave `allowPrivateIPs` in force. That distinction survives `scafctl config
set` and any other rewrite of the file -- an empty list is written back as
`allowedPrivateCIDRs: []`, not dropped, so a configuration that reads as
restrictive cannot quietly reload as permissive.

#### Behind an HTTP proxy

Proxy routing (`HTTP_PROXY`/`HTTPS_PROXY`) is **disabled by default**. A
proxied request is dialed to the proxy, not the target, so the dial-time
check above never runs for that hop -- the target is instead checked once
against a local DNS answer before the request is handed to the proxy, and a
proxy whose own resolution differs (split-horizon DNS, a rebind between check
and hand-off) could reach somewhere that local check never saw. Rather than
accept that gap silently, no proxy is used until you say the configured proxy
can be trusted:

```yaml
httpClient:
  trustedProxy: true
```

With a trusted proxy in use, scafctl still resolves the target hostname
locally first and refuses one that resolves into blocked space. That **fails
closed**: in a proxy-only environment with no direct resolver, every request
is refused. If the proxy itself enforces egress policy and target names may
not resolve locally at all, additionally relax just that case:

```yaml
httpClient:
  trustedProxy: true          # required: re-enables proxy routing
  trustProxyResolution: true  # optional: also allow a target that fails to resolve locally
```

`trustProxyResolution` has no effect on its own -- without `trustedProxy:
true`, proxy routing stays disabled and nothing is ever forwarded to a proxy.

#### Cloud metadata is never reachable

**Cloud metadata addresses can never be permitted.** `169.254.169.254`,
`169.254.170.23` (EKS Pod Identity), and `100.100.100.200` (Alibaba) stay
blocked under every configuration, including `allowPrivateIPs: true` and an
`allowedPrivateCIDRs` entry that covers them. Reaching them yields instance
credentials, so there is no configuration in which allowing them is correct.

The exception is the trusted-proxy path. `trustedProxy: true` is what
re-enables proxy routing at all, and doing so hands the actual connection
decision to the proxy -- even when local DNS resolves a target outside
blocked space, the proxy dials it, and the guarantee above becomes the
proxy's to keep, not this client's. `trustProxyResolution: true` widens that
further, letting a target that fails to resolve locally through as well. A
proxy that will resolve a hostname to a metadata or private address defeats
the guarantee either way. Enable either setting only when the proxy itself
blocks those destinations.

A denied request names the setting that would permit it, so an operator hitting
a legitimate internal endpoint is not left guessing:

```
blocked by SSRF policy: ... (to permit this destination, add its address range
to httpClient.allowedPrivateCIDRs; cloud metadata addresses can never be
permitted)
```

A malformed entry in `allowedPrivateCIDRs` is rejected at startup rather than
skipped, so a typo cannot silently narrow the allowlist you thought you had.

#### Upgrading: what changed and what may break

Enforcement moved from a check on the URL *text*, performed before the request,
to a check on the address actually connected to. Four behaviors change:

1. **A hostname that resolves into private space is now blocked.** Previously
   only IP literals were caught, so `https://internal.example.com/` pointing at
   `10.0.0.5` was allowed through. This is the hole the change closes, and it is
   the most likely source of a newly-failing solution.
2. **Every redirect hop is checked.** A public URL that redirects into private
   space is now refused at the redirect.
3. **Requests through a proxy fail closed** when the target hostname does not
   resolve locally -- see `trustProxyResolution` above.
4. **Cloud metadata is unreachable even with `allowPrivateIPs: true`.**
   Previously that setting skipped the address check entirely, which left
   `169.254.169.254` (and the other metadata addresses) reachable. They are now
   refused under every configuration, including `0.0.0.0/0`. A solution that
   deliberately read instance metadata over HTTP will stop working and cannot be
   re-enabled by configuration.

If a solution that worked before now fails with `blocked by SSRF policy`, the
destination was reaching private address space. Decide whether that was
intended; if it was, name the specific range:

```yaml
httpClient:
  allowedPrivateCIDRs:
    - 10.42.0.0/16   # not allowPrivateIPs: true
```

Reach for `allowPrivateIPs: true` only for local development. If you already had
it set, consider replacing it with the narrower list -- and note that adding the
list now takes precedence over it.

### Redirect and Pagination Safety

- Every redirect hop is checked at dial time, so a redirect into private space is refused even when the original URL was public
- Maximum 10 redirects are permitted before the request fails
- Pagination next URLs must stay on the same hostname as the original request

### TLS Verification

All HTTP connections verify TLS certificates by default. The `--insecure` flag on catalog commands disables TLS verification for local development only and should never be used in production.

## Secret Store Security

### OS Keyring

scafctl encrypts all cached secrets (auth tokens, master keys) using AES-256-GCM. The master encryption key is stored in your operating system's keyring:

| Platform | Keyring |
| ---------- | --------- |
| macOS | Keychain |
| Linux | Secret Service (GNOME Keyring / KWallet) |
| Windows | Credential Manager |

### Requiring the Secure Keyring

When the OS keyring is unavailable (e.g., headless CI, containers), scafctl falls back to a file-based or environment-variable-based master key with a logged warning. To **prevent this insecure fallback**, enable `requireSecureKeyring`:

```yaml
# config.yaml
settings:
  requireSecureKeyring: true
```

Or via environment variable:

```bash
export SCAFCTL_REQUIRE_SECURE_KEYRING=true
```

When enabled, scafctl will fail with a clear error instead of falling back to insecure storage:

```
OS keyring is unavailable and settings.requireSecureKeyring is enabled;
insecure keyring backend "file" would be used — refusing to proceed.
```

### Secret Export Encryption

Exported secrets (`scafctl secrets export --encrypt`) use:

- **PBKDF2-HMAC-SHA256** with 600,000 iterations for key derivation
- **AES-256-GCM** for authenticated encryption
- A unique random salt and nonce per export

The minimum accepted iteration count for decryption is 600,000 to prevent KDF downgrade attacks from tampered export files.

### Avoiding Orphaned Secrets

If the OS keychain is cleared or reset (e.g., OS reinstall), existing encrypted secrets become orphaned and are automatically deleted on next startup. To prevent data loss:

{{< tabs "security-hardening-cmd-1" >}}
{{% tab "Bash" %}}

```bash
# Before clearing the keychain, export your secrets
scafctl secrets export --encrypt --output secrets-backup.enc
```

{{% /tab %}}
{{% tab "PowerShell" %}}

```powershell
# Before clearing the keychain, export your secrets
scafctl secrets export --encrypt --output secrets-backup.enc
```

{{% /tab %}}
{{< /tabs >}}

## Plugin Security

### Mandatory Digest Verification

All plugin binaries are verified against a SHA-256 digest before execution. This is **mandatory** — if no digest is available (from the lock file or catalog), the fetch fails:

```
plugin my-plugin@1.0.0: no digest available for verification;
run 'scafctl package solution' to generate a lock file with pinned digests
```

Always use lock files in production:

{{< tabs "security-hardening-cmd-2" >}}
{{% tab "Bash" %}}

```bash
# Generate a lock file with pinned versions and digests
scafctl package solution -f solution.yaml

# The lock file pins exact versions and digests
cat .scafctl.lock.yaml
```

{{% /tab %}}
{{% tab "PowerShell" %}}

```powershell
# Generate a lock file with pinned versions and digests
scafctl package solution -f solution.yaml

# The lock file pins exact versions and digests
cat .scafctl.lock.yaml
```

{{% /tab %}}
{{< /tabs >}}

### Supply Chain Best Practices

1. **Always build lock files** before deploying solutions
2. **Commit lock files** to version control
3. **Review digest changes** in lock file diffs during code review
4. **Use trusted catalogs** -- verify catalog registry URLs in config
5. **Declare all plugin providers** -- solution execution requires every official/external provider to be listed in `bundle.plugins`
6. **Use `--strict` in CI** -- also require official auth handlers to be declared explicitly

### Strict Mode for CI/CD

During solution execution (`run solution`, `run resolver`, `render`), **official
providers must already be declared in `bundle.plugins`** -- there is no implicit
provider auto-resolution to guard against. The `--strict` flag extends the same
requirement to official **auth handlers** (fetched via the `identity` provider),
which would otherwise be auto-resolved at runtime:

{{< tabs "security-hardening-cmd-strict" >}}
{{% tab "Bash" %}}

```bash
# CI pipeline: also require official auth handlers to be declared
scafctl run solution -f ./solution.yaml --strict
```

{{% /tab %}}
{{% tab "PowerShell" %}}

```powershell
# CI pipeline: also require official auth handlers to be declared
scafctl run solution -f ./solution.yaml --strict
```

{{% /tab %}}
{{< /tabs >}}

Without `--strict`, official auth handlers are auto-fetched at runtime. While
convenient for development, this introduces network dependencies and potential
non-determinism in CI. Combine `--strict` with a lock file and
[strict lock mode](lock-modes-tutorial.md) for fully reproducible runs.

## Authentication Security

### GitHub App Private Key Storage

The private key can be provided from three sources (checked in priority order):

| Source | Security Level | Recommendation |
| -------- | :---: | --- |
| `privateKeySecretName` | ✅ Best | Key encrypted by OS keychain. Use in production. |
| `privateKeyPath` | ✅ Good | Key in a file. Use `chmod 600` to restrict permissions. |
| `privateKey` (inline) | ⚠️ Low | Key visible in config or env var. Use only in ephemeral CI. |

When the inline method is used, scafctl logs a security warning. Prefer `privateKeySecretName` or `privateKeyPath`:

```yaml
# Recommended: use the encrypted secret store
auth:
  github:
    appId: "12345"
    installationId: "67890"
    privateKeySecretName: "github-app-private-key"
```

{{< tabs "security-hardening-cmd-3" >}}
{{% tab "Bash" %}}

```bash
# Store the key in the secret store first
scafctl secrets set github-app-private-key < private-key.pem
```

{{% /tab %}}
{{% tab "PowerShell" %}}

```powershell
# Store the key in the secret store first
scafctl secrets set github-app-private-key < private-key.pem
```

{{% /tab %}}
{{< /tabs >}}

## Template and Expression Security

### Go Template Safety

- The `env` and `expandenv` Sprig functions are **disabled by default** to prevent templates from reading process environment variables
- Enable with `goTemplate.allowEnvFunctions: true` in config only if needed
- Templates use `text/template` (not `html/template`) — do not use for generating HTML

### CEL Expression Limits

CEL expressions are evaluated in a **sandboxed environment** with:

- **Cost limit**: 1,000,000 operations (configurable via `cel.costLimit`)
- **No file I/O**: No filesystem read/write functions
- **No network**: No HTTP or socket functions
- **No code execution**: No `exec`, `shell`, or reflection
- **Context-aware cancellation**: Inline CEL in templates respects parent timeouts

```yaml
# config.yaml — adjust CEL limits
cel:
  costLimit: 1000000  # default
```

## Production Hardening Checklist

```yaml
# config.yaml — recommended production settings
settings:
  requireSecureKeyring: true

httpClient:
  maxResponseBodySize: 104857600  # 100 MB
  # Private/loopback/link-local destinations are blocked by default.
  # Name only the ranges you actually need; prefer this over
  # allowPrivateIPs: true, which unblocks every private range at once.
  # allowedPrivateCIDRs:
  #   - 10.42.0.0/16
  enableCache: true
  retryMax: 3

cel:
  costLimit: 1000000

apiServer:
  # Bind loopback unless the server is deliberately exposed. Binding a
  # non-loopback address with auth disabled logs a startup warning.
  host: "127.0.0.1"
  port: 8080

  # Authentication is OFF by default. The API executes caller-submitted
  # solutions by design, so enable auth before exposing the server.
  auth:
    azureOIDC:
      enabled: true
      tenantId: "<tenant-id>"
      clientId: "<client-id>"

  # DNS-rebinding protection. An empty list accepts ANY Host header.
  allowedHosts:
    - "api.example.com"

  tls:
    enabled: true
    cert: "/etc/scafctl/tls/server.crt"
    key: "/etc/scafctl/tls/server.key"

  # Resource limits.
  requestTimeout: "60s"
  idleTimeout: "60s"
  maxHeaderBytes: 1048576   # 1 MB
  maxRequestSize: 10485760  # 10 MB
  maxConcurrent: 1000

  rateLimit:
    global:
      maxRequests: 100
      window: "1m"

  audit:
    enabled: true
```

- [ ] Use lock files for all solutions (`scafctl package solution`)
- [ ] Store GitHub App private keys in the secret store
- [ ] Enable `requireSecureKeyring` in production
- [ ] Leave private-address access at its default (blocked); if an internal endpoint is genuinely needed, name it in `allowedPrivateCIDRs` rather than setting `allowPrivateIPs: true`
- [ ] Export secrets backup before OS keychain changes
- [ ] Review lock file digest changes during code review

### API server (`scafctl serve`)

`scafctl serve` executes caller-submitted solutions by design. Treat it like a
build runner, not a read-only API: anyone who can reach the port and pass
authentication can make the server run a solution.

- [ ] Enable `apiServer.auth.azureOIDC` before binding a non-loopback address
- [ ] Keep `apiServer.host` at `127.0.0.1` unless the server is deliberately exposed
- [ ] Set `apiServer.allowedHosts` -- empty accepts any `Host` (no DNS-rebinding protection)
- [ ] Enable `apiServer.tls`, or terminate TLS at an authenticating proxy
- [ ] Tune `apiServer.rateLimit.global` -- a 100-request/minute per-IP limit applies by default
- [ ] Block the admin routes at the proxy when fronting the server with one -- the
      prefix follows `apiServer.apiVersion` (`/v1/admin/` by default). The startup
      warning prints the resolved path, but only when the server binds a
      non-loopback address with authentication disabled
- [ ] Enable `apiServer.audit` to retain a record of executed solutions
- [ ] Do not expose `/metrics` publicly -- it bypasses API middleware by design
