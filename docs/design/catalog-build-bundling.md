---
title: "Catalog Build Bundling"
weight: 15
---

# Solution File Bundling

**Date:** February 9, 2026

---

## Problem Statement

When a solution is built (`scafctl build solution`) and pushed to a catalog, only the solution YAML file is stored as a single OCI layer. Three categories of dependencies are lost:

1. **Local file references** — template files read by the `file` provider, sub-solutions used by the `solution` provider, or other local resources.
2. **Multi-file solution parts** — solutions split across multiple YAML files (e.g., `resolvers.yaml`, `workflow.yaml`) that compose the complete solution.
3. **Remote catalog dependencies** — sub-solutions referenced by catalog name (e.g., `deploy-to-k8s@2.0.0`) that must be fetched from a registry at runtime.

This means solutions are not portable across machines, teams, or environments.

### Examples of Broken Portability

**Template files via the `file` provider:**
```yaml
resolvers:
  mainTfTemplate:
    resolve:
      with:
        - provider: file
          inputs:
            operation: read
            path: templates/main.tf.tmpl   # ← Not included in build
```

**Sub-solutions via the `solution` provider:**
```yaml
resolvers:
  child-data:
    resolve:
      with:
        - provider: solution
          inputs:
            source: "./child.yaml"         # ← Not included in build
```

**Remote catalog references:**
```yaml
resolvers:
  infra:
    resolve:
      with:
        - provider: solution
          inputs:
            source: "deploy-to-k8s@2.0.0"  # ← Requires network access at runtime
```

**Dynamic paths computed via CEL or Go templates:**
```yaml
inputs:
  path:
    expr: "'templates/' + _.environment + '/main.tf.tmpl'"   # ← Cannot be statically analyzed
```

**Multi-file solutions:**
```
my-solution/
  solution.yaml       # root — references other files via compose
  resolvers.yaml       # ← Not included in build
  workflow.yaml        # ← Not included in build
```

After `scafctl build solution ./solution.yaml && scafctl catalog push ...`, a consumer running `scafctl run solution my-solution@1.0.0` on a different machine will get file-not-found errors for every local reference, and network errors for unavailable catalog dependencies.

---

## Design Goals

1. **Solutions built from local files must be self-contained** — all referenced files travel with the artifact.
2. **Zero-config for statically analyzable paths** — if scafctl can see a literal path in the YAML, it should bundle the file automatically.
3. **Explicit inclusion for dynamic paths** — when paths are computed at runtime (CEL, Go template, resolver binding), the author must declare what to include.
4. **Multi-file solutions merge into one at build time** — solutions split across multiple YAML files are composed into a single solution YAML in the artifact.
5. **Remote catalog dependencies are vendored** — catalog references discovered at build time are fetched and embedded in the artifact for offline, reproducible execution.
6. **Backward compatible build output** — existing solutions with no local file references produce identical artifacts.
7. **No execution-time behavior change** — bundled files are transparently available; providers do not need modification.

---

## Terminology

| Term | Definition |
|------|------------|
| **Bundle** | The collection of files (solution YAML + additional resources) packaged into a single OCI artifact |
| **Static path** | A file path that appears as a literal string in the solution YAML (e.g., `path: ./templates/main.tf.tmpl`) |
| **Dynamic path** | A file path computed at runtime via CEL (`expr:`), Go template (`tmpl:`), or resolver binding (`rslvr:`) |
| **Bundle root** | The directory containing the solution YAML file; all relative paths are resolved from here |
| **Manifest** | A JSON file within the bundle that maps original relative paths to their blob locations |
| **Compose** | The mechanism for splitting a solution across multiple YAML files that are merged at load/build time |
| **Vendored dependency** | A remote catalog artifact fetched at build time and embedded in the bundle |

---

## Design

### Approach: Multi-Layer OCI Artifact with File Manifest

The solution artifact transitions from a single content layer to a multi-layer OCI artifact:

| Layer | Media Type | Content |
|-------|-----------|---------|
| 0 | `application/vnd.scafctl.solution.v1+yaml` | Solution YAML (unchanged) |
| 1 | `application/vnd.scafctl.solution.bundle.v1+tar` | Tar archive of bundled files |

A **bundle manifest** is embedded inside the tar at `.scafctl/bundle-manifest.json`:

```json
{
  "version": 1,
  "root": ".",
  "files": [
    { "path": "templates/main.tf.tmpl", "size": 1234, "digest": "sha256:abc123..." },
    { "path": "child.yaml", "size": 567, "digest": "sha256:def456..." }
  ],
  "plugins": [
    { "name": "aws-provider", "kind": "provider", "version": "^1.5.0" },
    { "name": "vault-auth", "kind": "auth-handler", "version": "~1.2.0" }
  ]
}
```

When no files need bundling, layer 1 is omitted, preserving backward compatibility.

---

### File Discovery

File discovery happens during `scafctl build solution` and employs two complementary strategies:

#### 1. Static Analysis (Automatic)

The build command walks the parsed solution YAML and extracts literal file paths from known provider inputs:

| Provider | Input Field | Example |
|----------|------------|---------|
| `file` | `path` (when `operation` is `read`) | `path: ./templates/main.tf.tmpl` |
| `solution` | `source` (when it's a relative file path) | `source: "./child.yaml"` |

**Rules:**
- Only literal `ValueRef` values are analyzed (not `expr:`, `tmpl:`, or `rslvr:` forms).
- Paths starting with `./`, `../`, or lacking a scheme/`@` are treated as local file paths.
- Catalog references (e.g., `deploy-to-k8s@2.0.0`) and URLs (`https://...`) are excluded.
- Absolute paths are rejected during build with a clear error — bundled solutions must use relative paths.
- Discovered paths are resolved relative to the bundle root (the directory containing the solution YAML).

**Recursive discovery for sub-solutions:** When a sub-solution is discovered via the `solution` provider, the build command recursively analyzes the sub-solution's YAML for its own local file references. All paths are normalized relative to the parent solution's bundle root.

#### 2. Explicit Includes (Author-Declared)

For files referenced via dynamic paths (CEL, Go templates, resolver bindings), the solution author declares them in the top-level `bundle` section:

```yaml
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: dynamic-paths-example
  version: 1.0.0

bundle:
  include:
    - templates/**/*.tmpl
    - configs/*.yaml
    - scripts/setup.sh

spec:
  resolvers:
    templatePath:
      resolve:
        with:
          - provider: cel
            inputs:
              expression: "'templates/' + _.env + '/main.tf.tmpl'"
```

**`bundle.include` specification:**

| Field | Type | Description |
|-------|------|-------------|
| `include` | `[]string` | Glob patterns or explicit file paths, relative to the bundle root |

**Glob support:** Standard Go `doublestar` glob patterns (`**` for recursive matching, `*` for single-level).

**Deduplication:** Files matched by both static analysis and explicit includes are bundled once.

**Validation:** At build time, every include pattern must match at least one file. Patterns that match nothing produce a warning (not an error) to avoid breaking builds when optional files don't exist yet.

#### Why `bundle` at the Top Level (Not Under `spec`)?

The solution YAML separates concerns by top-level section:

| Section | Concern | Affects Execution? |
|---------|---------|--------------------|
| `metadata` | What the solution *is* | No |
| `catalog` | How it's *distributed* | No |
| `compose` | How it's *authored* on disk | No |
| `bundle` | How it's *packaged* | No |
| `spec` | How it *executes* | **Yes** |

`bundle` is build-time packaging metadata. Placing it under `spec` would blur the boundary between "what runs" and "how it's built." The existing precedent — `catalog` is already top-level despite being non-execution metadata — confirms this pattern. Everything under `spec` is execution-relevant; everything outside `spec` is lifecycle metadata.

### Plugin Dependencies

Solutions that use external plugins (providers or auth handlers distributed as OCI artifacts) declare them in `bundle.plugins`. This ensures that when a solution is built and pushed to a catalog, all required plugins are recorded and can be resolved at runtime.

#### Plugin Declaration

Each plugin entry has a `kind` field to distinguish plugin types:

```yaml
bundle:
  include:
    - templates/**/*.tmpl
  plugins:
    - name: aws-provider
      kind: provider
      version: "^1.5.0"
      defaults:
        region: us-east-1                  # literal
        output_format: json                # literal
        account_id:
          expr: "params.aws_account_id"    # CEL — resolved at execution time
    - name: azure-provider
      kind: provider
      version: ">=2.0.0"
    - name: vault-auth
      kind: auth-handler
      version: "~1.2.0"
```

**Why a `kind` field instead of grouped sections?**

Alternatives considered:
- **Grouped sections** (`bundle.providers`, `bundle.authHandlers`): Works for two types but doesn't extend cleanly if future plugin kinds are added. Separate arrays fragment the plugin list.
- **Infer from catalog metadata**: Requires fetching each plugin at build time to determine its type. Fails for offline builds and adds latency.
- **Flat list with `kind` field** (chosen): Each entry self-describes. Adding a new plugin kind in the future requires no schema changes — just a new `kind` value. The full list is visible in one place.

#### Plugin Defaults

The `defaults` map provides default values for all providers/auth-handlers from that plugin. Values use the full `ValueRef` type — supporting literal values, CEL expressions (`expr:`), Go templates (`tmpl:`), and resolver bindings (`rslvr:`). Defaults are shallow-merged beneath inline provider inputs at execution time:

```yaml
# In bundle.plugins:
plugins:
  - name: aws-provider
    kind: provider
    version: "^1.5.0"
    defaults:
      region: us-east-1
      output_format: json

# In spec — inline inputs override defaults:
spec:
  resolvers:
    s3Bucket:
      resolve:
        with:
          - provider: aws-provider
            inputs:
              operation: create-bucket
              bucket_name: my-bucket
              region: eu-west-1           # ← overrides default "us-east-1"
              # output_format: json       # ← inherited from defaults
```

**Merge semantics:**
1. Start with `defaults` from the matching `bundle.plugins` entry.
2. Overlay with inline `inputs` from the provider usage.
3. Inline always wins — no deep merge, no conflict errors.
4. If no `defaults` are declared, behavior is unchanged from today.

**Defaults support the full `ValueRef` type** — literal values, `expr:` (CEL), `tmpl:` (Go template), and `rslvr:` (resolver binding). This means defaults can reference parameters, other providers, metadata, and CEL functions:

```yaml
plugins:
  - name: aws-provider
    kind: provider
    version: "^1.5.0"
    defaults:
      region: us-east-1                              # literal
      account_id:
        expr: "params.aws_account_id"                 # CEL — resolved at execution time
      naming_prefix:
        tmpl: "{{ .metadata.name }}-{{ .params.env }}" # Go template
      vpc_id:
        rslvr: network-setup                           # resolver binding
```

**DAG integration:** Defaults are merged beneath inline inputs *before* DAG construction. The DAG analyzes the merged result, so:
- If a default contains `expr: "providers.vpc.cidr"`, a DAG edge is created from `vpc` → current provider.
- If an inline input overrides that default with a literal value, the edge disappears naturally — DAG construction operates on the merged result, not the defaults in isolation.
- No special handling is needed — defaults are "pre-filled inputs" and the existing resolution and DAG machinery handles them transparently after the merge step.

**Available execution-time context in default expressions:**
- `params.*` — user-supplied parameters
- `providers.<name>.*` — outputs from previously-executed providers
- `metadata.*` — solution metadata
- CEL functions (env vars, etc.)

#### Build-Time Plugin Handling

During `scafctl build solution`:
1. **Validate** that all `bundle.plugins` entries have valid `name`, `kind`, and `version` fields.
2. **Record** plugin dependencies in the bundle manifest for auditability.
3. Plugins are **not vendored** into the bundle — they are binary artifacts executed via gRPC, not YAML content. The lock file records resolved versions and digests.

During `scafctl run solution` from a catalog artifact:
1. **Read** plugin declarations from the bundle manifest.
2. **Resolve** each plugin from the local plugin cache or remote catalog, respecting version constraints.
3. **Fail fast** with a clear error if a required plugin is not available and cannot be fetched.

#### Why Plugins Are Not Under a `dependencies` Section

Alternatives considered:
- **Central `dependencies` section**: Groups all external references (catalog, plugins) in one place. But plugins require `kind`, `defaults`, and binary-specific handling that catalog references don't need. Mixing them overcomplicates the schema.
- **No formal declaration**: Discover plugins implicitly from provider names in `spec`. This fails for version pinning and defaults.
- **Under `bundle`** (chosen): Plugins are packaging-and-distribution metadata, not execution logic. They sit alongside `include` (files to bundle) as "external things this solution needs." Catalog reference versioning is handled by vendoring and the lock file.

---

#### Why Not a Separate Manifest File?

Alternatives considered:
- **`.scafctlbundle` file**: Adds a second file to track alongside the solution YAML. Easy to forget.
- **`bundle.yaml`**: Same issue — two files that must stay in sync.
- **Inline in solution YAML** (chosen): Single file, version-controlled together, validated during build.

The `bundle` section sits at the top level alongside `metadata`, `catalog`, `compose`, and `spec`. It has no effect on execution — it is build-time metadata only.

---

### Build-Time Behavior

The `scafctl build solution` command gains the following behavior:

```
scafctl build solution ./my-solution.yaml
```

1. **Parse** the solution YAML.
2. **Compose**: If `compose` is present, load and merge all composed files into a single solution. Validate merge rules (no duplicate resolvers/actions, no conflicting top-level fields).
3. **Static analysis**: Walk the merged spec and extract literal file paths from `file` provider `path` inputs and `solution` provider `source` inputs. Identify catalog references for vendoring.
4. **Explicit includes**: Expand `bundle.include` glob patterns relative to the bundle root. Filter against `.scafctlignore`.
5. **Plugin validation**: Validate all `bundle.plugins` entries (name, kind, version). Record in bundle manifest.
6. **Vendor catalog dependencies**: Fetch discovered catalog references from local/remote catalogs. Store their YAML in `.scafctl/vendor/` within the bundle. Rewrite `source` values in the merged solution to point to vendored paths.
7. **Merge and deduplicate** the file list.
8. **Validate**:
   - All discovered files exist on disk.
   - All paths are relative (no absolute paths).
   - No path escapes the bundle root (no `../../etc/passwd`).
   - No symlinks pointing outside the bundle root.
   - Total bundle size does not exceed a configurable limit (default: 50 MB).
9. **Package**: Create a tar archive of the discovered files (including vendored dependencies), preserving directory structure relative to the bundle root.
10. **Store**: Push the merged solution YAML as layer 0 and the tar archive as layer 1 to the OCI store.

#### New Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--no-bundle` | `false` | Skip file bundling; store only the solution YAML (legacy behavior) |
| `--no-vendor` | `false` | Skip vendoring catalog dependencies |
| `--bundle-max-size` | `50MB` | Maximum total size of bundled files |
| `--dry-run` | `false` | Show what would be bundled without building |

#### Dry-Run Output

```bash
$ scafctl build solution ./solution.yaml --dry-run

Bundle analysis for ./solution.yaml:

  Composed files:
    resolvers.yaml                 (2.1 KB)  → merged into solution
    workflow.yaml                  (1.8 KB)  → merged into solution

  Static analysis discovered:
    templates/main.tf.tmpl         (1.2 KB)
    templates/auto.tfvars.tmpl     (0.4 KB)
    child.yaml                     (0.6 KB)

  Explicit includes (bundle.include):
    configs/dev.yaml               (0.8 KB)
    configs/prod.yaml              (0.9 KB)

  Vendored catalog dependencies:
    deploy-to-k8s@2.0.0           (3.2 KB)  → .scafctl/vendor/deploy-to-k8s@2.0.0.yaml

  Plugin dependencies:
    aws-provider (provider)        ^1.5.0   defaults: region, output_format
    vault-auth (auth-handler)      ~1.2.0

  Total: 5 bundled files + 1 vendored dependency + 2 plugins, 7.1 KB

  Would build: dynamic-paths-example@1.0.0
```

---

### Runtime Behavior (Execution from Catalog)

When `scafctl run solution my-solution@1.0.0` loads an artifact from the catalog:

1. **Detect bundle**: Check if the manifest has more than one layer.
2. **Extract bundle**: If a bundle layer exists, extract the tar archive to a temporary directory.
3. **Set working directory context**: The solution executes with the temporary directory as its effective working directory for file resolution. This is transparent to providers — the `file` provider resolves relative paths against the working directory as it does today.
4. **Cleanup**: The temporary directory is removed after execution completes (or on error).

This approach requires **no changes to existing providers**. The `file` provider, `solution` provider, and all other providers continue to resolve relative paths against the working directory. The only change is that `scafctl run` sets the working directory to the extracted bundle directory when running a catalog artifact.

---

### Path Rewriting

**No path rewriting is performed for local file references.** All local paths in the solution YAML remain as authored. The runtime extracts bundled files into a directory structure that mirrors the original layout, so relative path resolution works identically to local development.

**Catalog references are rewritten to vendored paths.** When a catalog reference like `deploy-to-k8s@2.0.0` is vendored, the solution's `source` value is rewritten to `.scafctl/vendor/deploy-to-k8s@2.0.0.yaml`. This ensures the solution provider loads the vendored copy rather than attempting a catalog lookup at runtime.

For sub-solutions, the bundle preserves the sub-solution's path relative to the parent:

```
bundle-root/
  solution.yaml              # parent (merged from compose files)
  child.yaml                 # source: "./child.yaml"
  templates/
    main.tf.tmpl              # path: templates/main.tf.tmpl
  .scafctl/
    bundle-manifest.json
    vendor/
      deploy-to-k8s@2.0.0.yaml   # vendored catalog dependency
```

---

### OCI Artifact Structure (Before and After)

**Before (current):**
```
Manifest
├── Config: solution metadata JSON
└── Layer 0: solution.yaml (application/vnd.scafctl.solution.v1+yaml)
```

**After (with bundled files):**
```
Manifest
├── Config: solution metadata JSON
├── Layer 0: solution.yaml (application/vnd.scafctl.solution.v1+yaml)  ← merged from compose files
└── Layer 1: bundle.tar (application/vnd.scafctl.solution.bundle.v1+tar)
              ├── .scafctl/bundle-manifest.json
              ├── .scafctl/vendor/deploy-to-k8s@2.0.0.yaml
              ├── templates/main.tf.tmpl
              ├── templates/auto.tfvars.tmpl
              └── child.yaml
```

**After (no files to bundle — backward compatible):**
```
Manifest
├── Config: solution metadata JSON
└── Layer 0: solution.yaml (application/vnd.scafctl.solution.v1+yaml)
```

---

### Solution Struct Changes

New `Compose` and `Bundle` fields are added to the `Solution` struct:

```go
type Solution struct {
    APIVersion string   `json:"apiVersion" yaml:"apiVersion"`
    Kind       string   `json:"kind" yaml:"kind"`
    Metadata   Metadata `json:"metadata" yaml:"metadata"`
    Catalog    Catalog  `json:"catalog,omitempty" yaml:"catalog,omitempty"`
    Compose    []string `json:"compose,omitempty" yaml:"compose,omitempty" doc:"Relative paths to partial YAML files merged into this solution" maxItems:"100"`
    Bundle     Bundle   `json:"bundle,omitempty" yaml:"bundle,omitempty"`
    Spec       Spec     `json:"spec,omitempty" yaml:"spec,omitempty"`
}

// Bundle defines files and plugins to include when building a solution into a catalog artifact.
// This section is build-time metadata only and does not affect execution.
type Bundle struct {
    // Include is a list of glob patterns or explicit file paths to bundle.
    // Paths are relative to the directory containing the solution YAML file.
    Include []string `json:"include,omitempty" yaml:"include,omitempty" doc:"Glob patterns or file paths to include in the bundle" maxItems:"1000"`
    // Plugins declares external plugins required by this solution.
    Plugins []PluginDependency `json:"plugins,omitempty" yaml:"plugins,omitempty" doc:"External plugins required by this solution" maxItems:"50"`
}

// PluginDependency declares an external plugin required by a solution.
type PluginDependency struct {
    // Name is the plugin's catalog reference (e.g., "aws-provider").
    Name string `json:"name" yaml:"name" doc:"Plugin catalog reference" example:"aws-provider"`
    // Kind is the plugin type.
    Kind PluginKind `json:"kind" yaml:"kind" doc:"Plugin type" example:"provider"`
    // Version is a semver constraint (e.g., "^1.5.0", ">=2.0.0", "3.1.2").
    Version string `json:"version" yaml:"version" doc:"Semver version constraint" example:"^1.5.0" pattern:"^[~^>=<]*[0-9]" patternDescription:"semver constraint"`
    // Defaults provides default values for plugin inputs using the full ValueRef type.
    // Supports literal values, expr (CEL), tmpl (Go template), and rslvr (resolver binding).
    // These are shallow-merged beneath inline provider inputs (inline always wins).
    Defaults map[string]spec.ValueRef `json:"defaults,omitempty" yaml:"defaults,omitempty" doc:"Default input values for this plugin (supports ValueRef)"`
}

type PluginKind string

const (
    PluginKindProvider    PluginKind = "provider"
    PluginKindAuthHandler PluginKind = "auth-handler"
)
```

---

### Static Analysis Implementation

A new package `pkg/solution/bundler` handles file discovery and composition:

```go
package bundler

// Compose loads and merges all composed files referenced by the solution.
// Returns a new Solution with all parts merged. The original is not modified.
func Compose(sol *solution.Solution, bundleRoot string) (*solution.Solution, error)

// DiscoverFiles performs static analysis on a parsed (and composed) solution
// to find local file references and catalog references, then combines them
// with explicit bundle includes.
//
// Returns deduplicated lists of local files and catalog references.
func DiscoverFiles(sol *solution.Solution, bundleRoot string) (*DiscoveryResult, error)

// DiscoveryResult contains all files and dependencies discovered during analysis.
type DiscoveryResult struct {
    // LocalFiles are local file paths relative to the bundle root.
    LocalFiles []FileEntry
    // CatalogRefs are catalog references to vendor.
    CatalogRefs []CatalogRefEntry
}

// FileEntry represents a local file to be bundled.
type FileEntry struct {
    // RelPath is the path relative to the bundle root.
    RelPath string
    // Source indicates how the file was discovered.
    Source  DiscoverySource
}

// CatalogRefEntry represents a catalog dependency to vendor.
type CatalogRefEntry struct {
    // Ref is the original catalog reference (e.g., "deploy-to-k8s@2.0.0").
    Ref string
    // VendorPath is the path within the bundle where the vendored artifact is stored.
    VendorPath string
}

type DiscoverySource int

const (
    StaticAnalysis  DiscoverySource = iota
    ExplicitInclude
)
```

The static analysis walker inspects resolver and action provider inputs:

1. For each resolver's `resolve.with` entries, check if `provider == "file"` and if the `path` input is a literal string.
2. For each resolver's `resolve.with` entries, check if `provider == "solution"` and if the `source` input is a literal string — classify as local path or catalog reference.
3. For each action, check if `provider == "file"` and if the `path` input is a literal string.
4. Repeat for `transform.with` entries that use the `file` provider.
5. For discovered local sub-solution files, recursively analyze the sub-solution.
6. For discovered catalog references, record for vendoring.

Dynamic `ValueRef` forms (`expr:`, `tmpl:`, `rslvr:`) are skipped — these are the author's responsibility to declare in `bundle.include`.

---

### Catalog Store Changes

The `LocalCatalog.Store` method signature gains an optional second layer:

```go
// Store saves an artifact to the catalog.
// For solutions with bundled files, bundleData contains the tar archive.
// If bundleData is nil, only the primary content layer is stored.
func (c *LocalCatalog) Store(ctx context.Context, ref Reference, content []byte,
    bundleData []byte, annotations map[string]string, force bool) (ArtifactInfo, error)
```

When `bundleData` is non-nil, a second layer is added to the OCI manifest:

```go
layers := []ocispec.Descriptor{contentDesc}
if bundleData != nil {
    bundleDesc, err := c.pushBlob(ctx, MediaTypeSolutionBundle, bundleData)
    if err != nil { ... }
    layers = append(layers, bundleDesc)
}
```

The `Fetch` method is updated to return all layers, not just the first:

```go
// FetchSolution returns the solution YAML and, if present, the bundle tar.
func (c *LocalCatalog) FetchSolution(ctx context.Context, ref Reference) (
    solutionYAML []byte, bundleTar []byte, info ArtifactInfo, err error)
```

---

### Catalog Reference Vendoring

When static analysis discovers catalog references (e.g., `deploy-to-k8s@2.0.0`), the build command vendors them:

1. **Resolve** the catalog reference using the same resolution logic as `scafctl run solution` (local catalog → remote catalogs).
2. **Fetch** the artifact content (solution YAML).
3. **Store** the fetched content at `.scafctl/vendor/<name>@<version>.yaml` within the bundle.
4. **Rewrite** the `source` value in the merged solution YAML from the catalog reference to the vendored path.
5. **Record** the original reference, resolved version, and digest in the bundle manifest for auditability.

#### Versioned and Unversioned References

| Reference | Build Behavior |
|-----------|---------------|
| `deploy-to-k8s@2.0.0` | Fetched and pinned at exactly 2.0.0 |
| `deploy-to-k8s` (bare name) | Resolved to highest semver, vendored at that version. A warning is emitted recommending version pinning. |

#### Lock File (`solution.lock`)

After the first successful build, a `solution.lock` file is written alongside the solution YAML:

```yaml
# Auto-generated by scafctl build. Do not edit.
version: 1
dependencies:
  - ref: deploy-to-k8s@2.0.0
    digest: sha256:abc123...
    resolvedFrom: registry.example.com/solutions/deploy-to-k8s
    vendoredAt: .scafctl/vendor/deploy-to-k8s@2.0.0.yaml
plugins:
  - name: aws-provider
    kind: provider
    version: "^1.5.0"
    resolved: 1.5.3
    digest: sha256:789abc...
    resolvedFrom: registry.example.com/plugins/aws-provider
  - name: vault-auth
    kind: auth-handler
    version: "~1.2.0"
    resolved: 1.2.4
    digest: sha256:def012...
    resolvedFrom: registry.example.com/plugins/vault-auth
```

Subsequent builds replay the lock file to ensure reproducibility. Use `--update-lock` to re-resolve and update the lock.

#### Opting Out

Use `--no-vendor` to skip catalog vendoring. The solution will reference catalog dependencies at runtime, requiring network access.

#### Recursive Vendoring

If a vendored sub-solution itself references other catalog dependencies, those are vendored recursively. Circular references are detected and rejected.

---

### Security Considerations

| Threat | Mitigation |
|--------|----------|
| Path traversal (`../../../etc/passwd`) | Validate all paths stay within the bundle root |
| Symlink escape | Resolve symlinks and verify targets remain within the bundle root |
| Zip/tar bomb | Enforce `--bundle-max-size` limit (default 50 MB) |
| Sensitive file inclusion | Build dry-run shows all files; `.scafctlignore` filters apply |
| Binary file inclusion | Files are included as-is; no execution occurs during build |
| Vendored artifact tampering | Digest recorded in lock file; verified on subsequent builds |

---

### `.scafctlignore`

A `.scafctlignore` file in the bundle root controls which files are excluded from bundling. It uses the same syntax as `.gitignore`.

```
# .scafctlignore
*.test.yaml
testdata/
*.bak
.env
secrets/
```

**Why `.scafctlignore` instead of `.gitignore`?**

Different tools have different inclusion needs:
- Generated files might be git-ignored but needed in a bundle.
- Test fixtures might be tracked in git but shouldn't ship in an artifact.
- `.env` files might be in `.gitignore` already, but relying on that conflates two concerns.

A dedicated `.scafctlignore` gives precise, purpose-specific control. There is no fallback to `.gitignore` — this avoids the confusion of mixing two ignore systems.

Files explicitly listed by name in `bundle.include` (not via glob) bypass `.scafctlignore` — if the author names a specific file, it's intentional.

---

## Example: Complete Solution with Bundling

### Directory Structure

```
my-solution/
  solution.yaml          # root solution file
  resolvers.yaml         # composed: resolver definitions
  workflow.yaml          # composed: action definitions
  templates/
    main.tf.tmpl
    auto.tfvars.tmpl
    dev/main.tf.tmpl     # environment-specific template
    prod/main.tf.tmpl
  configs/
    dev.yaml
    prod.yaml
  infra/
    database.yaml        # local sub-solution
  .scafctlignore
```

### Solution Files

```yaml
# solution.yaml — root
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: terraform-scaffold
  version: 2.0.0
  description: Scaffold Terraform environments with bundled templates

compose:
  - resolvers.yaml
  - workflow.yaml

bundle:
  include:
    # Dynamic paths computed via CEL — must be explicitly declared
    - templates/**/*.tmpl
    # Shared configuration files
    - configs/*.yaml
  plugins:
    - name: aws-provider
      kind: provider
      version: "^1.5.0"
      defaults:
        region: us-east-1
```

```yaml
# resolvers.yaml — composed partial
resolvers:
  # Static path — automatically discovered, no bundle.include needed
  mainTfTemplate:
    description: main.tf template content
    resolve:
      with:
        - provider: file
          inputs:
            operation: read
            path: templates/main.tf.tmpl  # ← auto-discovered

  # Dynamic path — requires bundle.include entry above
  envConfig:
    description: Environment-specific configuration
    resolve:
      with:
        - provider: file
          inputs:
            operation: read
            path:
              expr: "'configs/' + _.env + '.yaml'"  # ← dynamic, covered by include glob

  # Local sub-solution — auto-discovered
  childResult:
    resolve:
      with:
        - provider: solution
          inputs:
            source: "./infra/database.yaml"  # ← auto-discovered

  # Remote catalog dependency — auto-vendored
  k8sDeployment:
    resolve:
      with:
        - provider: solution
          inputs:
            source: "deploy-to-k8s@2.0.0"  # ← vendored at build time
```

```yaml
# workflow.yaml — composed partial
workflow:
  actions:
    deploy:
      description: Deploy infrastructure
      provider: exec
      inputs:
        command: terraform apply -auto-approve
```

### Build

```bash
$ scafctl build solution ./solution.yaml

  Composed 2 files into solution
  Bundled 7 files (15.5 KB)
  Vendored 1 catalog dependency
  Built terraform-scaffold@2.0.0
    Digest: sha256:abc123...
    Catalog: ~/.scafctl/catalog
```

---

## Alternatives Considered

### 1. Inline All File Content into the Solution YAML

Embed file contents directly in the YAML using multi-line strings or base64. This avoids the bundling problem entirely but makes solutions unreadable, breaks editor tooling for template files, and doesn't scale.

**Rejected**: Poor developer experience.

### 2. Require All Files in a Single Directory, Bundle the Entire Directory

Always tar the entire solution directory. Simple but includes unnecessary files (tests, docs, build artifacts) and provides no control.

**Rejected**: Too coarse-grained; wastes space and risks leaking sensitive files.

### 3. External Manifest File (`.scafctlbundle`)

A separate file listing all files to include. Works but adds cognitive overhead — developers must remember to update two files.

**Rejected**: Easy to forget; inline `bundle` section is simpler.

### 4. Only Explicit Includes, No Static Analysis

Require the author to list every file, even those with literal paths. Safest but adds friction for the common case.

**Rejected**: Unnecessarily tedious for the 80% case where paths are static.

---

## Bundle Verification (`scafctl bundle verify`)

Validate that a built artifact contains all files needed for execution by performing a dry-run resolve against the bundled files.

### Command Specification

```bash
scafctl bundle verify <artifact-ref>
```

| Argument | Description |
|----------|-------------|
| `<artifact-ref>` | Catalog reference (e.g., `my-solution@1.0.0`) or path to a local `.tar` bundle |

| Flag | Default | Description |
|------|---------|-------------|
| `--params` | `{}` | JSON object of parameter values to use during verification |
| `--params-file` | — | Path to a YAML/JSON file containing parameter values |
| `--strict` | `false` | Fail on warnings (e.g., unreachable dynamic paths) |

### Verification Steps

1. **Extract bundle** to a temporary directory.
2. **Parse** the solution YAML and construct the resolver/action DAG.
3. **Static path check:** For every literal `path` or `source` in the solution, verify the file exists in the extracted bundle.
4. **Glob coverage check:** For every `bundle.include` pattern, verify at least one matching file exists.
5. **Vendored dependency check:** For every catalog reference rewritten to a vendored path, verify the vendored file exists.
6. **Plugin availability check:** For every `bundle.plugins` entry, verify the plugin can be resolved (local cache or registry).
7. **Dry-run resolve (optional with `--params`):** Execute resolvers in dry-run mode to catch runtime path errors that depend on parameter values.

### Output

```bash
$ scafctl bundle verify my-solution@1.0.0 --params '{"env": "prod"}'

Verifying my-solution@1.0.0...

  Static paths:
    ✓ templates/main.tf.tmpl
    ✓ child.yaml
    ✓ configs/base.yaml

  Dynamic paths (with --params):
    ✓ configs/prod.yaml (from expr: 'configs/' + _.env + '.yaml')

  Vendored dependencies:
    ✓ .scafctl/vendor/deploy-to-k8s@2.0.0.yaml

  Plugins:
    ✓ aws-provider@1.5.3 (provider)
    ✓ vault-auth@1.2.4 (auth-handler)

Verification passed: 6 files, 1 vendored dependency, 2 plugins
```

### Error Example

```bash
$ scafctl bundle verify broken-solution@1.0.0

Verifying broken-solution@1.0.0...

  Static paths:
    ✓ templates/main.tf.tmpl
    ✗ templates/missing.tf.tmpl — not found in bundle

  Vendored dependencies:
    ✗ .scafctl/vendor/old-dep@0.5.0.yaml — not found in bundle

Verification failed: 2 errors
```

### Implementation Notes

- Reuses the static analysis walker from `pkg/solution/bundler`.
- Dry-run resolve leverages the existing resolver execution engine with a `--dry-run` context flag that skips side effects.
- Exit code 0 on success, 1 on verification failure.

---

## Bundle Diffing (`scafctl bundle diff`)

Show what changed between two versions of a bundled artifact, enabling informed upgrade decisions and change auditing.

### Command Specification

```bash
scafctl bundle diff <ref-a> <ref-b>
```

| Argument | Description |
|----------|-------------|
| `<ref-a>` | First artifact reference (e.g., `my-solution@1.0.0`) |
| `<ref-b>` | Second artifact reference (e.g., `my-solution@2.0.0`) |

| Flag | Default | Description |
|------|---------|-------------|
| `--output` | `text` | Output format: `text`, `json`, `yaml` |
| `--files-only` | `false` | Show only file changes, skip solution YAML diff |
| `--solution-only` | `false` | Show only solution YAML diff, skip file changes |
| `--ignore` | — | Glob patterns to exclude from diff (repeatable) |

### Diff Categories

1. **Solution YAML diff:** Structural comparison of the merged solution (resolvers added/removed/modified, actions changed, metadata updates).
2. **Bundled files diff:** Files added, removed, or modified between versions.
3. **Vendored dependencies diff:** Catalog dependencies added, removed, or version-changed.
4. **Plugin dependencies diff:** Plugin version constraint or default value changes.

### Output

```bash
$ scafctl bundle diff my-solution@1.0.0 my-solution@2.0.0

Comparing my-solution@1.0.0 → my-solution@2.0.0

Solution YAML:
  resolvers:
    + newResolver              (added)
    ~ mainTfTemplate           (modified: provider inputs changed)
    - legacyResolver           (removed)
  workflow.actions:
    ~ deploy                   (modified: command changed)

Bundled files:
    + templates/new.tf.tmpl                    (added, 1.2 KB)
    ~ templates/main.tf.tmpl                   (modified, +15 -3 lines)
    - templates/old.tf.tmpl                    (removed)

Vendored dependencies:
    ~ deploy-to-k8s@2.0.0 → deploy-to-k8s@2.1.0   (upgraded)
    + logging-sidecar@1.0.0                        (added)

Plugins:
    ~ aws-provider: ^1.5.0 → ^1.6.0            (constraint changed)
      defaults.region: us-east-1 → us-west-2   (default changed)

Summary: 2 resolvers changed, 3 files changed, 1 dependency upgraded, 1 plugin updated
```

### JSON Output Structure

```json
{
  "refA": "my-solution@1.0.0",
  "refB": "my-solution@2.0.0",
  "solution": {
    "resolvers": {
      "added": ["newResolver"],
      "removed": ["legacyResolver"],
      "modified": ["mainTfTemplate"]
    },
    "actions": {
      "modified": ["deploy"]
    }
  },
  "files": {
    "added": [{"path": "templates/new.tf.tmpl", "size": 1234}],
    "removed": [{"path": "templates/old.tf.tmpl"}],
    "modified": [{"path": "templates/main.tf.tmpl", "linesAdded": 15, "linesRemoved": 3}]
  },
  "vendoredDependencies": {
    "upgraded": [{"name": "deploy-to-k8s", "from": "2.0.0", "to": "2.1.0"}],
    "added": [{"name": "logging-sidecar", "version": "1.0.0"}]
  },
  "plugins": {
    "modified": [{
      "name": "aws-provider",
      "versionFrom": "^1.5.0",
      "versionTo": "^1.6.0",
      "defaultsChanged": {"region": {"from": "us-east-1", "to": "us-west-2"}}
    }]
  }
}
```

### Implementation Notes

- Extracts both bundles to temporary directories.
- Solution YAML diff uses deep structural comparison (not line-by-line text diff) to produce semantic differences.
- File content diff uses standard unified diff format internally; summary shows line counts.
- Vendored dependency comparison uses digest matching — same digest = unchanged regardless of filename.

---

## Selective Extraction (`scafctl bundle extract`)

Extract only the files needed for a specific resolver or action, enabling partial bundle inspection and reduced extraction for large bundles.

### Command Specification

```bash
scafctl bundle extract <artifact-ref> [--output-dir <dir>]
```

| Flag | Default | Description |
|------|---------|-------------|
| `--output-dir` | `.` | Directory to extract files into |
| `--resolver` | — | Extract only files needed by this resolver (repeatable) |
| `--action` | — | Extract only files needed by this action (repeatable) |
| `--include` | — | Additional glob patterns to extract (repeatable) |
| `--list-only` | `false` | List files that would be extracted without extracting |
| `--flatten` | `false` | Extract all files to a flat directory (no subdirectories) |

### Behavior

1. **No filters:** Extract all bundled files (equivalent to full extraction).
2. **With `--resolver` or `--action`:** Perform static analysis to determine which files are referenced by the specified resolver(s) or action(s), including transitive dependencies (e.g., if resolver A depends on sub-solution B, include B's files).
3. **With `--include`:** Add files matching the glob patterns to the extraction set.

### Output

```bash
$ scafctl bundle extract my-solution@1.0.0 --resolver mainTfTemplate --list-only

Files needed for resolver 'mainTfTemplate':
  templates/main.tf.tmpl       (1.2 KB)
  templates/auto.tfvars.tmpl   (0.4 KB)

Total: 2 files, 1.6 KB

$ scafctl bundle extract my-solution@1.0.0 --resolver mainTfTemplate --output-dir ./extracted

Extracted 2 files (1.6 KB) to ./extracted/
```

### Use Cases

- **Debugging:** Extract only the files used by a failing resolver to inspect them.
- **Auditing:** Review templates used by a specific action before approving a solution.
- **Partial deployment:** Extract configuration files for a specific environment without pulling the entire bundle.

### Implementation Notes

- Builds on the static analysis walker; each resolver/action's file dependencies are traced through the DAG.
- Resolver dependencies (via `rslvr:` bindings) are followed transitively.
- Dynamic paths (`expr:`, `tmpl:`) cannot be traced without parameter values; `--resolver` extraction for dynamic paths emits a warning and skips those files unless `--include` explicitly adds them.

---

## Content-Addressable Deduplication

When multiple solutions share the same template files, store them once in the OCI registry using content-addressable layers, reducing storage costs and push/pull times.

### Concept

Instead of embedding all files in a single tar layer, split the bundle into multiple layers:

| Layer | Content |
|-------|---------|
| 0 | Solution YAML |
| 1 | Bundle manifest (JSON) |
| 2+ | Individual files or file groups, each as a separate blob |

Each file blob is stored by its content digest. When two solutions include the same file (e.g., a shared `terraform-module.tf.tmpl`), the registry stores one blob referenced by both manifests.

### OCI Artifact Structure (Deduplicated)

```
Manifest (my-solution@1.0.0)
├── Config: solution metadata JSON
├── Layer 0: solution.yaml
├── Layer 1: bundle-manifest.json
├── Layer 2: sha256:abc123... (templates/main.tf.tmpl)
├── Layer 3: sha256:def456... (templates/shared-module.tf.tmpl)  ← shared
└── Layer 4: sha256:789abc... (child.yaml)

Manifest (other-solution@2.0.0)
├── Config: solution metadata JSON
├── Layer 0: solution.yaml
├── Layer 1: bundle-manifest.json
├── Layer 2: sha256:def456... (templates/shared-module.tf.tmpl)  ← same blob
└── Layer 3: sha256:fedcba... (config.yaml)
```

### Bundle Manifest (Deduplicated Format)

```json
{
  "version": 2,
  "root": ".",
  "files": [
    { "path": "templates/main.tf.tmpl", "digest": "sha256:abc123...", "layer": 2 },
    { "path": "templates/shared-module.tf.tmpl", "digest": "sha256:def456...", "layer": 3 },
    { "path": "child.yaml", "digest": "sha256:789abc...", "layer": 4 }
  ]
}
```

### Build Behavior

1. **Compute digests** for all files to be bundled.
2. **Check registry** for existing blobs matching each digest (using OCI blob existence check).
3. **Skip upload** for blobs that already exist; only push new content.
4. **Construct manifest** referencing all layers (existing and new).

### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--dedupe` | `true` | Enable content-addressable deduplication |
| `--dedupe-threshold` | `4KB` | Minimum file size for individual layer extraction (smaller files are tarred together) |

Files below `--dedupe-threshold` are grouped into a single tar layer to avoid excessive layer counts for many small files.

### Benefits

- **Storage efficiency:** Shared files stored once across the registry.
- **Faster pushes:** Only new/changed files are uploaded.
- **Faster pulls:** Layer caching means unchanged files aren't re-downloaded.

### Implementation Notes

- Requires manifest version bump (`"version": 2`) to distinguish from tar-based bundles.
- Backward compatible: older scafctl versions that don't understand version 2 fall back to full extraction (or fail gracefully with a version error).
- Registry must support OCI blob mount/cross-repository mounting for full deduplication benefit.

---

## Vendor Update (`scafctl vendor update`)

Re-resolve and update vendored dependencies without a full rebuild, enabling quick dependency updates without modifying source files.

### Command Specification

```bash
scafctl vendor update [solution-path]
```

| Argument | Description |
|----------|-------------|
| `[solution-path]` | Path to solution YAML (default: `./solution.yaml`) |

| Flag | Default | Description |
|------|---------|-------------|
| `--dependency` | — | Update only this dependency (repeatable); if omitted, update all |
| `--dry-run` | `false` | Show what would be updated without making changes |
| `--lock-only` | `false` | Update `solution.lock` without re-vendoring files |
| `--pre-release` | `false` | Include pre-release versions when resolving |

### Behavior

1. **Parse** the solution YAML and `solution.lock` file.
2. **Re-resolve** catalog references against current registry state, respecting version constraints.
3. **Compare** resolved versions against locked versions.
4. **Fetch** updated dependencies and write to `.scafctl/vendor/`.
5. **Update** `solution.lock` with new digests and versions.

### Output

```bash
$ scafctl vendor update --dry-run

Checking vendored dependencies for ./solution.yaml...

  deploy-to-k8s:
    locked:   2.0.0 (sha256:abc123...)
    latest:   2.1.0 (sha256:def456...)
    action:   would update

  logging-sidecar:
    locked:   1.0.0 (sha256:789abc...)
    latest:   1.0.0 (sha256:789abc...)
    action:   up to date

  aws-provider (plugin):
    locked:   1.5.3 (sha256:aaa111...)
    latest:   1.5.5 (sha256:bbb222...)
    action:   would update

Summary: 2 dependencies would be updated

$ scafctl vendor update

Updating vendored dependencies...

  ✓ deploy-to-k8s: 2.0.0 → 2.1.0
  ✓ aws-provider: 1.5.3 → 1.5.5
  • logging-sidecar: up to date

Updated solution.lock
```

### Selective Update

```bash
$ scafctl vendor update --dependency deploy-to-k8s

Updating deploy-to-k8s...
  ✓ 2.0.0 → 2.1.0

Updated solution.lock
```

### Lock File Changes

After `vendor update`, the `solution.lock` file reflects new resolved versions:

```yaml
version: 1
dependencies:
  - ref: deploy-to-k8s@2.1.0           # ← updated
    digest: sha256:def456...            # ← new digest
    resolvedFrom: registry.example.com/solutions/deploy-to-k8s
    vendoredAt: .scafctl/vendor/deploy-to-k8s@2.1.0.yaml
plugins:
  - name: aws-provider
    kind: provider
    version: "^1.5.0"
    resolved: 1.5.5                     # ← updated
    digest: sha256:bbb222...            # ← new digest
    resolvedFrom: registry.example.com/plugins/aws-provider
```

### Use Cases

- **Security patches:** Update a dependency to pick up a security fix without rebuilding.
- **Dependency hygiene:** Regularly update vendored dependencies to latest compatible versions.
- **Pre-flight check:** Use `--dry-run` before a release to see available updates.

### Implementation Notes

- Reuses catalog resolution logic from `scafctl build`.
- Version constraint evaluation uses existing semver library.
- Lock file update is atomic — written to a temp file, then renamed.
- If `--dependency` specifies a dependency not in the lock file, exit with an error.

---

## Future Enhancements

1. **`scafctl plugins install`** — Pre-fetch plugins declared in `bundle.plugins` for offline execution.
2. **Per-provider defaults within a plugin** — If a single plugin exposes multiple providers with different default needs, allow scoping defaults to individual provider names.

---

## Implementation Plan

### Phase 1: Multi-File Composition
- Add `Compose` field to `Solution` struct
- Implement `pkg/solution/bundler.Compose()` (load, merge, validate)
- Merge rules: resolvers by name, actions by name, bundle.include union
- Circular reference detection for recursive compose
- Update `scafctl run solution` to support compose at load time

### Phase 2: Bundle Infrastructure
- Add `Bundle` struct to `Solution`
- Implement static analysis and glob expansion in `pkg/solution/bundler`
- Add bundle tar creation utilities
- Add new media type constant `MediaTypeSolutionBundle`
- Implement `.scafctlignore` support

### Phase 3: Build Command Integration
- Update `scafctl build solution` to compose + discover + package
- Update `LocalCatalog.Store` to support multi-layer artifacts
- Add `--no-bundle`, `--no-vendor`, `--bundle-max-size`, `--dry-run` flags
- Security validations (path traversal, symlinks, size limits)

### Phase 4: Catalog Vendoring
- Implement catalog reference discovery in static analysis
- Fetch and store vendored artifacts in `.scafctl/vendor/`
- Source rewriting in merged solution YAML
- Lock file generation and replay (`solution.lock`)
- Recursive vendoring with circular reference detection

### Phase 5: Runtime Extraction
- Update `LocalCatalog.Fetch` / `FetchSolution` to return bundle layers
- Update `scafctl run solution` to extract bundle to temp directory
- Set working directory context for bundled solutions
- Update `RemoteCatalog` push/pull to handle multi-layer artifacts

### Phase 6: Plugin Dependencies
- Add `Plugins` field to `Bundle` struct with `PluginDependency` type
- Plugin validation during build (name, kind, version)
- Record plugin dependencies in bundle manifest and lock file
- Plugin resolution and version constraint checking at runtime
- ValueRef defaults merge implementation (shallow merge beneath inline inputs, DAG-aware)
- CLI integration tests for plugin declaration and resolution

### Phase 7: Bundle Verification
- Implement `scafctl bundle verify` command
- Static path, glob coverage, and vendored dependency checks
- Plugin availability check
- Dry-run resolve with `--params` support
- CLI integration tests

### Phase 8: Bundle Diffing
- Implement `scafctl bundle diff` command
- Structural solution YAML comparison (resolvers, actions, metadata)
- Bundled file diff with line-count summaries
- Vendored dependency and plugin diff
- `text`, `json`, `yaml` output formats
- CLI integration tests

### Phase 9: Selective Extraction
- Implement `scafctl bundle extract` command
- DAG-based file dependency tracing per resolver/action
- Transitive dependency following for sub-solutions
- `--list-only`, `--flatten`, `--include` flag support
- CLI integration tests

### Phase 10: Content-Addressable Deduplication
- Bundle manifest version 2 schema
- Per-file digest computation and OCI blob existence check
- Multi-layer artifact construction with individual file blobs
- `--dedupe-threshold` for small-file grouping
- Backward compatibility handling for version 1 manifests
- CLI integration tests

### Phase 11: Vendor Update
- Implement `scafctl vendor update` command
- Lock file parsing and comparison against registry state
- Selective update with `--dependency` flag
- Atomic lock file writes
- `--dry-run`, `--lock-only`, `--pre-release` flag support
- CLI integration tests

### Phase 12: Polish
- Warning diagnostics for unresolvable dynamic paths
- Dry-run output formatting
- End-to-end integration tests
- Documentation and examples

---

## Summary

Solution file bundling makes solutions portable by collecting all dependencies into the OCI artifact at build time. Multi-file composition lets developers split large solutions across files while producing a single merged YAML in the artifact. Static analysis handles the common case for local files automatically, while `bundle.include` gives explicit control over dynamically referenced files. Catalog reference vendoring embeds remote dependencies for offline, reproducible execution. Plugin dependencies declared in `bundle.plugins` ensure external providers and auth handlers are versioned, recorded in the lock file, and resolvable at runtime — with ValueRef-aware defaults reducing repetition across provider usages. Bundle verification (`scafctl bundle verify`) validates artifact completeness, bundle diffing (`scafctl bundle diff`) enables change auditing between versions, and selective extraction (`scafctl bundle extract`) supports targeted file inspection. Content-addressable deduplication reduces registry storage by sharing identical files across solutions, and `scafctl vendor update` enables dependency management without full rebuilds. The design preserves backward compatibility, requires no changes to existing providers, and follows OCI conventions by using multi-layer manifests.
