# Local Catalog Implementation Plan

## Overview

This document outlines the implementation plan for the local catalog system, starting with `scafctl build` and `scafctl inspect` commands for solutions. The catalog uses OCI artifacts for storage, enabling future compatibility with remote OCI registries.

---

## Phase 1: Foundation (This Implementation)

### Scope
- Built-in local catalog at XDG data path (`paths.CatalogDir()`)
- Catalog registry that manages local + configured catalogs
- `scafctl build solution` command (local file → local catalog)
- `scafctl catalog list` command (list artifacts)
- `scafctl catalog inspect` command (view artifact metadata)
- `scafctl catalog delete` command (remove artifacts)
- `scafctl catalog prune` command (garbage collection)
- Solution-only support (plugins deferred to Phase 2)

### Out of Scope (Future Phases)
- Remote catalog support (`push`/`pull`)
- Plugin artifacts
- Dependency resolution (solutions depending on other solutions/plugins)
- Version constraint resolution
- `tag`, `save`, `load` commands
- Integration with `run` command for bare name resolution

---

## Catalog Architecture

### Built-in Local Catalog

The local catalog is always available and is the first in resolution order:

| Property | Value |
|----------|-------|
| Name | `local` |
| Type | OCI layout on disk |
| Path | `paths.CatalogDir()` (XDG-compliant) |
| Purpose | Store built artifacts, cache pulled artifacts |

### Catalog Resolution Order

When resolving an artifact (e.g., `scafctl run solution foo@1.2.3`):

1. **Local catalog** (built-in) - checked first
2. **Configured catalogs** - checked in config order
3. **Auto-pull** - if found in remote, pull to local (future)

### Command Behavior

| Command | Catalog Behavior |
|---------|------------------|
| `build` | Always stores to built-in local catalog |
| `inspect` | Checks local first, then configured catalogs |
| `run` | Local first → configured catalogs → auto-pull if found |
| `pull` | Downloads from remote → stores to local |
| `push` | Uploads from local → specified remote catalog |

### Config Structure

```yaml
# Config file (XDG config path)
catalogs:
  # Built-in "local" catalog is implicit - never defined here
  
  # User-defined remote catalogs (future - Phase 2)
  - name: company-registry
    url: oci://registry.company.com/scafctl
    default: true  # Used when no --catalog specified for push/pull
    
  - name: public
    url: oci://ghcr.io/scafctl-community
```

---

## Dependencies

### New Go Modules Required

```go
// OCI content store and artifact handling
"oras.land/oras-go/v2"                    // ORAS library for OCI operations
"oras.land/oras-go/v2/content/oci"        // Local OCI layout store
"github.com/opencontainers/image-spec/specs-go/v1" // OCI types
```

### Why ORAS?
- Standard library for OCI artifact manipulation
- Used by Helm, Notation, and many CNCF projects
- Supports both local stores and remote registries
- Handles content-addressable storage automatically
- Active maintenance and community

---

## Package Structure

```
pkg/
  catalog/
    catalog.go          # Catalog interface and registry
    local.go            # Local OCI store implementation (built-in)
    remote.go           # Remote OCI registry implementation (future)
    artifact.go         # Artifact types (solution, plugin)
    reference.go        # Name@version parsing
    media_types.go      # Media type constants
    annotations.go      # OCI annotation helpers
    errors.go           # Catalog-specific errors
    
  cmd/scafctl/
    build/
      build.go          # scafctl build command group
      solution.go       # scafctl build solution subcommand
    catalog/
      catalog.go        # scafctl catalog command group
      list.go           # scafctl catalog list subcommand
      inspect.go        # scafctl catalog inspect subcommand
      delete.go         # scafctl catalog delete subcommand
      prune.go          # scafctl catalog prune subcommand
```

---

## Detailed Design

### 1. Catalog Interface (`pkg/catalog/catalog.go`)

