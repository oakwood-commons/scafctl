---
title: "Provider Extraction Tutorial"
weight: 145
---

# Provider Extraction Tutorial

This tutorial walks through extracting a built-in scafctl provider into an
external plugin repo. It covers the full pipeline: scaffold, port logic, test,
build, publish, and verify auto-fetch.

The examples use the `static` provider as the reference extraction, but the
workflow applies to any provider.

## Prerequisites

| Requirement | Version |
|-------------|---------|
| Go | 1.26+ |
| scafctl | Latest (`scafctl version`) |
| Plugin SDK | v0.1.1 (`github.com/oakwood-commons/scafctl-plugin-sdk`) |
| GoReleaser | v2+ (for multi-platform builds) |
| golangci-lint | v2.11.4+ (for CI) |

## Overview

```
1. Scaffold       Generates plugin project structure
2. Port logic     Copy builtin provider code, adapt to plugin interface
3. Port tests     Adapt test patterns for the plugin interface
4. Build + test   Compile and run tests locally
5. Verify parity  Confirm output matches the built-in version
6. GitHub repo    Push to GitHub with CI and rulesets
7. Multi-build    Cross-compile for all target platforms
8. Publish        Store as OCI image index in the local catalog
9. Auto-fetch     Test bundle.plugins catalog resolution
10. Remove        Remove the built-in and update scafctl core
```

## Step 1: Scaffold the Plugin Project

Use the plugin-template solution to generate the project structure:

~~~bash
scafctl run solution \
  ghcr.io/oakwood-commons/solutions/plugin-template:1.0.0 \
  --output-dir /tmp \
  -r name=scafctl-plugin-static \
  -r module=github.com/oakwood-commons/scafctl-plugin-static \
  -r "description=Returns a static value without performing any operations" \
  -r capabilities=from,transform \
  -r create_repo=false \
  -r repo_visibility=public \
  -r plugin_type=provider
~~~

This generates ~26 files:

```
scafctl-plugin-static/
  cmd/scafctl-plugin-static/main.go    # Plugin entrypoint
  internal/static/provider.go          # Provider implementation (stub)
  internal/static/provider_test.go     # Tests (stub)
  .goreleaser.yaml                     # Multi-platform build config
  .github/workflows/ci.yaml            # Lint + test CI
  .github/workflows/release.yaml       # Tagged release + OCI publish
  go.mod
  ...
```

## Step 2: Port the Built-in Provider Logic

Replace the scaffolded stub in `internal/static/provider.go` with the real
provider logic. The key differences between builtin and plugin interfaces:

| Aspect | Builtin | Plugin |
|--------|---------|--------|
| Interface | `provider.Provider` (2 methods) | `plugin.ProviderPlugin` (8 methods) |
| Imports | `"github.com/oakwood-commons/scafctl/pkg/provider"` | `sdkprovider "github.com/oakwood-commons/scafctl-plugin-sdk/provider"` |
| Schema helpers | `"github.com/oakwood-commons/scafctl/pkg/provider/schemahelper"` | `sdkhelper "github.com/oakwood-commons/scafctl-plugin-sdk/provider/schemahelper"` |
| Output type | `provider.Output{Data: value}` | `sdkprovider.Output{Data: value}` |
| Capabilities | `provider.Capability*` | `sdkprovider.Capability*` |
| Execution | Single `Execute(ctx, input any)` | `ExecuteProvider(ctx, name string, inputs map[string]any)` |
| Descriptor | `Descriptor() *provider.Descriptor` | `GetProviderDescriptor(ctx, name) (*sdkprovider.Descriptor, error)` |

### Example: Static Provider Port

The builtin static provider's `Execute` method:

```go
// Builtin -- pkg/provider/builtin/staticprovider/static.go
func (p *StaticProvider) Execute(_ context.Context, input any) (*provider.Output, error) {
    inputMap, ok := input.(map[string]any)
    if !ok {
        return nil, fmt.Errorf("expected map[string]any, got %T", input)
    }
    value := inputMap["value"]
    return &provider.Output{Data: value}, nil
}
```

Becomes:

```go
// Plugin -- internal/static/provider.go
func (p *Plugin) ExecuteProvider(
    _ context.Context,
    name string,
    inputs map[string]any,
) (*sdkprovider.Output, error) {
    if name != ProviderName {
        return nil, fmt.Errorf("unknown provider: %s", name)
    }
    value := inputs["value"]
    return &sdkprovider.Output{Data: value}, nil
}
```

Key changes:

- Input is already `map[string]any` -- no type assertion needed
- The `name` parameter identifies which provider is being called (plugins can
  host multiple providers)
- Return an error for unknown provider names

### Provider Registration

The plugin must implement `GetProviders()` to declare which providers it offers:

```go
func (p *Plugin) GetProviders() []string {
    return []string{ProviderName}
}
```

### APIVersion Convention

