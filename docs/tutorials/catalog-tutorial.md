# Working with the Local Catalog

This tutorial covers how to use scafctl's local catalog to store, manage, and run solutions without needing file paths.

## What is the Catalog?

The catalog is a local OCI-based artifact store that lets you:

- **Build** solutions into versioned artifacts
- **Run** solutions by name instead of file path
- **Manage** multiple versions of the same solution
- **Share** solutions (remote registries coming in Phase 2)

Think of it like a local package manager for your scafctl solutions.

## Where is the Catalog Stored?

The catalog uses XDG Base Directory paths:

| Platform | Default Location |
|----------|------------------|
| macOS | `~/Library/Application Support/scafctl/catalog` |
| Linux | `~/.local/share/scafctl/catalog` |
| Windows | `%LOCALAPPDATA%\scafctl\catalog` |

## Quick Start

### 1. Build a Solution

Take any solution file and build it into the catalog:

```bash
# Build with explicit version
scafctl build solution -f my-solution.yaml --version 1.0.0

# Build using version from metadata.version field
scafctl build solution -f my-solution.yaml
```

Output:
```
 ✅ Built my-solution@1.0.0
 💡   Digest: sha256:abc123...
 💡   Catalog: ~/.local/share/scafctl/catalog
```

### 2. Run from Catalog

Now run it by name - no file path needed:

```bash
# Run latest version
scafctl run solution my-solution

# Run specific version
scafctl run solution my-solution@1.0.0

# Still works with parameters
scafctl run solution my-solution -r env=prod
```

### 3. List Catalog Contents

See what's in your catalog:

```bash
scafctl catalog list
```

Output:
```
SOLUTION              VERSION   CREATED                SIZE
my-solution           1.0.0     2026-02-06T10:30:00Z   1.2 KB
my-solution           1.1.0     2026-02-06T14:00:00Z   1.3 KB
deploy-app            2.0.0     2026-02-05T09:15:00Z   2.4 KB
```

```bash
# JSON output for scripting
scafctl catalog list -o json

# Filter by name
scafctl catalog list --name my-solution
```

## Managing Versions

### Building Multiple Versions

```bash
# Build v1.0.0
scafctl build solution -f solution-v1.yaml --version 1.0.0

# Build v1.1.0 (different file, same solution name)
scafctl build solution -f solution-v1.1.yaml --version 1.1.0

# Build v2.0.0-beta
scafctl build solution -f solution-v2.yaml --version 2.0.0-beta.1
```

### Version Resolution

When you run without specifying a version, scafctl picks the **highest semantic version**:

```bash
# With versions 1.0.0, 1.1.0, 2.0.0-beta.1 in catalog:
scafctl run solution my-solution        # Runs 2.0.0-beta.1 (highest)
scafctl run solution my-solution@1.1.0  # Runs exactly 1.1.0
```

### Overwriting Versions

By default, building an existing version fails:

```bash
scafctl build solution -f updated.yaml --version 1.0.0
# Error: artifact my-solution@1.0.0 already exists in catalog "local"
```

Use `--force` to overwrite:

```bash
scafctl build solution -f updated.yaml --version 1.0.0 --force
```

## Inspecting Solutions

View detailed metadata about a cataloged solution:

```bash
scafctl catalog inspect my-solution
```

Output:
```
Solution: my-solution@1.1.0

METADATA
  Name:         my-solution
  Version:      1.1.0
  Display Name: My Example Solution
  Description:  Deploys infrastructure to cloud

ARTIFACT
  Digest:       sha256:def456...
  Created:      2026-02-06T14:00:00Z
  Size:         1.3 KB

STRUCTURE
  Resolvers:    5
  Actions:      3
  Finally:      1
```

```bash
# Inspect specific version
scafctl catalog inspect my-solution@1.0.0

# JSON output
scafctl catalog inspect my-solution -o json
```

## Cleanup

### Deleting Solutions

Remove a specific version:

```bash
scafctl catalog delete my-solution@1.0.0
```

Output:
```
 ✅ Deleted my-solution@1.0.0
 💡   Digest: sha256:abc123...
```

**Note:** You must specify the version. This prevents accidentally deleting all versions.

### Pruning Orphaned Data

After deleting solutions, blob data may remain. Clean it up with prune:

```bash
scafctl catalog prune
```

Output:
```
 ✅ Pruned catalog successfully!
 💡   Manifests Removed: 2
 💡   Blobs Removed: 5
 💡   Space Reclaimed: 4.2 KB
```

## Name Resolution Priority

When you run a solution, scafctl checks sources in this order:

1. **Catalog** (if the name is a "bare name" - no path separators or file extensions)
2. **File system** (if it looks like a path)
3. **URL** (if it starts with `http://` or `https://`)

Examples:

```bash
# Bare name → checks catalog first
scafctl run solution my-solution

# Path → goes directly to file system
scafctl run solution ./my-solution.yaml
scafctl run solution examples/deploy.yaml

# URL → fetches from remote
scafctl run solution https://example.com/solution.yaml
```

## Complete Workflow Example

Here's a typical development workflow:

```bash
# 1. Develop your solution locally
scafctl run solution -f ./deploy.yaml --dry-run

# 2. Test it
scafctl run solution -f ./deploy.yaml -r env=dev

# 3. Build to catalog with version
scafctl build solution -f ./deploy.yaml --version 1.0.0

# 4. Now run from anywhere by name
scafctl run solution deploy -r env=staging

# 5. Make improvements, build new version
scafctl build solution -f ./deploy.yaml --version 1.1.0

# 6. Run latest version
scafctl run solution deploy -r env=prod

# 7. Clean up old versions
scafctl catalog delete deploy@1.0.0
scafctl catalog prune
```

## Command Reference

| Command | Description |
|---------|-------------|
| `scafctl build solution -f FILE` | Build solution to catalog |
| `scafctl catalog list` | List all solutions |
| `scafctl catalog inspect NAME[@VERSION]` | Show solution details |
| `scafctl catalog delete NAME@VERSION` | Remove a solution version |
| `scafctl catalog prune` | Clean up orphaned data |
| `scafctl run solution NAME[@VERSION]` | Run from catalog |

## Next Steps

- [Getting Started](getting-started.md) - Basic scafctl usage
- [Actions Tutorial](actions-tutorial.md) - Building workflows
- [Resolver Tutorial](resolver-tutorial.md) - Data resolution patterns
