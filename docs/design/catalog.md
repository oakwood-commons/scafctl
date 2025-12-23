# Catalog

This document proposes a simple, durable catalog model for scafctl that works equally well on local filesystems and remote object storage (S3/GCS/Azure Blob/HTTP). It focuses on:
- Clear folder-first layout you can browse and copy around
- Small JSON indexes for fast queries and caching
- Strong version semantics with SemVer constraints and prereleases (alpha/beta)
- Integrity metadata (digest/size/signature) and reproducible packaging

## Goals
- Store and discover versioned resources: solutions, datasources, releases, providers.
- Work locally (folder-based) and in remote buckets without behavioral drift.
- Resolve by name/kind with SemVer constraints; optionally include prereleases.
- Keep per-package state isolated (no single giant index that becomes a bottleneck).
- Enable offline mirrors and deterministic resolution via digests.

## Resource Kinds
- `solution`: A scafctl solution (its manifest plus packaged templates/assets).
- `datasource`: Data bundles that solutions can ingest.
- `release`: Software releases packaged by scafctl (e.g., binary + metadata).
- `provider`: Plugins/providers that extend scafctl.

Each resource is identified by `kind` + `name` and has one or more versions (SemVer).
## Directory Layout

At a catalog root (local folder or bucket prefix):
```
<catalog-root>/
  catalog.json                # optional, lists known packages (for discovery)
  solutions/
    my-solution/
      index.json              # per-package index of versions
      v1.2.3/
        package.tgz           # packaged artifact (format varies by kind)
        manifest.yaml         # optional, if not embedded in package
        sha256                # optional plain text digest file
        signature             # optional detached signature (e.g., cosign)
      v1.3.0-beta.1/
    package.tgz
    sha256
  datasources/
    customers/
      index.json
      v0.4.0/
    data.tgz
    sha256
  releases/
    tool-x/
      index.json
      v2.1.0/
  tool-x_windows_amd64.zip
  tool-x_darwin_arm64.tar.gz
  sha256
  providers/
    git/
      index.json
      v0.3.0-alpha.2/
        provider.tgz
        sha256
```

Notes:
- All versions live under the package directory as `v<semver>/` folders.
- `index.json` is per-package (scales better, avoids a single monolithic index file).
- An optional `catalog.json` at the root lists packages to accelerate discovery; not required for resolution once a package path is known.

## JSON Indexes
Two small JSON documents:

1) Optional top-level `catalog.json`

```
{
  "apiVersion": "catalog.scafctl.dev/v1",
  "generatedAt": "2025-12-23T00:00:00Z",
  "packages": [
    { "kind": "solution", "name": "my-solution", "path": "solutions/my-solution/" },
    { "kind": "datasource", "name": "customers", "path": "datasources/customers/" },
    { "kind": "release", "name": "tool-x", "path": "releases/tool-x/" },
    { "kind": "provider", "name": "git", "path": "providers/git/" }
  ]
}
```

2) Required per-package `index.json`

```
{
  "apiVersion": "catalog.scafctl.dev/v1",
  "kind": "solution",
  "name": "my-solution",
  "versions": {
    "1.2.3": {
      "createdAt": "2025-12-01T12:34:56Z",
      "digest": "sha256:...",
      "size": 123456,
      "path": "v1.2.3/package.tgz",
      "signatures": [
        { "type": "cosign", "path": "v1.2.3/signature" }
      ],
      "metadata": {
        "license": "Apache-2.0",
        "tags": ["prod", "kubernetes"],
        "requires": [
          { "kind": "provider", "name": "git", "constraint": ">=0.2.0 <1.0.0" }
        ]
      }
    },
    "1.3.0-beta.1": {
      "createdAt": "2025-12-10T08:00:00Z",
      "digest": "sha256:...",
      "size": 125678,
      "path": "v1.3.0-beta.1/package.tgz",
      "metadata": { "channel": "beta" }
    }
  }
}
```

