---
title: "OCI Artifact Signing & Verification (Cosign/Sigstore)"
weight: 10
---

# OCI Artifact Signing & Verification

## Overview

scafctl uses **keyless OIDC-based Sigstore/Cosign signing** to provide cryptographic proof that OCI artifacts were built by trusted identities. This covers both **plugins** (providers and auth handlers) and **solutions** pushed to the catalog. The mechanism eliminates the need for long-lived signing keys while establishing a verifiable chain of trust from build to runtime.

---

## Architecture

```
+----------------------------------------------------------+
| SCAFCTL REPO                                             |
| .github/workflows/sign-plugin.yml (reusable workflow)    |
| Consumed by external plugin/solution repos               |
| via @sign-plugin/v1                                      |
+------------------------+---------------------------------+
                         |
        +----------------+----------------+
        v                v                v
+---------------+ +---------------+ +-------------------+
| PROVIDER REPOS| | AUTH HANDLER  | | SOLUTION REPOS    |
|               | | REPOS         | |                   |
| 1. Push tag   | | 1. Push tag   | | 1. Push tag       |
| 2. Call sign  | | 2. Call sign  | | 2. Build & push   |
|    workflow   | |    workflow   | |    OCI artifact   |
| 3. OCI signed | | 3. OCI signed | | 3. Call sign      |
|    (keyless)  | |    (keyless)  | |    workflow       |
| 4. Sig stored | | 4. Sig stored | | 4. OCI signed     |
|    in Rekor   | |    in Rekor   | |    (keyless)      |
+---------------+ +---------------+ +-------------------+
                         |
                         v
              +----------------------+
              |  CATALOG / REGISTRY  |
              |  (ghcr.io/...)       |
              |  - OCI images        |
              |  - Cosign signatures |
              +----------------------+
                         |
                         v
              +------------------------------+
              |  SCAFCTL RUNTIME             |
              |  (built with -tags cosign)   |
              |                              |
              |  1. Download OCI artifact    |
              |  2. Verify digest            |
              |  3. Query Rekor for sig      |
              |  4. Validate certificate     |
              |  5. Match against policy     |
              |  6. Apply mode (warn/enforce)|
              |  7. Cache artifact           |
              +------------------------------+
```

---

## Signing Mechanism

### Keyless OIDC via Fulcio/Rekor

The system uses **GitHub Actions OIDC tokens** rather than long-lived keys:

1. **GitHub Actions generates an OIDC token** when a release workflow runs on a tagged commit
2. **Fulcio** (Sigstore CA) issues a **short-lived signing certificate** containing:
   - **Issuer**: `https://token.actions.githubusercontent.com`
   - **Identity**: The full workflow URI (e.g., `https://github.com/oakwood-commons/scafctl-plugin-auth-entra/.github/workflows/sign-plugin.yml@refs/tags/v1.2.0`)
3. **Cosign signs the OCI image** using the ephemeral certificate
4. **The signature and certificate are recorded in Rekor** (public transparency log)
5. **No private keys are managed** -- the OIDC token is the only credential

### What Gets Signed

| Artifact | Method | When |
|----------|--------|------|
| Plugin OCI image index | `cosign sign --recursive` (keyless) | Tag push in plugin repo CI |
| Solution OCI artifact | `cosign sign --recursive` (keyless) | Solution pushed to catalog via CI |
| SBOM referrer artifacts | `cosign sign` (keyless) | Automatically after primary artifact is signed |
| Individual platform binaries | `cosign sign-blob` (keyless) | Tag push alongside GoReleaser |
| scafctl release binaries | `cosign sign-blob` (keyless) | scafctl tag push |

---

## What the scafctl Repo Provides

### Reusable Workflow: `sign-plugin.yml`

The scafctl repo hosts a reusable GitHub Actions workflow at `.github/workflows/sign-plugin.yml` that all external plugin and solution repos consume:

~~~yaml
# In an external plugin repo's CI:
jobs:
  sign:
    name: Sign OCI Artifact
    needs: publish-catalog
    permissions:
      id-token: write   # Required for OIDC token (Fulcio certificate)
      packages: write   # Required to push signature to registry
    uses: oakwood-commons/scafctl/.github/workflows/sign-plugin.yml@sign-plugin/v1
    with:
      image-ref: ghcr.io/oakwood-commons/auth-handlers/entra@sha256:abc123...