```go
// ArtifactKind represents the type of artifact
type ArtifactKind string

const (
    ArtifactKindSolution ArtifactKind = "solution"
    ArtifactKindPlugin   ArtifactKind = "plugin"
)

// Reference uniquely identifies an artifact
type Reference struct {
    Kind    ArtifactKind
    Name    string           // e.g., "my-solution"
    Version *semver.Version  // e.g., 1.2.3
    Digest  string           // sha256:... (optional, for pinning)
}

// ArtifactInfo contains metadata about a stored artifact
type ArtifactInfo struct {
    Reference   Reference
    Digest      string            // Content digest
    CreatedAt   time.Time
    Size        int64
    Annotations map[string]string // OCI annotations
    Catalog     string            // Which catalog this came from (e.g., "local")
}

// Catalog defines the interface for a single catalog (local or remote)
type Catalog interface {
    // Name returns the catalog identifier (e.g., "local", "company-registry")
    Name() string
    
    // Store saves an artifact to the catalog
    Store(ctx context.Context, ref Reference, content []byte, annotations map[string]string) (ArtifactInfo, error)
    
    // Fetch retrieves an artifact from the catalog
    Fetch(ctx context.Context, ref Reference) ([]byte, ArtifactInfo, error)
    
    // Resolve finds the best matching version for a reference
    Resolve(ctx context.Context, ref Reference) (ArtifactInfo, error)
    
    // List returns all artifacts matching criteria
    List(ctx context.Context, kind ArtifactKind, name string) ([]ArtifactInfo, error)
    
    // Exists checks if an artifact exists
    Exists(ctx context.Context, ref Reference) (bool, error)
    
    // Delete removes an artifact
    Delete(ctx context.Context, ref Reference) error
}

// Registry manages multiple catalogs with resolution order
type Registry struct {
    local    Catalog   // Built-in local catalog (always first)
    catalogs []Catalog // Configured catalogs in order
    logger   logr.Logger
}

// NewRegistry creates a registry with the built-in local catalog
func NewRegistry(logger logr.Logger) (*Registry, error)

// Local returns the built-in local catalog
func (r *Registry) Local() Catalog

// Resolve finds an artifact across all catalogs (local first)
func (r *Registry) Resolve(ctx context.Context, ref Reference) ([]byte, ArtifactInfo, error)

// List returns artifacts from all catalogs
func (r *Registry) List(ctx context.Context, kind ArtifactKind, name string) ([]ArtifactInfo, error)
```

### 2. Local Catalog Implementation (`pkg/catalog/local.go`)

```go
const LocalCatalogName = "local"

// LocalCatalog implements Catalog using a local OCI layout store
type LocalCatalog struct {
    store    *oci.Store        // ORAS OCI store
    basePath string            // From paths.CatalogDir()
    logger   logr.Logger
}

// NewLocalCatalog creates a catalog at the XDG data path
// Uses paths.CatalogDir() which returns:
//   - Linux: ~/.local/share/scafctl/catalog/
//   - macOS: ~/Library/Application Support/scafctl/catalog/
//   - Windows: %LOCALAPPDATA%\scafctl\catalog\
func NewLocalCatalog(logger logr.Logger) (*LocalCatalog, error)

// NewLocalCatalogAt creates a catalog at a custom path (for testing)
func NewLocalCatalogAt(basePath string, logger logr.Logger) (*LocalCatalog, error)

// Name returns "local"
func (c *LocalCatalog) Name() string { return LocalCatalogName }
```

**OCI Layout Structure:**
```
$XDG_DATA_HOME/scafctl/catalog/   # e.g., ~/.local/share/scafctl/catalog/
  oci-layout              # {"imageLayoutVersion": "1.0.0"}
  index.json              # OCI image index (manifest list)
  blobs/
    sha256/
      abc123...           # Manifests and content blobs
```

### 3. Media Types (`pkg/catalog/media_types.go`)

```go
const (
    // Solution artifact media types
    MediaTypeSolutionManifest = "application/vnd.scafctl.solution.manifest.v1+json"
    MediaTypeSolutionContent  = "application/vnd.scafctl.solution.v1+yaml"
    MediaTypeSolutionConfig   = "application/vnd.scafctl.solution.config.v1+json"
    
    // Plugin artifact media types (for future use)
    MediaTypePluginManifest = "application/vnd.scafctl.plugin.manifest.v1+json"
    MediaTypePluginBinary   = "application/vnd.scafctl.plugin.v1+binary"
    MediaTypePluginConfig   = "application/vnd.scafctl.plugin.config.v1+json"
)
```

