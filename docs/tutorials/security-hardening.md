---
title: Security Hardening
weight: 80
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

```yaml
# config.yaml — allow private IPs for on-premises or local dev
httpClient:
  allowPrivateIPs: true
```

### Redirect and Pagination Safety

- Each HTTP redirect target is validated against the SSRF blocklist
- Maximum 10 redirects are permitted before the request fails
- Pagination next URLs must stay on the same hostname as the original request

### TLS Verification

All HTTP connections verify TLS certificates by default. The `--insecure` flag on catalog commands disables TLS verification for local development only and should never be used in production.

## Secret Store Security

### OS Keyring

scafctl encrypts all cached secrets (auth tokens, master keys) using AES-256-GCM. The master encryption key is stored in your operating system's keyring:

| Platform | Keyring |
|----------|---------|
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

```bash
# Before clearing the keychain, export your secrets
scafctl secrets export --encrypt --output secrets-backup.enc
```

## Plugin Security

### Mandatory Digest Verification

All plugin binaries are verified against a SHA-256 digest before execution. This is **mandatory** — if no digest is available (from the lock file or catalog), the fetch fails:

```
plugin my-plugin@1.0.0: no digest available for verification;
run 'scafctl build solution' to generate a lock file with pinned digests
```

Always use lock files in production:

```bash
# Generate a lock file with pinned versions and digests
scafctl build solution -f solution.yaml

# The lock file pins exact versions and digests
cat .scafctl.lock.yaml
```

### Supply Chain Best Practices

1. **Always build lock files** before deploying solutions
2. **Commit lock files** to version control
3. **Review digest changes** in lock file diffs during code review
4. **Use trusted catalogs** — verify catalog registry URLs in config

## Authentication Security

### GitHub App Private Key Storage

The private key can be provided from three sources (checked in priority order):

| Source | Security Level | Recommendation |
|--------|:---:|---|
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

```bash
# Store the key in the secret store first
scafctl secrets set github-app-private-key < private-key.pem
```

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
  # allowPrivateIPs: false  # default, blocks SSRF
  enableCache: true
  retryMax: 3

cel:
  costLimit: 1000000
```

- [ ] Use lock files for all solutions (`scafctl build solution`)
- [ ] Store GitHub App private keys in the secret store
- [ ] Enable `requireSecureKeyring` in production
- [ ] Keep `allowPrivateIPs: false` (default) unless on-premises access is needed
- [ ] Export secrets backup before OS keychain changes
- [ ] Review lock file digest changes during code review
