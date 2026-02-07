# Remote Registry Implementation Plan

## Status: ✅ Implemented (Phase 1-5 Complete)

## Overview

This document outlines the implementation plan for remote OCI registry support, enabling users to push artifacts to and pull artifacts from remote registries. This builds on the existing local catalog infrastructure and OCI foundation using oras-go.

**Implementation Status:**
- ✅ Phase 1: Remote Catalog Infrastructure - Complete
- ✅ Phase 2: Push Command - Complete
- ✅ Phase 3: Pull Command - Complete
- ✅ Phase 4: Auth & Config - Complete (docker config support)
- ✅ Phase 5: URL Parsing - Complete
- 🔄 Phase 6: Integration Tests - Basic tests added
- ⏳ Phase 7: Documentation - Pending

---

## Phase 1: Remote Catalog Infrastructure ✅

### Goals
- Create `RemoteCatalog` type implementing the `Catalog` interface
- Support standard OCI registries (Docker Hub, GHCR, ACR, ECR, Harbor, etc.)
- Use existing docker config for authentication

### Tasks

#### 1.1 Create `pkg/catalog/remote.go` ✅

```go
// RemoteCatalog implements Catalog interface for OCI registries.
type RemoteCatalog struct {
    name       string
    repository remote.Repository  // oras-go remote repository
    logger     logr.Logger
}

// NewRemoteCatalog creates a remote catalog client.
func NewRemoteCatalog(name, registryURL string, logger logr.Logger) (*RemoteCatalog, error)
```

**Key methods:**
- `Store()` - Push artifact to remote registry
- `Fetch()` - Pull artifact from remote registry
- `Resolve()` - Find latest version matching reference
- `List()` - List all artifacts (if registry supports listing)
- `Exists()` - Check if artifact exists
- `Delete()` - Delete artifact (if registry supports delete)

#### 1.2 Create `pkg/catalog/auth.go`

```go
// CredentialStore provides OCI registry credentials.
type CredentialStore struct {
    dockerConfig string  // Path to docker config file
    logger       logr.Logger
}

// NewCredentialStore creates a credential store from docker config.
func NewCredentialStore(logger logr.Logger) (*CredentialStore, error)

// Credential returns auth config for a registry host.
func (c *CredentialStore) Credential(ctx context.Context, host string) (auth.Credential, error)
```

**Authentication sources (in order):**
1. `~/.docker/config.json` (standard docker config)
2. Docker credential helpers (docker-credential-osxkeychain, etc.)
3. Environment variables: `SCAFCTL_REGISTRY_USER`, `SCAFCTL_REGISTRY_PASSWORD`
4. Anonymous access (public registries)

#### 1.3 Update `pkg/catalog/registry.go`

- Add `AddRemoteCatalog(name, url string)` method
- Support loading remote catalogs from config
- Maintain local-first resolution order

---

## Phase 2: Push Command

### Command: `scafctl push <name[@version]>`

Push an artifact from local catalog to a remote registry.

### CLI Design

```bash
# Push to default remote catalog
scafctl push my-solution@1.0.0

# Push to specific catalog
scafctl push my-solution@1.0.0 --catalog ghcr.io/myorg/scafctl

# Push with custom name (copy/rename)
scafctl push my-solution@1.0.0 --as production-solution

# Force overwrite existing
scafctl push my-solution@1.0.0 --force
```

### Implementation

#### 2.1 Create `pkg/cmd/scafctl/catalog/push.go`

```go
type PushOptions struct {
    Reference    string   // Artifact reference (name@version)
    CatalogURL   string   // Target catalog URL (--catalog)
    TargetName   string   // Optional target name (--as)
    Force        bool     // Overwrite existing (--force)
    CliParams    *settings.Run
    IOStreams    *terminal.IOStreams
}

func CommandPush(cliParams *settings.Run, ioStreams *terminal.IOStreams, _ string) *cobra.Command
func runPush(ctx context.Context, opts *PushOptions) error
```

#### 2.2 Add push capability to `LocalCatalog`