Use `"v1"` (not `"scafctl.io/v1"`) to match the builtin convention:

```go
func (p *Plugin) GetProviderDescriptor(
    _ context.Context,
    name string,
) (*sdkprovider.Descriptor, error) {
    return &sdkprovider.Descriptor{
        APIVersion: "v1",
        Name:       ProviderName,
        Version:    semver.MustNewVersion(Version),
        // ...
    }, nil
}
```

## Step 3: Port Tests

Adapt the builtin test patterns for the plugin interface:

```go
// Builtin test pattern
func TestExecute_String(t *testing.T) {
    p := New()
    output, err := p.Execute(context.Background(), map[string]any{
        "value": "hello",
    })
    require.NoError(t, err)
    assert.Equal(t, "hello", output.Data)
}

// Plugin test pattern
func TestExecuteProvider_String(t *testing.T) {
    p := &Plugin{}
    output, err := p.ExecuteProvider(context.Background(), "static", map[string]any{
        "value": "hello",
    })
    require.NoError(t, err)
    assert.Equal(t, "hello", output.Data)
}
```

Add tests for plugin-specific methods:

- `GetProviders` -- returns expected provider names
- `GetProviderDescriptor` -- returns correct schema and capabilities
- `ExecuteProvider` with unknown name -- returns error
- `DescribeWhatIf` -- if the builtin implements WhatIf

## Step 4: Build and Test Locally

~~~bash
cd /tmp/scafctl-plugin-static
go mod tidy
go build ./...
go test ./...
go vet ./...
~~~

Run linting with the project's `.golangci.yml`:

~~~bash
golangci-lint run ./...
~~~

## Step 5: Verify Output Parity

Create a test solution that uses the provider:

```yaml
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: parity-test
  version: 1.0.0
spec:
  resolvers:
    string-test:
      resolve:
        with:
          - provider: static
            inputs:
              value: "hello"
    object-test:
      resolve:
        with:
          - provider: static
            inputs:
              value:
                key1: value1
                key2: 42
```

Run with the builtin first, then with the plugin:

~~~bash
# Builtin output (baseline)
scafctl run resolver -f parity-test.yaml -o json > builtin-output.json

# Plugin output (via --plugin-dir)
scafctl run resolver -f parity-test.yaml -o json \
  --plugin-dir /tmp/scafctl-plugin-static/ > plugin-output.json

# Compare
diff builtin-output.json plugin-output.json
~~~

The outputs should be identical.

## Step 6: Create GitHub Repo

~~~bash
cd /tmp/scafctl-plugin-static
git init
git add .
git commit -s -S -m "feat: initial plugin scaffold"
gh repo create oakwood-commons/scafctl-plugin-static --public --source=. --push
~~~

Configure repository settings:

~~~bash
# Branch ruleset (main): require status checks, signed commits, linear history
gh api repos/oakwood-commons/scafctl-plugin-static/rulesets \
  --method POST --input - <<'JSON'
{
  "name": "main branch protection",
  "target": "branch",
  "enforcement": "active",
  "conditions": {"ref_name": {"include": ["refs/heads/main"], "exclude": []}},
  "rules": [
    {"type": "pull_request", "parameters": {"required_approving_review_count": 1}},
    {"type": "required_linear_history"},
    {"type": "required_signatures"}
  ]
}
JSON

# Tag ruleset (v*): prevent deletion and force push
gh api repos/oakwood-commons/scafctl-plugin-static/rulesets \
  --method POST --input - <<'JSON'
{
  "name": "version tag protection",
  "target": "tag",
  "enforcement": "active",
  "conditions": {"ref_name": {"include": ["refs/tags/v*"], "exclude": []}},
  "rules": [
    {"type": "deletion"},
    {"type": "non_fast_forward"}
  ]
}
JSON

# Enable vulnerability alerts + automated security fixes
gh api repos/oakwood-commons/scafctl-plugin-static/vulnerability-alerts --method PUT
gh api repos/oakwood-commons/scafctl-plugin-static/automated-security-fixes --method PUT
~~~

## Step 7: Multi-Platform Build

Build for all target platforms using GoReleaser:

~~~bash
goreleaser --snapshot --clean
~~~

Or manually:

~~~bash
GOOS=linux   GOARCH=amd64 go build -o dist/linux-amd64   ./cmd/scafctl-plugin-static/
GOOS=linux   GOARCH=arm64 go build -o dist/linux-arm64   ./cmd/scafctl-plugin-static/
GOOS=darwin  GOARCH=amd64 go build -o dist/darwin-amd64  ./cmd/scafctl-plugin-static/
GOOS=darwin  GOARCH=arm64 go build -o dist/darwin-arm64  ./cmd/scafctl-plugin-static/
GOOS=windows GOARCH=amd64 go build -o dist/windows-amd64.exe ./cmd/scafctl-plugin-static/
~~~

## Step 8: Publish to OCI Catalog