~~~

The workflow:

1. Installs `cosign`
2. Authenticates to GHCR
3. Runs `cosign sign --yes --recursive "$IMAGE_REF"` (keyless OIDC)
4. Verifies the signature immediately after signing

### Versioning via Tag

The reusable workflow is pinned via a mutable tag: `sign-plugin/v1`

After updating `sign-plugin.yml` on `main`, maintainers update the tag:

~~~bash
git checkout main && git pull
git tag -f sign-plugin/v1 HEAD
git push --force-with-lease origin refs/tags/sign-plugin/v1
~~~

All plugin and solution repos referencing `@sign-plugin/v1` automatically use the updated workflow on their next release.

---

## Runtime Verification Flow

### Entry Point

Verification occurs during the OCI artifact fetch phase. For plugins, this is in `pkg/plugin/fetcher.go`; for solutions, it occurs during catalog pull when signature policy is enabled:

1. OCI artifact is downloaded from the registry
2. **Digest verification** (always performed, regardless of policy)
3. If signature policy is enabled, `verifySignature()` is called:
   - Queries Rekor transparency log for cosign signatures on the image reference
   - Validates the signing certificate chain against Fulcio root CAs
   - Extracts OIDC issuer and identity from certificate extensions
   - Matches against the configured `SignaturePolicy` (trusted issuers + identities)
4. Returns a `SignatureResult` containing issuer, identity, and signing timestamp

The same `SignatureVerifier` interface and Cosign implementation (`pkg/plugin/cosign.go`) is shared across both plugin and solution verification paths.

### Build Tag: Conditional Cosign Support

Cosign support is behind a **build tag** to avoid a ~15MB binary size increase:

- **With Cosign** (`go build -tags cosign`): Full signature verification via `pkg/plugin/cosign.go`
- **Without Cosign** (default): Stub in `pkg/plugin/cosign_stub.go` returns `ErrCosignNotAvailable` if verification is requested

Official releases include the `cosign` build tag. Embedders opt in as needed.

---

## Configuration

### Signature Policy

~~~yaml
# ~/.config/scafctl/config.yaml
plugins:
  signatures:
    mode: "warn"          # off | warn | enforce
    trustedIssuers:
      - "https://token.actions.githubusercontent.com"
    trustedIdentities:
      - "https://github.com/oakwood-commons/*"
~~~

### Verification Modes

| Mode | Behavior | Use Case |
|------|----------|----------|
| `off` (default) | Digest-only; no signature check | Development, testing |
| `warn` | Verify signature; log warning on failure but continue | Migration / rollout |
| `enforce` | Verify signature; fail hard on missing or invalid signature | Production CI |

### Embedder API

Embedders can override the policy programmatically via `RootOptions.PluginSignaturePolicy` without relying on config files.

---

## Lock File Integration

When running `scafctl build solution`, signature metadata is captured in the lock file:

~~~yaml
plugins:
  - name: entra
    kind: auth-handler
    version: 1.2.0
    digest: sha256:abc123...
    signature:
      issuer: "https://token.actions.githubusercontent.com"
      identity: "https://github.com/oakwood-commons/scafctl-plugin-auth-entra/.github/workflows/sign-plugin.yml@refs/tags/v1.2.0"
      signedAt: "2026-03-15T10:30:00Z"
~~~

This provides audit trails and drift detection. The lock file itself is not signed -- verification is policy-enforced at runtime.

---

## What Happens on scafctl Release

When a git tag is pushed to the scafctl repo (e.g., `v1.5.0`):

1. `.github/workflows/release.yml` triggers
2. Tests and integration tests run
3. GoReleaser builds multi-platform binaries
4. Cosign signs each binary with `cosign sign-blob` (keyless OIDC)
5. Binaries and signatures are published to GitHub Releases

---

## SBOM Generation & Signing

### Automatic SBOM for Solutions