```go
// Push uploads an artifact from local catalog to a remote catalog.
func (l *LocalCatalog) Push(ctx context.Context, ref Reference, target *RemoteCatalog, opts PushOptions) error
```

Uses oras-go `Copy()` to transfer manifests and blobs.

### Tasks
- [ ] Create `pkg/cmd/scafctl/catalog/push.go`
- [ ] Implement `PushOptions` struct with flags
- [ ] Implement `runPush()` function
- [ ] Add `Push()` method to `LocalCatalog`
- [ ] Wire up to catalog command group
- [ ] Add unit tests for push command
- [ ] Add integration test (use testcontainers for registry)

---

## Phase 3: Pull Command

### Command: `scafctl pull <name[@version]>`

Pull an artifact from a remote registry to local catalog.

### CLI Design

```bash
# Pull from default remote catalog
scafctl pull my-solution@1.0.0

# Pull latest version
scafctl pull my-solution

# Pull from specific catalog
scafctl pull ghcr.io/myorg/scafctl/my-solution@1.0.0

# Pull with different local name
scafctl pull my-solution@1.0.0 --as local-solution
```

### Implementation

#### 3.1 Create `pkg/cmd/scafctl/catalog/pull.go`

```go
type PullOptions struct {
    Reference    string   // Artifact reference (name@version or full URL)
    LocalName    string   // Optional local name (--as)
    CliParams    *settings.Run
    IOStreams    *terminal.IOStreams
}

func CommandPull(cliParams *settings.Run, ioStreams *terminal.IOStreams, _ string) *cobra.Command
func runPull(ctx context.Context, opts *PullOptions) error
```

#### 3.2 Add pull capability to `LocalCatalog`

```go
// Pull downloads an artifact from a remote catalog to local catalog.
func (l *LocalCatalog) Pull(ctx context.Context, source *RemoteCatalog, ref Reference, opts PullOptions) (ArtifactInfo, error)
```

### Tasks
- [ ] Create `pkg/cmd/scafctl/catalog/pull.go`
- [ ] Implement `PullOptions` struct with flags
- [ ] Implement `runPull()` function
- [ ] Add `Pull()` method to `LocalCatalog`
- [ ] Wire up to catalog command group
- [ ] Add unit tests for pull command
- [ ] Add integration test

---

## Phase 4: Authentication & Configuration

### 4.1 Docker Config Support

Implement credential resolution from `~/.docker/config.json`:

```go
// In pkg/catalog/auth.go
func (c *CredentialStore) loadDockerConfig() error {
    // Standard locations:
    // 1. $DOCKER_CONFIG/config.json
    // 2. ~/.docker/config.json
}

func (c *CredentialStore) resolveCredHelper(helper string, host string) (auth.Credential, error) {
    // Execute docker-credential-{helper} to get credentials
}
```

### 4.2 Add `--catalog` Flag

Add global flag support for catalog override:

```go
// In pkg/cmd/scafctl/root.go
cmd.PersistentFlags().StringVar(&opts.CatalogURL, "catalog", "", "Remote catalog URL override")
```

### 4.3 Config Integration

Catalog configuration in `~/.scafctl/config.yaml`:

```yaml
catalogs:
  - name: company
    url: oci://registry.company.com/scafctl
    default: true  # Used when no --catalog specified
    
  - name: public
    url: oci://ghcr.io/scafctl-community
```

### Tasks
- [ ] Implement `CredentialStore` in `pkg/catalog/auth.go`
- [ ] Add docker credential helper support
- [ ] Add `--catalog` global flag to root command
- [ ] Add catalog URL parsing and validation
- [ ] Add environment variable support (`SCAFCTL_DEFAULT_CATALOG`)
- [ ] Update config schema for remote catalogs
- [ ] Add unit tests for auth

---

## Phase 5: Registry URL Parsing

### URL Formats

Support multiple URL formats for registries:

