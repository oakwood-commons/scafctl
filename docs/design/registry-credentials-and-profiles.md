# Registry Credentials and Auth Profiles

## Status

Draft -- Phase 1 ready for implementation

## Problem

Registry credentials come from many sources and each has its own model:

| Registry | Auth Source | Username Convention | Token Lifetime |
|----------|-----------|-------------------|---------------|
| ghcr.io | GitHub OAuth | GitHub username | ~8 hours |
| *.azurecr.io | Entra ID OAuth | `00000000-...` (zero-GUID) | ~1 hour |
| *.pkg.dev / gcr.io | GCP ADC / OAuth | `oauth2accesstoken` | ~1 hour |
| quay.io | Built-in OAuth2 | `$oauthtoken` | varies |
| Docker Hub | Username/password or PAT | literal username | long-lived |
| Custom (e.g. JFrog) | PAT, API key, or OAuth | varies | varies |

Docker itself has **no concept of profiles**. `~/.docker/config.json` stores one
credential per registry host. `docker login ghcr.io` as user A, then
`docker login ghcr.io` as user B -- B silently overwrites A. This is the
industry-wide model.

scafctl auth profiles solve a different problem -- maintaining parallel *identity
provider* credentials (e.g. work vs personal GitHub OAuth tokens). But once those
tokens are *bridged* to registry credentials, the profile dimension is lost
because the native credential store and Docker config are both keyed by host
alone.

## Decision: Don't Make Registry Credentials Profile-Aware

### Rationale

1. **Docker doesn't support it.** `docker push ghcr.io/org/image` reads one
   credential for `ghcr.io`. Adding profile-aware registry storage would create
   credentials that only scafctl can use -- Docker, Podman, Buildah, Skopeo,
   crane, and oras would all ignore them.

2. **The active profile already solves this for scafctl operations.** Catalog
   pull/push resolves the auth handler at runtime via `BridgeAuthToRegistry()`.
   The handler respects the active profile. The bridge is only for Docker/Podman
   interop.

3. **Short-lived tokens make storage less important.** GCP and Entra tokens
   expire in ~1 hour. GitHub in ~8 hours. The real credential is the refresh
   token, which is already profile-aware.

### Model

**Rule: Bridged registry credentials always reflect the last `auth login` for
that handler.** This matches `docker login` behavior -- last login wins.

| Concern | Model | Profile-aware? |
|---------|-------|---------------|
| Auth handler tokens (OAuth refresh) | Per (handler, profile) | Yes |
| scafctl catalog pull/push | Handler token at runtime | Yes (via active profile) |
| scafctl provider operations | Handler token at runtime | Yes (via `--auth-profile`) |
| `registries.json` (native store) | Per host | No (last login wins) |
| Docker/Podman `config.json` | Per host | No (last login wins) |
| `docker-credential-scafctl` | Per host | Phase 3 (profile-aware) |

## Current Gaps

### Storage metadata

`NativeCredential` stores only `username`, `password`, and `containerAuthFile`.
No record of which handler or profile bridged the credential. This makes it
impossible to show "ghcr.io credentials came from github/work" in `auth status`.

### Catalog pull/push profile injection

`RemoteCatalogConfig` accepts an `AuthHandler` but not a profile. The `ctx`
passed to `BridgeAuthToRegistry()` in `remote.go` does not have a profile
injected via `auth.WithProfile()`. This means catalog operations always use the
handler's built-in profile unless the CLI layer sets the profile on context
before constructing the remote catalog.

### Credential helper

`docker-credential-scafctl get` reads from the native store or secrets store.
It does not invoke auth handlers and has no profile awareness. It returns stale
bridged credentials rather than fresh tokens.

### `auth status` display

All row types (auth handlers, catalogs, registries) are in one flat table with
a `kind` column. Registry rows don't show the username or which handler/profile
bridged them. With multiple profiles this becomes noisy and hard to scan.

### Credential source reporting

`CredentialWithSource()` returns a source string like "native credential store"
but does not include profile or handler info. When debugging "why did my pull
fail?", there is no way to see which profile's token was used.

## Implementation Plan

### Phase 1: Transparency (implement now)

Make the existing model visible and predictable. No storage format changes.

#### 1a. Add bridge metadata to `NativeCredential`

Add `Handler` and `Profile` fields to `NativeCredential`:

~~~go
type NativeCredential struct {
    Username          string `json:"username"`
    Password          string `json:"password,omitempty"`
    ContainerAuthFile string `json:"containerAuthFile,omitempty"`
    Handler           string `json:"handler,omitempty"`
    Profile           string `json:"profile,omitempty"`
}
~~~

Set these in `bridgeAuthToRegistryPostLogin()`. Backward compatible -- old
entries just have empty handler/profile fields.

#### 1b. Warn on credential replacement

When bridging overwrites a credential with a different username, warn:

~~~
WARNING: Replacing ghcr.io credentials (was: abaker9_ford, now: abaker-9)
~~~

#### 1c. Improve `auth status` display

Split the flat table into sections using manual `Writer` headers + multiple
`kvx.Write()` calls:

~~~
Auth Handlers
+-------------------------------------------------------------------+
| Handler  Profile            Status          User             ... |
|-------------------------------------------------------------------|
| entra    built-in (active)  authenticated   abaker9@ford.com ... |
| github   work (active)      authenticated   abaker9@ford.com ... |
| gcp      built-in (active)  authenticated   gcloud ADC       ... |
+-------------------------------------------------------------------+

Registry Credentials
+----------------------------------------------------------+
| Registry          User              Source               |
|----------------------------------------------------------|
| ghcr.io           abaker9_ford      github / work        |
| us-docker.pkg.dev oauth2accesstoken gcp / built-in       |
+----------------------------------------------------------+
~~~

Default view hides unauthenticated profiles. `--all` shows everything.

For structured output (`-o json/yaml`), emit a flat array of all status entries
(handlers, catalogs, registries combined). Each entry has a `kind` field to
distinguish its type.

#### 1d. Improve login output (done)

Consolidated success line with profile:

~~~
OK: Logged in to GitHub as abaker-9 [profile: personal]
-> Registry credentials stored for ghcr.io (user: abaker-9, via github handler)
~~~

#### 1e. Inject profile into catalog operations

Pass `auth.WithProfile(ctx, profile)` into the context before constructing
`RemoteCatalog` so that `BridgeAuthToRegistry()` uses the correct profile's
token at pull/push time.

### Phase 2: Better `auth status` with kvx (after kvx changes)

Depends on:

- [kvx#62](https://github.com/oakwood-commons/kvx/issues/62) -- nested table
  rendering in detail view sections
- [kvx#63](https://github.com/oakwood-commons/kvx/issues/63) -- honor
  DisplaySchema in non-interactive output

Once both land, replace the manual sectioned output with a single DisplaySchema:

- **Interactive (`-i`)**: Card list of handlers, drill into detail with
  profile + registry sub-tables
- **Non-interactive (default)**: Sectioned output driven by the same schema
- **Structured (`-o json/yaml`)**: Unchanged

### Phase 3: Profile-aware credential helper (future)

Make `docker-credential-scafctl` resolve credentials dynamically instead of
reading stale bridged values:

~~~json
// ~/.docker/config.json
{
  "credHelpers": {
    "ghcr.io": "scafctl"
  }
}
~~~

When Docker calls `docker-credential-scafctl get` for `ghcr.io`:

1. Determine the handler for the registry (via `InferAuthHandler`)
2. Resolve the active profile for that handler (from config)
3. Call `handler.GetToken()` with that profile's context
4. Return a fresh credential to Docker

This gives Docker transparent profile-aware credentials without changing
Docker's storage model. Requires:

- Loading auth handler registry in the credential helper
- Initializing plugin handlers (latency concern for Docker operations)
- Config access for active profile resolution

## External Dependencies

| Issue | Repo | Status | Blocks |
|-------|------|--------|--------|
| [#62](https://github.com/oakwood-commons/kvx/issues/62) Nested table in detail view | kvx | Filed | Phase 2 |
| [#63](https://github.com/oakwood-commons/kvx/issues/63) DisplaySchema in non-interactive | kvx | Filed | Phase 2 |

No issues needed for plugins or the SDK:

- **scafctl-plugin-sdk**: Already has `auth.WithProfile(ctx)` /
  `auth.ProfileFromContext(ctx)`. Profile flows through context to plugin
  handlers. No struct changes needed.
- **GitHub plugin**: Profile resolution is handled host-side via
  `ProfileResolverFunc` on `HostServiceDeps` (already implemented). No plugin
  changes needed.
- **Other plugins** (GCP, Entra, exec, etc.): Same host-side resolution
  applies. No changes needed.

## Alternatives Considered

### Profile-keyed registry credentials

Store credentials as `(host, profile)` tuples. Rejected because:

- Docker/Podman can't consume them
- Requires separate Docker config files per profile (`DOCKER_CONFIG` switching)
- Adds complexity without clear user benefit

### Automatic re-bridge on profile switch

When running `auth switch github personal`, automatically re-bridge registry
credentials. Rejected for now because:

- `auth switch` is a config change, not a login -- the token might be expired
- Re-bridging requires a valid token, which might trigger a re-auth
- Silently changing Docker credentials on a config switch is surprising

Could be added later as an opt-in behavior.

### Per-profile Docker config directories

Set `DOCKER_CONFIG=~/.docker/profiles/<profile>/`. Rejected because:

- Requires users to set `DOCKER_CONFIG` before every `docker` command
- Breaks Docker Desktop credential store integration
- Too invasive for the problem being solved