### 4. OCI Annotations (`pkg/catalog/annotations.go`)

```go
const (
    // Standard OCI annotations
    AnnotationTitle       = "org.opencontainers.image.title"
    AnnotationVersion     = "org.opencontainers.image.version"
    AnnotationDescription = "org.opencontainers.image.description"
    AnnotationCreated     = "org.opencontainers.image.created"
    AnnotationAuthors     = "org.opencontainers.image.authors"
    AnnotationSource      = "org.opencontainers.image.source"
    
    // scafctl-specific annotations
    AnnotationArtifactType = "dev.scafctl.artifact.type"       // "solution" or "plugin"
    AnnotationCategory     = "dev.scafctl.solution.category"
    AnnotationTags         = "dev.scafctl.solution.tags"       // comma-separated
    AnnotationMaintainers  = "dev.scafctl.solution.maintainers" // JSON array
    AnnotationRequires     = "dev.scafctl.solution.requires"   // dependency specs (future)
    
    // Plugin-specific (future)
    AnnotationProviders    = "dev.scafctl.plugin.providers"    // comma-separated provider names
    AnnotationPlatform     = "dev.scafctl.plugin.platform"     // e.g., "linux/amd64"
)
```

### 5. Reference Parsing (`pkg/catalog/reference.go`)

```go
// ParseReference parses "name@version" or "name" format
func ParseReference(kind ArtifactKind, input string) (Reference, error)

// Examples:
// "my-solution@1.2.3" → Reference{Kind: solution, Name: "my-solution", Version: 1.2.3}
// "my-solution"       → Reference{Kind: solution, Name: "my-solution", Version: nil}

// String returns the canonical reference string
func (r Reference) String() string
```

---

## CLI Commands

### `scafctl build solution`

**Purpose:** Package a local solution file into the local catalog.

**Usage:**
```bash
scafctl build solution [flags]

# Build from current directory (finds solution.yaml automatically)
scafctl build solution

# Build from specific file
scafctl build solution -f ./my-solution.yaml

# Build with explicit version override
scafctl build solution -f ./solution.yaml --version 2.0.0
```

**Flags:**
| Flag | Short | Description |
|------|-------|-------------|
| `--file` | `-f` | Path to solution file (default: auto-discover) |
| `--version` | | Override version from metadata |
| `--output` | `-o` | Output format: table, json, yaml (default: table) |
| `--quiet` | `-q` | Only output artifact digest |

**Build Process:**
1. Load and parse solution file
2. Validate solution schema (reuse existing validation)
3. Extract metadata (name, version, description, etc.)
4. Serialize solution to canonical YAML
5. Create OCI manifest with layers:
   - Config blob: solution metadata as JSON
   - Content blob: solution YAML
6. Add OCI annotations from metadata
7. Store in local catalog
8. Output artifact info (name, version, digest, size)

**Output (table format):**
```
Built solution successfully!

Name:     my-solution
Version:  1.2.3
Digest:   sha256:abc123...
Size:     2.4 KB
Stored:   ~/.local/share/scafctl/catalog  (or platform-specific XDG path)
```

### `scafctl catalog list`

**Purpose:** List solutions stored in the local catalog.

**Usage:**
```bash
scafctl catalog list [flags]

# List all solutions
scafctl catalog list

# Output as JSON
scafctl catalog list -o json

# Only show names (quiet mode)
scafctl catalog list -q
```

**Flags:**
| Flag | Short | Description |
|------|-------|-------------|
| `--output` | `-o` | Output format: table, json, yaml, quiet (default: table) |
| `--quiet` | `-q` | Only output solution names |

**Output (table format):**
```
SOLUTION                    VERSION   CREATED                SIZE
dependencies-example        1.0.0     2026-02-05T14:30:00Z   1.2 KB
my-app                      2.1.0     2026-02-06T09:15:00Z   3.4 KB
my-app                      2.0.0     2026-02-05T16:00:00Z   3.2 KB
```

