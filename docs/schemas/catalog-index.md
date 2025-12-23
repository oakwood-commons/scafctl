# Catalog Index Schema

This document defines the minimal schema for catalog JSON files used by scafctl. Catalogs are folder-based and can be hosted locally or in object storage.

## Files

- Top-level discovery (optional): `catalog.json`
- Per-package index (required for resolution): `<root>/<kind>/<name>/index.json`

## `catalog.json` (optional)

Purpose: lightweight discovery of available packages. Not required for resolution if package paths are known.

Example:

```json
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

Fields:
- `apiVersion` (string): Schema version. Current: `catalog.scafctl.dev/v1`.
- `generatedAt` (RFC3339 string): Generation timestamp.
- `packages` (array): Entries with `kind`, `name`, and `path` (relative to catalog root).

## `<kind>/<name>/index.json` (required)

Purpose: authoritative per-package version map.

Example (solution):

```json
{
  "apiVersion": "catalog.scafctl.dev/v1",
  "kind": "solution",
  "name": "my-solution",
  "versions": {
    "1.2.3": {
      "createdAt": "2025-12-01T12:34:56Z",
      "digest": "sha256:1f2d...",
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
      "digest": "sha256:9abc...",
      "size": 125678,
      "path": "v1.3.0-beta.1/package.tgz",
      "metadata": { "channel": "beta" }
    }
  }
}
```

Fields:
- `apiVersion` (string): Schema version.
- `kind` (enum string): `solution` | `datasource` | `release` | `provider`.
- `name` (string): Package name.
- `versions` (object): Map of SemVer (no leading `v`) to `VersionDescriptor`.

`VersionDescriptor` fields:
- `createdAt` (RFC3339 string): When this version was published.
- For non-release kinds:
  - `digest` (string): Content digest, e.g., `sha256:<hex>`.
  - `size` (number): Size in bytes of the primary artifact.
  - `path` (string): Path to the primary artifact relative to the package root (e.g., `v1.2.3/package.tgz`).
  - `signatures` (array, optional): Detached signatures `{ type, path }`.
- `metadata` (object, optional): Open-ended fields; suggested keys: `license`, `tags`, `channel`, `requires`.

Dependency spec (`metadata.requires`):

```json
{
  "kind": "provider",
  "name": "git",
  "constraint": ">=0.2.0 <1.0.0"
}
```

### Release versions with platform artifacts

For `kind: "release"`, each version lists one or more platform-specific artifacts. Fields differ slightly from other kinds.

Example (release):

```json
{
  "apiVersion": "catalog.scafctl.dev/v1",
  "kind": "release",
  "name": "tool-x",
  "versions": {
    "2.1.0": {
      "createdAt": "2025-12-22T10:00:00Z",
      "artifacts": [
        {
          "os": "windows",
          "arch": "amd64",
          "ext": "zip",
          "path": "v2.1.0/tool-x_2.1.0_windows_amd64.zip",
          "digest": "sha256:1111...",
          "size": 22334455
        },
        {
          "os": "linux",
          "arch": "amd64",
          "libc": "gnu",
          "ext": "tar.gz",
          "path": "v2.1.0/tool-x_2.1.0_linux_amd64_gnu.tar.gz",
          "digest": "sha256:2222...",
          "size": 20334455
        },
        {
          "os": "linux",
          "arch": "amd64",
          "libc": "musl",
          "ext": "tar.gz",
          "path": "v2.1.0/tool-x_2.1.0_linux_amd64_musl.tar.gz",
          "digest": "sha256:3333...",
          "size": 19994455
        },
        {
          "os": "darwin",
          "arch": "arm64",
          "ext": "tar.gz",
          "path": "v2.1.0/tool-x_2.1.0_darwin_arm64.tar.gz",
          "digest": "sha256:4444...",
          "size": 18334455
        }
      ],
      "checksums": "v2.1.0/checksums.txt",
      "signatures": [
        { "type": "cosign", "path": "v2.1.0/signature" }
      ],
      "metadata": {
        "releaseNotes": "..."
      }
    }
  }
}
```

Artifact fields:
- `os` (string): `linux` | `windows` | `darwin` | others if needed.
- `arch` (string): `amd64` | `arm64` | `arm` | etc.
- `variant` (string, optional): e.g., `armv7`.
- `libc` (string, optional): `gnu` or `musl` (Linux only).
- `ext` (string): `zip`, `tar.gz`, etc.
- `path` (string): Relative path to the artifact file.
- `digest` (string): Artifact content digest.
- `size` (number): Artifact size in bytes.

Version-level fields for releases:
- `artifacts` (array): List of platform-specific artifacts.
- `checksums` (string, optional): Path to checksum manifest covering all artifacts.
- `signatures` (array, optional): Signatures for the release (or per artifact if desired).

Selection rules (resolver):
- Determine target platform from flags or environment.
- Prefer exact matches `(os, arch, variant, libc)`; then `(os, arch)`; then a universal artifact if present (`os:any`, `arch:any`).

Naming convention (recommended):
- `<name>_<version>_<os>_<arch>[_<variant>][_<libc>].<ext>`
- Examples: `tool-x_2.1.0_windows_amd64.zip`, `tool-x_2.1.0_linux_amd64_musl.tar.gz`.

## SemVer and Prereleases

- Default resolution excludes prereleases unless `includePrerelease=true` or the constraint contains a prerelease.
- Use Masterminds/semver-compatible constraints (`^`, `~`, ranges, `||`).
- Examples: `^1.2.0`, `~1.3.2`, `>=1.3.0-0`.

## Validation Notes

- Ensure all artifact paths referenced by `versions[*].path` exist in the catalog.
- Recommend verifying `digest` on fetch; fail on mismatch.
- If signatures are present and verification is enabled, fail on invalid signature.

## Backwards Compatibility

- Future minor fields may be added under `metadata`. Avoid relying on undocumented keys.
- A new `apiVersion` will be introduced for breaking changes.