Design choices:
- Versions are plain SemVer strings without a leading `v` in the JSON keys; folders use `v<semver>` names for readability.
- `metadata` is open-ended to avoid frequent schema churn.
- `requires` allows dependency constraints across kinds (optional).

## Versioning & Constraints
- Use standard SemVer 2.0.0 (major.minor.patch-pre+build).
- By default, resolution returns only stable versions (no prerelease).
- To include prereleases (alpha/beta/rc), the resolver accepts an `includePrerelease` flag or the constraint explicitly includes a prerelease (e.g., `>=1.3.0-beta.1`).
- Supported constraints (typical SemVer set): `=`, `>`, `>=`, `<`, `<=`, `~`, `^`, ranges with logical AND (`,` or space) and OR (`||`).
- Examples:
  - `^1.2.0` resolves latest `>=1.2.0 <2.0.0` stable.
  - `~1.3.2` resolves `>=1.3.2 <1.4.0`.
  - `>=1.3.0-0` with `includePrerelease=true` will consider betas.

Implementation note: Masterminds/semver (v3) handles prerelease rules well and is widely used in Go.

## Backends (Local and Remote)

Backends share the same folder layout and JSON. Resolution is path/URI agnostic.

- Local filesystem: `file:///path/to/catalog` or a plain path configured in scafctl.
- Object storage: `s3://bucket/prefix`, `gs://bucket/prefix`, `azblob://container/prefix`.
- HTTP(s) read-only catalogs: `https://example.com/catalog/` (requires listing allowed or direct known paths).

Resolver behavior:
- Read `index.json` lazily per package; cache with ETag/Last-Modified when available.
- Never require `catalog.json` for resolution; it is an optimization for discovery and UI.
- Support multiple sources with priority; first success wins. Allow pinning to a specific source.

## Integrity, Reproducibility, Security

- Store `sha256` digest and size for every version. Verify on download by default.
- Optional detached signatures (e.g., cosign). If signature exists and verification is enabled, fail on mismatch.
- Support content-addressable `blobs/sha256/<digest>` mirror as an optional optimization (advanced).
- Encourage reproducible packs: canonical tar ordering, normalized timestamps, gzip level, etc.

## Packaging Conventions (per kind)

- solution: single `package.tgz` containing `solution.yaml` + templates/assets. Alternatively permit external `manifest.yaml` if not embedded.
- datasource: `data.tgz` or a directory snapshot; keep simple tgz initially.
- release: multiple platform artifacts with predictable names and a per-version checksum manifest covering all artifacts. Recommended naming: `<name>_<version>_<os>_<arch>[_<variant>][_<libc>].<ext>`
  - OS: `linux`, `windows`, `darwin`
  - Arch: `amd64`, `arm64`, (optionally `arm`)
  - Variant (optional): e.g., `armv7`
  - Libc (optional on Linux): `gnu` or `musl`
  - Ext: `zip` on Windows, `tar.gz` elsewhere
  - Examples: `tool-x_2.1.0_windows_amd64.zip`, `tool-x_2.1.0_linux_amd64_gnu.tar.gz`, `tool-x_2.1.0_linux_amd64_musl.tar.gz`, `tool-x_2.1.0_darwin_arm64.tar.gz`
  - Include `checksums.txt` in the version folder (lines like: `<sha256>  <relative-path>`), and record its path in the index.
- provider: `provider.tgz` with plugin binary + manifest.

## CLI UX

- `scafctl catalog add-source <name> <uri>`: Add a catalog source (local path or bucket/URL) with priority.
- `scafctl catalog list [--kind solution|datasource|release|provider] [--source <name>]`: Discover packages (uses `catalog.json` when present, or falls back to directory listing where supported).
- `scafctl catalog get <kind>/<name>@<constraint> [--pre] [--source <name>]`: Resolve a version and output the artifact path/URI or fetch it locally.
- For releases, add platform selection flags (default to local runtime):
  - `--platform <os>/<arch>` (e.g., `linux/amd64`, `windows/amd64`, `darwin/arm64`)
  - `--libc <gnu|musl>` (linux only)
  - `--variant <armv7|...>` (optional)