Store all platform binaries in a single OCI image index:

~~~bash
scafctl build plugin \
  --name static \
  --kind provider \
  --version 1.0.0 \
  --platform linux/amd64=dist/linux-amd64 \
  --platform linux/arm64=dist/linux-arm64 \
  --platform darwin/amd64=dist/darwin-amd64 \
  --platform darwin/arm64=dist/darwin-arm64 \
  --platform windows/amd64=dist/windows-amd64.exe
~~~

This stores the artifact in the local catalog. Verify:

~~~bash
scafctl catalog list --kind provider
~~~

To push to a remote OCI registry (e.g., GHCR), use `scafctl catalog push` or
let CI/CD handle it on a tagged release.

## Step 9: Test Auto-Fetch

Add `bundle.plugins` to a solution and test the full fetch pipeline:

```yaml
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: auto-fetch-test
  version: 1.0.0
spec:
  bundle:
    plugins:
      - name: static
        kind: provider
        version: "^1.0.0"
  resolvers:
    greeting:
      resolve:
        with:
          - provider: static
            inputs:
              value: "hello from auto-fetched plugin"
```

### Generate a Lock File

The lock file pins exact versions and binary content digests for reproducible
builds and supply chain security:

~~~bash
scafctl build solution -f auto-fetch-test.yaml --ignore-preflight
~~~

This creates a `solution.lock` file next to the solution with pinned digests:

```yaml
version: 1
plugins:
  - name: static
    kind: provider
    version: 1.0.0
    digest: sha256:aa0e3fa...  # Binary content digest
    resolvedFrom: local
```

> [!IMPORTANT]
> The digest is the SHA-256 hash of the **binary content**, not the OCI
> manifest or index. This ensures runtime verification matches: scafctl
> computes `sha256(downloaded_binary)` and compares it to the lock file digest.

### Run the Solution

~~~bash
# Clear cache to force a fresh fetch
rm -rf "$(scafctl paths cache)/plugins/static"

# Run -- auto-fetches from catalog, verifies digest, caches, and executes
scafctl run resolver -f auto-fetch-test.yaml -o json
~~~

Expected output:

```json
{
  "greeting": "hello from auto-fetched plugin"
}
```

## Step 10: Remove from scafctl Core

After the plugin is validated and published:

1. **Remove the builtin registration** in `pkg/provider/builtin/builtin.go`:
   - Remove the import (e.g., `staticprovider`)
   - Remove `staticprovider.New()` from the `providers` slice

2. **Remove the builtin source** -- delete the provider package
   (e.g., `pkg/provider/builtin/staticprovider/`)

3. **Update example solutions** -- add `bundle.plugins` entries to any
   solutions that use the provider

4. **Update integration tests** -- ensure tests that use the provider either
   include `bundle.plugins` or use `--plugin-dir`

5. **Run full e2e**:

~~~bash
task test:e2e 2>&1 | tee /tmp/e2e-results.txt | tail -5
~~~

## Lessons Learned from the Pilot

The `static` provider extraction uncovered several issues. Watch for these
when extracting other providers:

### Scaffold Fixes Needed

| Issue | Fix |
|-------|-----|
| `go.mod.tpl` had wrong jsonschema module path | Use `github.com/google/jsonschema-go v0.4.2` |
| `go.mod.tpl` had outdated SDK version | Pin to `v0.1.1` |
| `main.go.tpl` indentation broken by Go template trim markers | Remove right-trim from `else` tag |
| `provider.go.tpl` used `scafctl.io/v1` for APIVersion | Use `v1` to match builtin convention |
| `ci.yaml.tpl` used golangci-lint v1 with v2 config | Upgrade to action v7, lint v2.11.4 |

### Capability Considerations

- **Action capability output schema mismatch**: The builtin `static` provider
  declared `CapabilityAction` with an output schema of
  `{success: bool, value: any}`, but `Execute()` returned raw `value` as
  `Data`. Rather than fixing this, the `action` capability was removed from the
  plugin. Consider whether each capability is meaningful before porting.

### Plugin Shadowing

- Builtins always take priority over plugins with the same name. During
  extraction testing, you must either:
  - Temporarily comment out the builtin registration, OR
  - Use `--plugin-dir` which bypasses catalog auto-fetch
  - The final removal in Step 10 resolves this permanently

### Digest Verification

- Lock files store the **binary content digest** (`sha256` of the actual
  plugin binary), not the OCI manifest or index digest. This is critical
  for supply chain security -- the runtime verification computes the same
  hash over the downloaded bytes.
- Always test with a fresh lock file after rebuilding the plugin binary.

## What's Next

After completing the extraction:

- **Tag a release** on the plugin repo (e.g., `v1.0.0`) to trigger CI/CD
  publishing to GHCR
- **Enable branch rulesets** that were disabled during development
- **Update the scafctl changelog** noting the provider extraction
- **Repeat** for the next provider in the extraction plan
