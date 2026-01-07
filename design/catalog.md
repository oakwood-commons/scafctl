# Catalog

## Purpose

The catalog is the distribution, versioning, and discovery mechanism for scafctl artifacts. It provides a local-first way to store, retrieve, and version solutions, providers, and related metadata without requiring a database or centralized service.

The catalog answers:

- What artifacts exist?
- What versions are available?
- How do I retrieve an immutable artifact?
- How do I publish a new version?

The catalog does not execute logic, resolve data, or orchestrate workflows.

---

## Core Principles

- Local-first
- Offline-capable
- Immutable artifacts
- Content-addressed
- Versioned with semantic versioning
- No database requirement
- Client-side resolution
- Compatible with external tooling

---

## Artifact Types

The catalog stores artifacts, not raw files.

### Supported Artifact Types

- Solutions
- Provider plugins
- Metadata bundles
- Releases (tags over immutable artifacts)

Each artifact type has a well-defined structure and identity.

---

## OCI as the Storage Format

The catalog is implemented using OCI artifacts, not container images.

Using OCI provides:

- Content-addressable storage
- Immutable digests
- Tag-based versioning
- Existing registry support
- Existing tooling
- Local and remote transport
- No runtime services

Artifacts are simple compressed payloads stored and transported using the OCI specification.

---

## Artifact Identity

Artifacts are referenced using a canonical identifier.

### Reference Format

type:name@version # or constraint

Examples:

solution:gcp-projects@1.7.0
solution:gcp-projects@^1.7
provider:api@2.3.1

At execution time, all references are resolved to an immutable digest.

---

## Solution Artifacts

Solution artifacts contain:

- One or more solution YAML files
- Required metadata
- Optional documentation

Example logical reference:

solution:gcp-projects@1.7.0

Example OCI layout:

oci://registry/solutions/gcp-projects:1.7.0

Payload:

- tar.gz archive
- solution.yaml
- metadata.yaml or metadata.json

---

## Provider Artifacts

Provider artifacts deliver plugin binaries and schemas.

They may include:

- Plugin binaries (platform-specific)
- Provider schemas
- Capability metadata

Example reference:

provider:api@2.3.1

Providers are discovered from the catalog and loaded via the plugin system.

---

## Releases and Tags

Releases are modeled as OCI tags.

Properties:

- Tags point to immutable digests
- Multiple tags may reference the same digest
- Tags may be mutable
- Digests are immutable

Examples:

- 1.7.0
- 1.7
- latest

Execution always resolves to a digest.

---

## Local Catalog

The catalog works locally without any remote dependency.

Example local layout:

~/.scafctl/catalog/
  oci/
    blobs/
    index/

This directory is an OCI content store.

Behavior:

- Artifacts are cached locally
- Previously fetched artifacts can be reused offline
- Local-only catalogs are supported

---

## Remote Catalogs

Remote catalogs are standard OCI registries.

Supported backends:

- OCI registries
- Local filesystem stores
- Private registries
- Air-gapped mirrors

scafctl does not require a specific registry implementation.

---

## Version Resolution

Version resolution is performed client-side.

When resolving a reference:

solution:gcp-projects@^1.7

scafctl:

1. Lists available versions
2. Applies semantic version constraints
3. Selects the highest compatible version
4. Resolves to a digest
5. Fetches the artifact if needed

No server-side logic is required.

---

## Querying the Catalog

Catalog queries are intentionally limited and deterministic.

Supported queries:

- List artifact names
- List versions for an artifact
- Inspect artifact metadata
- Resolve references

Example commands:

scafctl catalog list solutions
scafctl catalog versions solution:gcp-projects
scafctl catalog inspect solution:gcp-projects@1.7.0

All queries operate on local metadata and cached manifests.

---

## Publishing Artifacts

Publishing is explicit and simple.

Example:

scafctl catalog publish solution.yaml \
  --name gcp-projects \
  --version 1.7.0

Behavior:

- Validate artifact
- Package files into OCI format
- Push to configured catalog
- Tag with version
- Update local cache

Publishing does not require a database or service.

---

## Execution Flow with Catalog

When running a solution:

scafctl run solution:gcp-projects@1.7.0

Flow:

1. Parse reference
2. Resolve version and digest
3. Fetch artifact if not cached
4. Load solution YAML
5. Resolve resolvers
6. Render or execute actions

Catalog resolution is a read-only operation.

---

## Render Mode and the Catalog

In render mode:

scafctl render solution:gcp-projects@1.7.0

Behavior:

- Artifact is resolved and fetched
- Solution is rendered
- No providers are executed
- Output is a declarative action graph

The catalog remains unchanged.

---

## Design Constraints

- The catalog must not require a database
- Artifacts must be immutable by digest
- Version constraints are resolved client-side
- The catalog must work offline
- Artifacts must be transport-agnostic
- Execution must never mutate catalog contents

---

## Summary

The catalog is the packaging and distribution layer of scafctl. By leveraging OCI artifacts, it provides local-first, immutable, versioned storage for solutions and providers without requiring a database or centralized service. The catalog enables reproducible execution, offline workflows, and simple publishing while integrating cleanly with existing container tooling and registries.