---

### `scafctl catalog inspect`

**Purpose:** Display metadata and structure of a cataloged solution.

**Usage:**
```bash
scafctl catalog inspect <name[@version]> [flags]

# Inspect latest version
scafctl catalog inspect my-solution

# Inspect specific version  
scafctl catalog inspect my-solution@1.2.3

# Inspect by digest
scafctl catalog inspect my-solution@sha256:abc123...
```

**Flags:**
| Flag | Short | Description |
|------|-------|-------------|
| `--output` | `-o` | Output format: table, json, yaml (default: table) |
| `--show-content` | | Include full solution content |

**Output (table format):**
```
Solution: my-solution@1.2.3

METADATA
  Name:         my-solution
  Version:      1.2.3
  Display Name: My Example Solution
  Description:  Does something useful
  Category:     infrastructure
  Tags:         gcp, terraform, networking

ARTIFACT
  Digest:       sha256:abc123def456...
  Created:      2026-02-06T10:30:00Z
  Size:         2.4 KB
  Media Type:   application/vnd.scafctl.solution.v1+yaml

STRUCTURE
  Resolvers:    5 (config, env, http-data, transformed, validated)
  Actions:      3 (deploy, verify, notify)
  Finally:      1 (cleanup)

MAINTAINERS
  • Jane Doe <jane@example.com>
```

---

### `scafctl catalog delete`

**Purpose:** Remove a solution from the local catalog.

**Usage:**
```bash
scafctl catalog delete <name[@version]> [flags]

# Delete a specific version
scafctl catalog delete my-solution@1.2.3

# Delete latest version
scafctl catalog delete my-solution

# Skip confirmation prompt
scafctl catalog delete my-solution@1.2.3 --force
```

**Flags:**
| Flag | Short | Description |
|------|-------|-------------|
| `--force` | `-f` | Skip confirmation prompt |
| `--output` | `-o` | Output format: table, json, yaml (default: table) |

**Output (table format):**
```
Deleted solution successfully!

Name:     my-solution
Version:  1.2.3
Digest:   sha256:abc123...
```

**Note:** Delete untags the artifact but does not immediately remove blobs from storage. Use `catalog prune` to reclaim disk space.

---

### `scafctl catalog prune`

**Purpose:** Remove orphaned manifests and blobs from the local catalog to reclaim disk space.

**Usage:**
```bash
scafctl catalog prune [flags]

# Prune orphaned data
scafctl catalog prune

# Output as JSON
scafctl catalog prune -o json
```

**Flags:**
| Flag | Short | Description |
|------|-------|-------------|
| `--output` | `-o` | Output format: table, json, yaml (default: table) |

**Output (table format):**
```
Pruned catalog successfully!

Manifests Removed:   3
Blobs Removed:       7
Space Reclaimed:     7.5 KB
```

**How it works:**
1. Scans all tagged manifests in the catalog
2. Collects all blob digests referenced by tagged manifests
3. Removes manifests without `org.opencontainers.image.ref.name` annotation (orphaned)
4. Removes blobs not referenced by any remaining manifest

---

## Open Questions & Decisions Needed

### 1. **Version Resolution Without Remote Catalog** ✅ DECIDED

**Decision:** Highest semver version in local catalog.

---

### 2. **Duplicate Version Handling** ✅ DECIDED

**Decision:** Error by default, `--force` flag to overwrite.

---

### 3. **Integration with `run solution`** ✅ DECIDED

**Decision:** Catalog-first for bare names (no path separators), paths are files.

---

### 4. **Catalog Path Configuration** ✅ DECIDED

**Decision:** XDG path via `paths.CatalogDir()`, override via `SCAFCTL_CATALOG_PATH` env var.

---

### 5. **List Command** ✅ DECIDED

**Decision:** Option B - Add separate `scafctl catalog` command group with `list`, `inspect`, `delete`, and `prune` subcommands.