When solutions are pushed to the catalog (`scafctl catalog push`), an **SPDX 2.3 JSON SBOM** is automatically generated and attached as an OCI referrer artifact. This is enabled by default and can be disabled with `--no-sbom`.

The SBOM includes:
- The root solution package with SHA-256 content digest
- Bundle layer packages (if present)
- Resolver provider dependencies (as SPDX packages)
- Plugin dependencies (as SPDX packages with kind annotation)
- SPDX relationships (DESCRIBES, CONTAINS, DEPENDS_ON)

Plugins (providers and auth handlers) do not receive auto-generated SBOMs -- they are opaque binaries whose dependency metadata is managed by their respective build systems.

### SBOM Signing

The reusable signing workflow (`sign-plugin.yml`) automatically signs all OCI referrer artifacts (including SBOMs) attached to the primary image. This ensures the SBOM has the same integrity guarantees as the artifact it describes:

~~~
Artifact (signed)
  +-- SBOM referrer (also signed)
       +-- Signature proves SBOM was attached by the same trusted CI identity
~~~

The `sign-referrers` input (default: `true`) controls this behavior. When enabled, the workflow:

1. Signs the primary OCI artifact
2. Discovers all referrer artifacts (SBOMs, attestations) via `cosign tree`
3. Signs each referrer individually with the same keyless OIDC identity
4. Verifies the primary artifact signature

This prevents an attacker from replacing a legitimate SBOM with one that hides malicious dependencies -- the referrer signature must match the same trusted identity pattern.

### CLI Usage

~~~bash
# Push with SBOM (default for solutions)
scafctl catalog push my-solution@1.0.0

# Explicitly disable SBOM
scafctl catalog push my-solution@1.0.0 --no-sbom

# Plugins skip SBOM automatically
scafctl catalog push my-provider@2.0.0 --kind provider
~~~

---

## External Services

| Service | Role | URL |
|---------|------|-----|
| Rekor | Public transparency log (stores signatures) | `https://rekor.sigstore.dev/` |
| Fulcio | Certificate authority (issues ephemeral certs) | `https://v1.fulcio.sigstore.dev/` |
| GitHub OIDC | Identity provider for keyless signing | `https://token.actions.githubusercontent.com` |

---

## Design Decisions

| Decision | Rationale |
|----------|-----------|
| Keyless OIDC, not key-based | No long-lived secrets to manage; every signature is tied to a GitHub Actions identity and specific commit |
| Build tag for cosign library | Protects default builds from binary size increase; embedders opt in |
| Three verification modes | Allows gradual rollout: `off` -> `warn` -> `enforce` |
| Glob patterns for identity matching | Simple, human-readable policy (e.g., `https://github.com/oakwood-commons/*`) |
| Signature metadata in lock files | Audit trail only; not a verification mechanism |
| Reusable workflow via mutable tag | Centralized signing logic; plugin repos always get latest without code changes |
| SBOM auto-generated for solutions | Secure-by-default; every solution in the catalog has provenance metadata without opt-in |
| SBOM signed alongside primary artifact | Prevents SBOM tampering; same trust chain as the artifact itself |
| Plugins skip SBOM generation | Opaque binaries whose dependencies are managed by their own build systems |

---

## Future Enhancements

### Signature Pinning / TOFU (Trust on First Use)

A compromised but *trusted* workflow can sign malicious artifacts that still pass policy. Adding optional `expectedIdentity` pinning per plugin in lock files would enable drift detection -- on subsequent runs, reject identity changes unless explicitly acknowledged.

### Offline Bundle Verification

