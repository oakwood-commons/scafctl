# scafctl Catalog System

## Overview

scafctl manages artifact lifecycle through a workflow analogous to container images, using OCI artifacts for storage and distribution. This design enables version control, dependency management, and consistent deployment across environments for solutions, providers, auth handlers, and other scafctl artifacts.

---

## Core Concepts

### Artifacts

The catalog system supports multiple artifact types:

- **Solutions**: YAML configuration files that define resolvers, actions, and dependencies
- **Providers**: Binary executables that provide custom providers using hashicorp/go-plugin over gRPC
- **Auth Handlers**: Binary executables that provide authentication handlers using hashicorp/go-plugin over gRPC
- **Future artifact types**: TBD as the system evolves

### Catalogs

- **Local catalog**: Functions like Docker's local image store, caching built and pulled artifacts
- **Remote catalog**: Centralized OCI registry with GUI frontend for artifact discovery and distribution

### Artifact Identification

Artifacts are distinguished using OCI media types and annotations:

**Media Types**:
- Solutions: `application/vnd.scafctl.solution.v1+yaml`
- Providers: `application/vnd.scafctl.provider.v1+binary`
- Auth Handlers: `application/vnd.scafctl.auth-handler.v1+binary`

**OCI Annotations**:
- `org.opencontainers.image.title`: Artifact name
- `org.opencontainers.image.version`: Semantic version
- `dev.scafctl.artifact.type`: Artifact type (`solution`, `provider`, `auth-handler`)
- `dev.scafctl.provider.names`: Comma-separated provider names (providers only)
- `dev.scafctl.auth-handler.names`: Comma-separated auth handler names (auth handlers only)
- `dev.scafctl.solution.requires`: Dependency specifications (solutions only)

**Repository Structure**:
```
registry.example.com/
  solutions/
    my-solution:1.2.3
    team-workflow:2.0.1
  providers/
    aws-provider:1.5.0
    custom-provider:0.8.2
  auth-handlers/
    okta-handler:1.0.0
    ldap-handler:2.1.0
```

---

## Artifact Lifecycle

### Local Development (Unpublished Artifacts)

**Solutions**: YAML file(s) in a local directory that may reference other remote solutions and plugins as dependencies.

**Plugins**: Source code that implements provider(s) using the hashicorp/go-plugin interface.

### Building Artifacts

**Command**: `scafctl build <name>` (analogous to `docker build`)