This provides a dedicated namespace for catalog operations and follows the pattern of other CLIs like `docker image` and `helm repo`.

---

### 6. **Content Storage Format** ✅ DECIDED

**Decision:** Store original YAML verbatim (preserves user formatting).

---

## Implementation Order

### Step 1: Core Catalog Package ✅ DONE
1. Create `pkg/catalog/` package structure
2. Implement `Reference` parsing
3. Implement `LocalCatalog` with ORAS
4. Add unit tests for store/fetch/list/delete/prune operations

### Step 2: Build Command ✅ DONE
1. Create `pkg/cmd/scafctl/build/` command structure
2. Implement `build solution` subcommand
3. Wire up solution loading, validation, and catalog storage
4. Name extraction from solution `metadata.name` (with filename fallback)
5. Add integration tests

### Step 3: Catalog Commands ✅ DONE
1. Create `pkg/cmd/scafctl/catalog/` command structure
2. Implement `catalog list` subcommand with kvx output
3. Implement `catalog inspect` subcommand
4. Implement `catalog delete` subcommand
5. Implement `catalog prune` subcommand (garbage collection)
6. Add output formatting (table, json, yaml)
7. Add integration tests

### Step 4: Integration with Run Command ✅ DONE
1. Update solution getter to check catalog for bare names
2. Implement version resolution (highest semver)
3. Add integration tests for catalog-first resolution

### Step 5: Documentation & Examples ✅ COMPLETE
1. ✅ Update user documentation (getting-started.md updated with catalog commands)
2. ✅ Add catalog tutorial (docs/tutorials/catalog-tutorial.md)
3. ✅ Update tutorials index (_index.md)

---

## Testing Strategy

### Unit Tests
- Reference parsing edge cases
- Catalog store/fetch/list operations (mock OCI store)
- Annotation extraction and mapping
- Version comparison and resolution

### Integration Tests (in `tests/integration/cli_test.go`)
- `scafctl build solution` from example files
- `scafctl inspect solution` on built artifacts
- `scafctl run solution` from catalog
- Error cases (missing artifact, invalid version, duplicate build)

### Example Workflow Test
```bash
# Build a solution
scafctl build solution -f examples/solutions/basic.yaml

# Inspect it
scafctl inspect solution basic

# Run from catalog
scafctl run solution basic

# List catalog contents
scafctl get solution  # shows catalog + local files
```

---

## Estimated Effort

| Component | Effort | Notes |
|-----------|--------|-------|
| Core catalog package | 3-4 hours | ORAS integration, types |
| Build command | 2-3 hours | CLI + solution packaging |
| Inspect command | 1-2 hours | CLI + formatting |
| Run integration | 2-3 hours | Getter updates, resolution |
| Tests | 2-3 hours | Unit + integration |
| Documentation | 1 hour | |
| **Total** | **~12-16 hours** | |

---

## Success Criteria

1. ✅ Can build a solution from file to local catalog
2. ✅ Can list cataloged solutions (`scafctl catalog list`)
3. ✅ Can inspect a cataloged solution (`scafctl catalog inspect`)
4. ✅ Can delete a cataloged solution (`scafctl catalog delete`)
5. ✅ Can prune orphaned blobs (`scafctl catalog prune`)
6. ✅ Version resolution works (highest semver for inspect)
7. ✅ Duplicate version detection works (`--force` to overwrite)
8. ✅ All existing tests still pass
9. ✅ Integration tests cover happy path and error cases
10. ✅ Can run a solution by name from catalog

---

## Future Phases (Out of Scope)

### Phase 2: Remote Catalog
- `scafctl push solution` to OCI registry
- `scafctl pull solution` from OCI registry
- OCI authentication (docker config, credential helpers)
- `--catalog <url>` flag

### Phase 3: Advanced Features
- `scafctl tag solution source target`
- `scafctl save/load` for offline distribution
- Cache TTL and automatic cleanup policies
- Multi-catalog resolution for `run` command

### Phase 4: Plugins
- `scafctl build plugin`
- Multi-platform plugin binaries
- Plugin discovery and loading from catalog
- Solution → plugin dependency resolution