| Format | Example | Description |
|--------|---------|-------------|
| Full URL | `oci://ghcr.io/org/repo/name@1.0.0` | Complete artifact reference |
| Registry + name | `ghcr.io/org/repo/name@1.0.0` | Inferred OCI scheme |
| Name only | `name@1.0.0` | Use default catalog |
| Docker Hub | `myorg/name:1.0.0` | Docker Hub shorthand |

### Implementation

```go
// In pkg/catalog/reference.go
func ParseRemoteReference(ref string) (registry, repository, tag string, err error)
```

### Tasks
- [ ] Implement `ParseRemoteReference()` function
- [ ] Add URL normalization (add oci:// prefix, etc.)
- [ ] Support Docker Hub shorthand (`user/repo` → `docker.io/user/repo`)
- [ ] Add unit tests for URL parsing

---

## Phase 6: Integration Tests

### Test Registry Setup

Use testcontainers to spin up a local registry for integration tests:

```go
// In tests/integration/registry_test.go
func setupRegistry(t *testing.T) (registryURL string, cleanup func())
```

### Test Cases

1. **Push/Pull round-trip**
   - Build solution → push to registry → pull from registry → verify content

2. **Authentication**
   - Push to private registry with credentials
   - Pull from private registry with credentials
   - Fail gracefully on auth errors

3. **Version resolution**
   - Pull latest when no version specified
   - Pull specific version

4. **Error handling**
   - Registry unavailable
   - Artifact not found
   - Permission denied

### Tasks
- [ ] Create `tests/integration/registry_test.go`
- [ ] Implement registry container setup
- [ ] Add push/pull round-trip tests
- [ ] Add authentication tests
- [ ] Add error handling tests

---

## Phase 7: Documentation & Examples

### Documentation Updates

1. **Tutorial: `docs/tutorials/catalog-tutorial.md`**
   - Add remote registry section
   - Push/pull workflows
   - Authentication setup

2. **Design doc: `docs/design/catalog.md`**
   - Update with implemented features

3. **Examples**
   - Add `examples/config/remote-registry.yaml`
   - Add CI/CD workflow examples

### Tasks
- [ ] Update `docs/tutorials/catalog-tutorial.md`
- [ ] Update `docs/design/catalog.md`
- [ ] Create example config files
- [ ] Update README with registry examples

---

## Dependencies

### Go Modules (already available)

```go
"oras.land/oras-go/v2"                      // OCI operations
"oras.land/oras-go/v2/registry/remote"      // Remote registry client
"oras.land/oras-go/v2/registry/remote/auth" // Authentication
```

### Key ORAS APIs

```go
// Remote repository
repo, _ := remote.NewRepository(registryURL)
repo.Client = &auth.Client{
    Credential: credentialFunc,
}

// Copy from local to remote (push)
oras.Copy(ctx, localStore, localRef, remoteRepo, remoteRef, opts)

// Copy from remote to local (pull)
oras.Copy(ctx, remoteRepo, remoteRef, localStore, localRef, opts)
```

---

## Implementation Order

1. **Phase 1**: Remote catalog infrastructure (`remote.go`, `auth.go`)
2. **Phase 5**: URL parsing (needed for Phase 2/3)
3. **Phase 2**: Push command
4. **Phase 3**: Pull command
5. **Phase 4**: Full auth & config integration
6. **Phase 6**: Integration tests
7. **Phase 7**: Documentation

Estimated effort: **3-5 days**

---

## Success Criteria

- [ ] `scafctl push` uploads artifacts to OCI registries
- [ ] `scafctl pull` downloads artifacts from OCI registries
- [ ] Authentication works with docker config
- [ ] `--catalog` flag overrides default catalog
- [ ] Works with common registries (GHCR, Docker Hub, Harbor, ACR, ECR)
- [ ] All new code has unit test coverage
- [ ] Integration tests pass with testcontainers registry
- [ ] Documentation updated

---

## Future Enhancements (Out of Scope)

- Dependency resolution during pull (pull transitive dependencies)
- Auto-pull during `run solution` when artifact not found locally
- Registry mirroring/proxy support
- Signature verification (Sigstore/Cosign)
- SBOM attachment