- `scafctl publish <kind> <path> --name <pkg> --version <semver> --catalog <name> [--sign]`: Package and publish into the target catalog, update `index.json`, write digests, optionally sign.
- `scafctl catalog verify <kind>/<name>@<version> [--source <name>]`: Verify integrity and signature.

Examples:

```
# Resolve latest stable 1.x of my-solution from default sources
scafctl catalog get solution/my-solution@^1.0.0

# Include prereleases when resolving
scafctl catalog get solution/my-solution@^1.3.0 --pre

# Publish a new provider prerelease
scafctl publish provider ./dist --name git --version 0.3.0-beta.2 --catalog corp-bucket

# Get a release for the current platform (auto-detected)
scafctl catalog get release/tool-x@^2.1.0

# Get a specific platform artifact
scafctl catalog get release/tool-x@^2.1.0 --platform linux/amd64 --libc musl
```

## Resolution Algorithm (high level)

1) Determine candidate sources (configured list ordered by priority; allow override with `--source`).
2) For each source:
   - Locate package path: `<root>/<kind>/<name>/`.
   - Fetch and parse `index.json`.
   - Build a version set; if `includePrerelease=false`, drop prereleases.
   - Apply SemVer constraint; pick max satisfying version.
   - If `kind=release`, select platform artifact:
     - Determine target: flags override environment; otherwise use `runtime.GOOS`, `runtime.GOARCH`, detect libc if linux.
     - Prefer exact match `(os, arch, variant, libc)`; fall back to `(os, arch)`; finally to a universal artifact if present (`os:any`, `arch:any`).
     - Return the selected artifact descriptor (with `path`, `digest`, etc.).
3) If no match in any source, return a structured not found error with context.

## Caching and Updates

- Maintain a local cache directory (e.g., `~/.cache/scafctl/catalog/…`).
- Cache `index.json` responses with ETag/Last-Modified for HTTP; rely on object metadata/versioning for buckets when available.
- Respect a TTL (configurable) and a `--no-cache` flag for forced refresh.
- Cache resolved artifacts by digest to avoid duplicates across sources.

## Concurrency and Consistency

### The Problem

When two publishers add different versions of the same package simultaneously, they can create a race condition:

1. Publisher A reads `index.json` (contains versions 1.0.0, 1.1.0)
2. Publisher B reads `index.json` (also sees 1.0.0, 1.1.0)
3. Publisher A adds version 1.2.0 and writes `index.json`
4. Publisher B adds version 1.3.0 and writes `index.json`
5. Result: `index.json` only contains 1.3.0; version 1.2.0 is lost (orphaned files exist but index doesn't reference them)

### Solutions by Backend

#### Local Filesystem

Use file locking to serialize index updates:

- Acquire an exclusive lock on `index.json.lock` before reading
- Read, modify, write `index.json`
- Release lock
- Alternatively: atomic rename pattern with temp files
- On lock timeout (e.g., 30s), fail with a clear error asking the user to retry

Limitation: Only works for local concurrent processes; doesn't help with NFS or shared filesystems without proper locking.

#### Object Storage (S3, GCS, Azure Blob)

Use optimistic concurrency with conditional writes:

1. **Read with ETag**: Fetch `index.json` and its ETag/version metadata
2. **Upload artifacts**: Write new version folder files (these are new paths, no conflicts)
3. **Conditional write**: Update `index.json` only if ETag matches (If-Match header for S3, precondition for GCS)
4. **On conflict (412 Precondition Failed)**:
   - Re-read `index.json` (now includes other publisher's version)
   - Merge: add our version to the refreshed index
   - Retry conditional write with new ETag
   - Max retries: 3-5 attempts with exponential backoff
5. **On success**: Return

Backend-specific APIs:
- **S3**: `PutObject` with `If-Match: "<etag>"` header
- **GCS**: `storage.Writer` with `Conditions.GenerationMatch`
- **Azure Blob**: `Upload` with `BlobRequestConditions.IfMatch`

#### HTTP Catalogs

Writable HTTP catalogs must implement similar conditional write semantics:

- Support `If-Match` or `If-None-Match` headers
- Return `412 Precondition Failed` on conflict
- Provide ETag in responses for subsequent conditional writes

Read-only HTTP catalogs don't support publishing (no write operations).

### Publishing Workflow (scafctl publish)

```
1. Validate inputs (version not empty, files exist)
2. Determine package path: <catalog-root>/<kind>/<name>/
3. Acquire lock (filesystem) or fetch ETag (object storage)
4. Read index.json
5. Check if version already exists:
   - If exists with same digest: skip (idempotent)
   - If exists with different digest: fail with error
6. Upload artifact files to v<version>/ folder
7. Compute digests, generate checksums.txt
8. Add new version entry to index.json
9. Write index.json with concurrency control:
   - Filesystem: write while holding lock
   - Object storage: conditional write with ETag
   - On conflict: retry from step 4 (re-read, merge, retry)
10. Release lock / return success
```

### Conflict Resolution

**Same version, different content**: Hard error. Do not allow overwriting a published version with different content. This ensures immutability and prevents supply chain attacks.

**Different versions, concurrent publish**: Automatic merge via retry. Both versions end up in `index.json`.

**Retry limits**: After 3-5 retry attempts, fail with a clear error. This prevents infinite loops if the catalog is under heavy write load.

### Best Practices for Teams

1. **Use CI/CD pipelines**: Serialize publishes through a single automation system to avoid races.
2. **Coordinate releases**: For large teams, use a release calendar or locking mechanism external to scafctl.
3. **Publish stable versions infrequently**: Prereleases (alpha/beta) can be published more freely; stable releases should be coordinated.
4. **Idempotent re-publish**: `scafctl publish` should detect when a version+digest already exists and skip gracefully (useful for retry-safe CI jobs).
5. **Monitor publish failures**: Set up alerts for 412 conflicts that exceed retry limits; investigate catalog write contention.

### Testing Concurrency

Recommend integration tests that:

- Spawn multiple `scafctl publish` processes targeting the same package
- Verify all versions appear in final `index.json`
- Verify no orphaned artifacts (files in version folders but not in index)
- Measure retry counts and latency under contention

## Minimal JSON Schema (summary)

See docs/schemas/catalog-index.md for a formalized schema and examples. Key fields:

- `apiVersion`: Schema version identifier.
- `kind`, `name`: Package identity.
- `versions`: Map of SemVer -> descriptor with `path`, `digest`, `size`, `signatures`, `metadata`.

## Implementation Plan (brief)

- Introduce `pkg/catalog` with a small API:
  - `Resolver` interface: `Resolve(ctx, Ref, Options) (Descriptor, error)`
  - `Publisher` interface: `Publish(ctx, Kind, Name, Version, Files, Options) error`
  - `Source` abstraction: filesystem, bucket, HTTP read-only
  - Use Masterminds/semver for constraints and prerelease handling
- Wire CLI commands under `scafctl catalog ...` and `scafctl publish ...`
- Backfill tests: file-based catalog end-to-end, semver edge cases, prerelease behavior

This design keeps the catalog human-friendly, easy to host anywhere, and efficient to resolve at scale without a central database.
# Catalog Architecture (Draft)

> **Goal:** Collapse scafctl's catalog into a single hierarchical store that supports solutions, data sources, providers, and future raw artifacts while remaining usable over local folders or remote object storage.

## Requirements Recap

- **Single logical catalog** – one bucket (remote) or one directory (local) instead of per-type buckets.
- **Pluggable backends** – local filesystem, Google Cloud Storage, S3, etc. with the same layout.
- **Multiple catalogs** – users may declare an ordered list of catalog endpoints (local + remote mirrors).
- **Versioned artifacts** – solutions, datasources, provider plugins, and generic raw artifacts should share the same publishing model.
- **Incremental metadata** – `scafctl build` produces leaf metadata; `scafctl publish` uploads artifacts and rolls up indexes above them.
- **Offline friendly** – everything is plain files; no database is required.

## Directory Layout

```
<catalog-root>/
  catalog.json              # root manifest
  solutions/
    <solution-id>/
      index.json            # version manifest
      1.0.0/
        build.json          # build metadata (created by scafctl build)
        solution.yaml       # normalized solution object
        files/...           # optional rendered files or templates
  datasources/
    <datasource-id>/...
  releases/
    <release-id>/...
  providers/
    <provider-id>/...
  artifacts/
    <artifact-id>/...
```

- `<catalog-root>` can be a filesystem directory or an object storage prefix (`gs://bucket/catalog/`).
- IDs remain Kubernetes-style (`group/name`). Nested directories mirror the ID components for readability: `solutions/example.app/frontend`.
- Version directories are immutable once published. `scafctl publish` must refuse to overwrite existing versions unless `--force` is supplied.

## Metadata Files

### `catalog.json`

Root level manifest summarizing available types.

```json
{
  "schemaVersion": "1.0",
  "generatedAt": "2025-12-20T18:45:00Z",
  "types": {
    "solutions": {
      "path": "solutions/",
      "count": 18,
      "latest": [
        { "id": "example.app/frontend", "version": "2.3.1" },
        { "id": "platform.tf/bootstrap", "version": "1.5.0" }
      ]
    },
    "datasources": { "path": "datasources/", "count": 6 },
    "providers":   { "path": "providers/", "count": 4 },
    "artifacts":   { "path": "artifacts/", "count": 2 }
  }
}
```

- Allows fast discovery without scanning the entire tree.
- `generatedAt` keeps downstream caches honest.

### `<type>/<id>/index.json`

Per-artifact manifest enumerating versions.

```json
{
  "id": "example.app/frontend",
  "type": "solution",
  "description": "Example frontend scaffold",
  "tags": ["frontend", "go"],
  "versions": [
    {
      "version": "2.3.1",
      "artifactPath": "solutions/example.app/frontend/2.3.1/",
      "meta": "solutions/example.app/frontend/2.3.1/build.json",
      "createdAt": "2025-12-18T12:03:09Z",
      "digest": "sha256:..."
    },
    {
      "version": "2.2.0",
      "artifactPath": "solutions/example.app/frontend/2.2.0/",
      "meta": "solutions/example.app/frontend/2.2.0/build.json",
      "createdAt": "2025-10-07T09:44:21Z",
      "digest": "sha256:..."
    }
  ]
}
```

- Always sorted newest first.
- `digest` allows integrity checks before downloading full artifacts.

### `<type>/<id>/<version>/build.json`

Leaf manifest created by `scafctl build`.

```json
{
  "schemaVersion": "1.0",
  "type": "solution",
  "id": "example.app/frontend",
  "version": "2.3.1",
  "createdAt": "2025-12-18T12:03:09Z",
  "builtBy": {
    "scafctl": "0.18.0",
    "gitSHA": "3acb1e8",
    "builder": "ci@company.com"
  },
  "inputs": {
    "source": "./solutions/frontend",
    "commit": "main@3acb1e8"
  },
  "primaryArtifact": "solution.yaml",
  "artifacts": [
    {
      "name": "solution.yaml",
      "path": "solution.yaml",
      "mediaType": "application/x-yaml",
      "digest": "sha256:..."
    },
    {
      "name": "templates.tar.gz",
      "path": "files/templates.tar.gz",
    - `primaryArtifact` points at the default payload for consumers. Its value can change per type (`solution.yaml`, `datasource.json`, `plugin.tar.gz`, etc.).

    ## Heterogeneous Artifacts

    - Each top-level directory under `<catalog-root>` represents an artifact type that can be listed in `catalog.json`. Adding a new type (for example `releases/`) is a matter of provisioning the directory and teaching the CLI about the schema it expects.
    - Version folders may contain any file set required by that type. For a CLI release the tree might look like:

      ```
      releases/scafctl/0.86.0/
        build.json
        datasource.json          # primary artifact (referenced by build.json.primaryArtifact)
        scafctl-banner.svg
        scafctl-icon.svg
      ```
    - Consumers rely on `build.json` metadata rather than hard-coded filenames, which lets legacy catalogs that stored `solution.json` or `datasource.json` keep their naming.
      "mediaType": "application/gzip",
      "digest": "sha256:..."
    }
  ],
  "metadata": {
    "displayName": "Example Frontend",
    "maintainers": [
      { "name": "Example Team", "email": "eng@example.com" }
    ],
    "tags": ["frontend", "go"],
    "dependsOn": ["datasource:platform/identity"]
  }
}
```

- `scafctl build` writes this file alongside the serialized solution/datasource/provider files.
- Additional files (rendered templates, provider binaries, etc.) live under the same version folder.

## Catalog Backends

The catalog layout works for any object store. scafctl resolves URIs using a single abstraction:

- `file://` or bare paths → local filesystem
- `gs://bucket/path` → Google Cloud Storage (stackdriver signers)
- `s3://bucket/path` → AWS S3 (future)

Configuration example (`~/.config/scafctl/config.yaml`):

```yaml
catalogs:
  - name: local-dev
    uri: "./.scafctl/catalog"
    writable: true
  - name: staging
    uri: "gs://scafctl-catalog-staging"
    writable: true
  - name: public
    uri: "gs://scafctl-catalog-prod"
    writable: false
```

- Catalogs are searched in order for reads. The first `writable: true` entry becomes the default publish target unless overridden with `--catalog`.
- Local catalogs can be version controlled (e.g., committing build metadata).

## CLI Workflow

### Build

```
scafctl build solution ./solutions/frontend \
  --id example.app/frontend \
  --version 2.3.1 \
  --out ./dist/catalog
```

- Creates `./dist/catalog/solutions/example.app/frontend/2.3.1/` with the normalized solution, templates, and `build.json`.
- Updates nothing outside the version folder.

### Publish

```
scafctl publish solutions example.app/frontend@2.3.1 --catalog staging
```

Steps:

1. Upload the version folder to the remote catalog (skip files already present via checksum).
2. Update `<type>/<id>/index.json` by merging the new version entry.
3. Update root `catalog.json` aggregates (counts, latest pointers).
4. Optionally invalidate CDN caches if configured.

`scafctl publish` will refuse to publish if `index.json` already contains the same version unless `--force` is provided.

### Sync

For offline users, `scafctl sync catalog public` downloads manifests (and optionally artifacts) into a local cache.

## Supporting Raw Artifacts

- `artifacts/` (and other custom types like `releases/`) are first-class directories for payloads that are not solutions.
- Each entry in `build.json.artifacts` records a `mediaType`, digest, and optional `platform` stanza (`{ "platform": { "os": "linux", "arch": "amd64" } }`).
- Solutions, providers, or datasources can declare dependencies on `artifact:` or `release:` IDs; scafctl ensures required payloads are present locally before execution.

## Migration Plan

1. Provision a new bucket `gs://scafctl-catalog-prod/`.
2. Export existing per-type buckets (`legacy-solutions_prod`, `legacy-datasources_prod`) into the new layout using a migration script:
   - Map buckets to type directories.
   - Generate `build.json` from existing metadata (CLI will provide helpers).
   - Generate missing `index.json` and `catalog.json`.
3. Update scafctl configuration to point at the unified catalog.
4. Deprecate legacy buckets once consumers have switched.

## Open Questions

- Should `scafctl publish` lock the catalog (e.g., via object-level leases) to avoid concurrent index edits? (GCS supports object preconditions.)
- Do we need delta indexes (per namespace) to keep `index.json` small for high-churn artifacts?
- How much of the metadata should be duplicated between `build.json` and `index.json` vs. referenced indirectly?

## References

- [docs/design/plugins.md](./plugins.md) — provider packaging relies on catalog distribution.
- [docs/design/providers.md](./providers.md) — provider descriptors referenced by `build.json`.
- Future RFCs: build metadata schema, publish command UX, catalog locking strategy.