**Solution build process**:
1. Validates solution schema and structure
2. Resolves and fetches all remote dependencies (solutions and required providers)
3. Verifies dependency compatibility and detects circular dependencies
4. Caches resolved dependencies for faster subsequent builds
5. Packages the solution as an [OCI artifact](https://github.com/opencontainers/image-spec/blob/main/spec.md)
6. Stores the artifact in the local catalog with version metadata and annotations

**Provider/Auth Handler build process**:
1. Compiles the binary with hashicorp/go-plugin integration
2. Validates the binary exposes the required gRPC interface
3. Extracts metadata (provider names or auth handler names) from the binary
4. Packages as an OCI artifact with appropriate media type
5. Stores in the local catalog with capability annotations

**Key features**:
- Build caching for improved performance
- Dependency resolution and validation
- Multi-platform support for different target environments
- Platform compatibility verification

### Publishing Artifacts

**Command**: `scafctl catalog push <name>@<version> --catalog <registry>` (analogous to `docker push`)

Publishes artifacts from the local catalog to a remote catalog, making them discoverable and accessible to other users. The artifact kind is automatically inferred from the local catalog. Use `--kind` to override if needed.

Authentication to remote catalogs is handled by the OCI specification.

### Pulling Artifacts

**Command**: `scafctl catalog pull <registry>/<repo>/<kind>/<name>@<version>` (analogous to `docker pull`)

Downloads artifacts from a remote catalog to the local catalog. Pulled artifacts are cached locally and can be used as dependencies or executed directly. Providers and auth handlers are dynamically loaded during solution execution when required.

### Inspecting Artifacts

**Command**: `scafctl inspect <name>@<version>`

Displays artifact metadata, dependencies, structure, and platform requirements without downloading or building the artifact. For providers, shows available provider names. For auth handlers, shows available handler names.

### Tagging Artifacts

**Command**: `scafctl tag <source> <target>`

Creates aliases for artifact versions (e.g., `artifact@1.2.3` → `artifact:latest` or `artifact:stable`), enabling flexible versioning strategies.

### Offline Distribution

**Commands**: `scafctl save` / `scafctl load` (analogous to `docker save/load`)

Export artifacts (solutions, plugins, and their dependencies) as tar archives for air-gapped or offline environments. This enables artifact transfer without direct registry access.

**Examples**:
```bash
# Save a solution with all dependencies
scafctl save solution my-solution@1.2.3 -o solution.tar

# Save a plugin
scafctl save plugin aws-provider@1.5.0 -o aws-plugin.tar

# Load from archive
scafctl load -i solution.tar
scafctl load -i aws-provider.tar
```

---

## Versioning

All artifacts follow semantic versioning (e.g., `artifact@1.2.3`). Version tags like `latest` and `stable` can be created using the `scafctl tag` command.

---

## Design Considerations

### Authentication

Authentication to remote catalogs leverages standard OCI registry authentication mechanisms (Docker config, credential helpers, token authentication).

### Discovery

The remote catalog provides a GUI frontend for browsing, searching, and exploring available artifacts. Users can filter by artifact type, search by provider capabilities (for provider artifacts), and view dependency graphs.

### Dependency Management

**Solutions**:
- Can depend on other solutions
- Can declare required providers with version constraints
- Dependencies are resolved recursively during build
- Circular dependencies are detected and rejected

**Providers**:
- Are self-contained binaries
- Expose one or more providers via gRPC
- Can be shared across multiple solutions
- Are loaded dynamically when solutions require them

**Auth Handlers**:
- Are self-contained binaries
- Expose one or more auth handlers via gRPC
- Provide authentication capabilities (e.g., Entra, GitHub, Okta)
- Are loaded dynamically when providers require authentication

During build, scafctl recursively resolves all dependencies, validates compatibility, and detects circular references. Resolved dependencies are cached to optimize subsequent builds.

### Multi-Platform Support

All artifact types support platform-specific targeting:
- **Solutions**: Validated for deployment environment compatibility
- **Providers/Auth Handlers**: Multiple binaries for different OS/architecture combinations (linux/amd64, darwin/arm64, etc.)

Platform-specific artifacts use OCI image index (manifest list) to select the appropriate binary at runtime.

### Caching

Build caching improves performance by reusing:
- Previously resolved dependencies
- Validation results
- Downloaded provider/auth handler binaries
- Compiled templates and expressions

Cache behavior follows content-based invalidation strategies using artifact digests.

### Provider Integration

Providers are automatically discovered and loaded when solutions declare them as dependencies:

```yaml
# Solution YAML
apiVersion: scafctl.dev/v1
kind: Solution
metadata:
  name: my-solution
  version: 1.0.0
dependencies:
  providers:
    - name: aws-provider
      version: ^1.5.0
    - name: custom-provider
      version: 2.1.0
spec:
  resolvers:
    # Can now use providers from installed plugins
```

During execution, scafctl:
1. Checks if required plugins are available in local catalog
2. Downloads missing plugins from configured catalogs
3. Validates plugin versions meet dependency constraints
4. Dynamically loads plugin binaries
5. Bridges plugin providers to the execution context

---

## Local Catalog

The catalog works locally without any remote dependency.

Example local layout:

```
~/.scafctl/catalog/
  oci/
    blobs/
    index/
```

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

## Execution Flow with Catalog

When running a solution:

```bash
scafctl run solution gcp-projects@1.7.0
```

Flow:

1. Parse reference
2. Resolve version and digest
3. Fetch artifact if not cached
4. Check and load required plugins
5. Load solution YAML
6. Resolve resolvers
7. Render or execute actions

Catalog resolution is a read-only operation.

---

## Design Constraints

- Artifacts must be immutable by digest
- Version constraints are resolved client-side
- The catalog must work offline
- Artifacts must be transport-agnostic
- Execution must never mutate catalog contents

---

## Summary

The catalog is the packaging and distribution layer of scafctl. By leveraging OCI artifacts and a Docker-like workflow, it provides local-first, immutable, versioned storage for solutions and plugins. The catalog enables reproducible execution, offline workflows, dependency management, and multi-platform support while integrating cleanly with existing container tooling and registries.
