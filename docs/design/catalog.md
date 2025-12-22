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