`enforce` mode is unusable in air-gapped environments since it requires live access to Rekor and Fulcio. Supporting pre-fetched Rekor bundles (cosign's `--bundle` flag) stored alongside cached binaries would enable offline re-verification without relaxing policy.

### Revocation Mechanism

Once signed, a compromised plugin version remains valid indefinitely. Adding an optional `revokedDigests` list in policy configuration (or a remote revocation endpoint) would allow invalidating known-bad artifacts without waiting for a new release.

### Immutable Workflow Tags

The mutable `sign-plugin/v1` tag can be force-pushed, which is a supply chain risk for the signing workflow itself. Offering immutable semver tags (e.g., `sign-plugin/v1.0.0`) as a SHA-pinning option for high-security consumers would reduce this risk.

### Timestamp Authority (TSA) Support

If the Fulcio root CA is rotated or revoked, old signatures may become unverifiable. Adding RFC 3161 timestamping or Sigstore's TSA would ensure long-lived artifact validity independent of CA lifecycle.

### Verification Result Caching

Every `enforce` run re-verifies against Rekor, adding latency and network dependency. Caching verified signatures with a configurable TTL (e.g., 1 hour) would reduce overhead for repeated runs while maintaining security guarantees.

### Multi-Signature Support

Currently only the first valid signature is used. For high-assurance scenarios, requiring N-of-M signatures (e.g., CI signature + maintainer signature) would add defense-in-depth against single-point compromise.

### Policy Versioning and Drift Detection

Policy changes silently affect all cached plugins. Logging policy changes and optionally recording a policy hash in the lock file would enable drift detection across team members and CI environments.

### Explicit Threat Model Documentation

Adding a dedicated threat model section listing what the system protects against (and what it does not) -- such as compromised CI runners, stolen OIDC tokens, or registry poisoning -- would help operators make informed trust decisions.

---

## FAQ

### Does updating `sign-plugin/v1` invalidate previously signed plugins?

No. The `sign-plugin.yml` workflow is a **build-time tool** only. Once a plugin is signed, the signature is immutably recorded in Rekor and attached to the OCI image in the registry. Verification relies on Sigstore's public infrastructure (Fulcio root CAs + Rekor), not on the workflow source. Updating the tag only affects the next time a plugin repo triggers its release pipeline.

### What if Rekor or Fulcio are unavailable at verification time?

If the Sigstore services are unreachable and the policy mode is `enforce`, verification will fail and the plugin will not load. In `warn` mode, a warning is logged and execution continues. For air-gapped environments, use `off` mode with digest-only verification.

### Can I use my own signing keys instead of keyless?

The current implementation is keyless-only (OIDC via GitHub Actions). Key-based signing is not supported. This is intentional -- keyless signing eliminates key management burden and ties every signature to a verifiable CI identity.

### How do I verify a plugin manually outside of scafctl?

~~~bash
cosign verify \
  --certificate-oidc-issuer="https://token.actions.githubusercontent.com" \
  --certificate-identity-regexp="https://github.com/oakwood-commons/.*" \
  ghcr.io/oakwood-commons/providers/my-provider:v1.0.0
~~~

### What prevents a compromised workflow from signing a malicious plugin?

The certificate identity embedded in the signature contains the **exact workflow file path and tag ref** (e.g., `.../.github/workflows/sign-plugin.yml@refs/tags/v1.2.0`). Your `trustedIdentities` policy must match this pattern. If an attacker pushes a different workflow file, the identity won't match your policy and verification fails.

### Do I need the cosign build tag for development?

No. Without `-tags cosign`, scafctl skips signature verification entirely (returns a no-op). The `off` policy mode also skips verification regardless of build tag. You only need the tag when you want to test or enforce signature checks.

### What happens if a plugin is unsigned but my policy is set to `enforce`?

The plugin fetch fails with an error indicating no valid signature was found. The plugin will not be loaded or cached.

### Can embedders disable signing entirely?

Yes. Embedders can set `RootOptions.PluginSignaturePolicy` with `Mode: "off"` or simply omit the cosign build tag from their build. Both approaches result in no signature verification.

### How do I add a new trusted identity (e.g., a third-party provider)?

Add their workflow identity pattern to `trustedIdentities` in your config:

~~~yaml
plugins:
  signatures:
    mode: "enforce"
    trustedIssuers:
      - "https://token.actions.githubusercontent.com"
    trustedIdentities:
      - "https://github.com/oakwood-commons/*"
      - "https://github.com/my-org/my-provider/.github/workflows/release.yml@refs/tags/*"
~~~

### Why is the lock file not signed?

Lock files change frequently (version bumps, new plugins added) and are committed to source control. Signing them would create constant churn. Instead, lock files record signature *metadata* for audit purposes, and actual verification happens at runtime against the live artifact.
