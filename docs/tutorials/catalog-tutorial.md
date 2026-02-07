# Working with the Local Catalog

This tutorial covers how to use scafctl's local catalog to store, manage, and run solutions without needing file paths.

## What is the Catalog?

The catalog is a local OCI-based artifact store that lets you:

- **Build** solutions into versioned artifacts
- **Run** solutions by name instead of file path
- **Manage** multiple versions of the same solution
- **Share** solutions via remote OCI registries (ghcr.io, Docker Hub, ACR, etc.)
- **Export/Import** solutions for air-gapped environments

Think of it like a package manager for your scafctl solutions.

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
scafctl build solution my-solution.yaml --version 1.0.0

# Build using version from metadata.version field
scafctl build solution my-solution.yaml
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
scafctl build solution solution-v1.yaml --version 1.0.0

# Build v1.1.0 (different file, same solution name)
scafctl build solution solution-v1.1.yaml --version 1.1.0

# Build v2.0.0-beta
scafctl build solution solution-v2.yaml --version 2.0.0-beta.1
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
scafctl build solution updated.yaml --version 1.0.0
# Error: artifact my-solution@1.0.0 already exists in catalog "local"
```

Use `--force` to overwrite:

```bash
scafctl build solution updated.yaml --version 1.0.0 --force
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

## Exporting and Importing (Air-Gapped Environments)

The `save` and `load` commands let you transfer catalog artifacts between machines without network access.

### Saving an Artifact

Export a solution to a tar archive:

```bash
# Save latest version
scafctl catalog save my-solution -o my-solution.tar

# Save specific version
scafctl catalog save my-solution@1.0.0 -o my-solution-v1.tar
```

Output:
```
 ✅ Saved my-solution@1.0.0
 💡   Output: my-solution.tar
 💡   Size: 2.4 KB
 💡   Digest: sha256:abc123...
```

The archive uses the standard **OCI Image Layout** format, making it compatible with other OCI tools.

### Loading an Artifact

Import an artifact from a tar archive:

```bash
scafctl catalog load --input my-solution.tar
```

Output:
```
ARTIFACT        VERSION   DIGEST
my-solution     1.0.0     sha256:abc123...
```

If the artifact already exists in your catalog, loading fails:

```bash
scafctl catalog load --input my-solution.tar
# Error: artifact "my-solution@1.0.0" already exists in catalog
```

Use `--force` to overwrite:

```bash
scafctl catalog load --input my-solution.tar --force
```

### Air-Gapped Transfer Workflow

Here's a complete workflow for transferring solutions to a machine without internet:

```bash
# On the connected machine:
# 1. Build the solution
scafctl build solution deploy.yaml --version 1.0.0

# 2. Export to tar
scafctl catalog save deploy@1.0.0 -o deploy-v1.0.0.tar

# 3. Copy to USB drive or other media
cp deploy-v1.0.0.tar /Volumes/USB/

# --- Transfer to air-gapped machine ---

# On the air-gapped machine:
# 4. Load from tar
scafctl catalog load --input /Volumes/USB/deploy-v1.0.0.tar

# 5. Run the solution
scafctl run solution deploy -r target=server1
```

### Archive Format

The exported tar file contains an OCI Image Layout:

```
my-solution.tar
├── oci-layout           # OCI layout version file
├── index.json           # Image index with manifest reference
└── blobs/
    └── sha256/
        ├── <manifest>   # Artifact manifest
        ├── <config>     # Configuration blob
        └── <content>    # Solution YAML content
```

This format is:
- **Self-contained** - includes all layers and metadata
- **Verifiable** - content-addressable by digest
- **Standard** - compatible with OCI registry tools

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
scafctl build solution ./deploy.yaml --version 1.0.0

# 4. Now run from anywhere by name
scafctl run solution deploy -r env=staging

# 5. Make improvements, build new version
scafctl build solution ./deploy.yaml --version 1.1.0

# 6. Run latest version
scafctl run solution deploy -r env=prod

# 7. Clean up old versions
scafctl catalog delete deploy@1.0.0
scafctl catalog prune
```

## Command Reference

| Command | Description |
|---------|-------------|
| `scafctl build solution FILE` | Build solution to catalog |
| `scafctl catalog list` | List all solutions |
| `scafctl catalog inspect NAME[@VERSION]` | Show solution details |
| `scafctl catalog delete NAME@VERSION` | Remove a solution version |
| `scafctl catalog prune` | Clean up orphaned data |
| `scafctl catalog save NAME[@VERSION] -o FILE` | Export to tar archive |
| `scafctl catalog load --input FILE` | Import from tar archive |
| `scafctl catalog push NAME[@VERSION]` | Push to remote registry |
| `scafctl catalog pull REGISTRY/REPO/KIND/NAME[@VERSION]` | Pull from remote registry |
| `scafctl run solution NAME[@VERSION]` | Run from catalog |

## Remote Registry Support

scafctl supports pushing and pulling artifacts to/from OCI-compliant container registries like GitHub Container Registry (ghcr.io), Docker Hub, Azure Container Registry, and others.

### Setting Up Authentication

scafctl reads container credentials from the same locations as Docker and Podman:

| Priority | Location | Description |
|----------|----------|-------------|
| 1 | `$DOCKER_CONFIG/config.json` | Docker config env var |
| 2 | `~/.docker/config.json` | Docker default |
| 3 | `$XDG_RUNTIME_DIR/containers/auth.json` | Podman rootless |
| 4 | `~/.config/containers/auth.json` | Podman default |

You can also use environment variables:
- `SCAFCTL_REGISTRY_USERNAME`
- `SCAFCTL_REGISTRY_PASSWORD`

### Authenticating to GitHub Container Registry (ghcr.io)

GitHub Container Registry requires a Personal Access Token (PAT) with the `write:packages` scope.

#### Option 1: Using GitHub CLI (Recommended)

If you have the [GitHub CLI](https://cli.github.com/) installed, this is the easiest method:

```bash
# Login with the required scopes (interactive)
gh auth login -s write:packages -s read:packages -s delete:packages

# Then login to the container registry using the gh token
gh auth token | docker login ghcr.io -u YOUR_GITHUB_USERNAME --password-stdin

# Or with Podman
gh auth token | podman login ghcr.io -u YOUR_GITHUB_USERNAME --password-stdin
```

#### Option 2: Create a Personal Access Token Manually

1. Go to [GitHub Settings → Developer settings → Personal access tokens → Tokens (classic)](https://github.com/settings/tokens)
2. Click **Generate new token (classic)**
3. Give it a descriptive name (e.g., "scafctl registry access")
4. Select scopes:
   - `write:packages` - Upload packages
   - `read:packages` - Download packages
   - `delete:packages` - (Optional) Delete packages
5. Click **Generate token** and copy the token

Then authenticate:

**Using Docker:**

```bash
echo "YOUR_GITHUB_TOKEN" | docker login ghcr.io -u YOUR_GITHUB_USERNAME --password-stdin
```

**Using Podman:**

```bash
echo "YOUR_GITHUB_TOKEN" | podman login ghcr.io -u YOUR_GITHUB_USERNAME --password-stdin
```

This saves credentials to your Docker/Podman config file, which scafctl will automatically use.

#### Step 3: Verify Authentication

Check that you can access the registry:

```bash
# Docker
docker pull ghcr.io/YOUR_ORG/ANY_PUBLIC_IMAGE:latest

# Or test with scafctl (will fail if no artifacts exist, but auth should work)
scafctl catalog push my-solution@1.0.0 --catalog ghcr.io/YOUR_ORG --log-level -1
```

### Pushing to a Remote Registry

Push an artifact from your local catalog to a remote registry:

```bash
# Push to GitHub Container Registry
scafctl catalog push my-solution@1.0.0 --catalog ghcr.io/myorg/scafctl

# Push with a different name
scafctl catalog push my-solution@1.0.0 --as production-solution --catalog ghcr.io/myorg/scafctl

# Force overwrite existing artifact
scafctl catalog push my-solution@1.0.0 --force --catalog ghcr.io/myorg/scafctl
```

Output:
```
 💡 Pushing my-solution@1.0.0 to ghcr.io/myorg/scafctl...
 ✅ Pushed my-solution@1.0.0 (1.2 KB)
```

**Repository Path Structure:**

The artifact is pushed to: `ghcr.io/myorg/scafctl/solutions/my-solution:1.0.0`

The full path is: `<registry>/<repository>/solutions/<name>:<version>`

### Pulling from a Remote Registry

Pull an artifact from a remote registry to your local catalog:

```bash
# Pull a solution
scafctl catalog pull ghcr.io/myorg/scafctl/solutions/my-solution@1.0.0

# Pull without specifying version (gets latest)
scafctl catalog pull ghcr.io/myorg/scafctl/solutions/my-solution

# Pull with a different local name
scafctl catalog pull ghcr.io/myorg/scafctl/solutions/my-solution@1.0.0 --as local-solution

# Force overwrite if already exists locally
scafctl catalog pull ghcr.io/myorg/scafctl/solutions/my-solution@1.0.0 --force
```

Output:
```
 💡 Pulling ghcr.io/myorg/scafctl/solutions/my-solution@1.0.0...
 ✅ Pulled my-solution@1.0.0 (1.2 KB)
```

### Deleting from a Remote Registry

Delete an artifact from a remote registry using the full reference:

```bash
# Delete from remote registry
scafctl catalog delete ghcr.io/myorg/scafctl/solutions/my-solution@1.0.0
```

**Note:** Not all registries support deletion via the OCI API. GitHub Container Registry (ghcr.io) requires you to delete packages through the GitHub web interface at:
`https://github.com/orgs/YOUR_ORG/packages`

Registries that support OCI DELETE:
- Docker Hub ✅
- Azure Container Registry ✅
- Harbor ✅
- Amazon ECR ✅

Registries that require web UI deletion:
- GitHub Container Registry (ghcr.io) ❌

### Complete Remote Workflow Example

Here's a typical workflow for sharing solutions via a remote registry:

```bash
# === Developer A (publishing) ===

# 1. Build the solution locally
scafctl build solution deploy.yaml --version 1.0.0

# 2. Push to remote registry
scafctl catalog push deploy@1.0.0 --catalog ghcr.io/myorg/scafctl

# === Developer B (consuming) ===

# 3. Pull from remote registry
scafctl catalog pull ghcr.io/myorg/scafctl/solutions/deploy@1.0.0

# 4. Run the solution
scafctl run solution deploy -r target=production
```

### Troubleshooting

#### Authentication Errors (403 Forbidden)

If you get a 403 error:

```
❌ failed to push artifact: ... response status code 403: denied
```

**Check:**
1. Your token has `write:packages` scope for pushing
2. You're logged in: `docker login ghcr.io` or `podman login ghcr.io`
3. The org/repo exists and you have access
4. Use `--log-level -1` to see which auth config file is being used

```bash
scafctl catalog push my-solution@1.0.0 --catalog ghcr.io/myorg --log-level -1
```

#### Config File Not Found

If scafctl isn't finding your credentials, check where they're stored:

```bash
# Docker
cat ~/.docker/config.json

# Podman
cat ~/.config/containers/auth.json
```

#### Insecure Registries (HTTP)

For local testing with registries that don't use HTTPS:

```bash
scafctl catalog push my-solution@1.0.0 --catalog localhost:5000 --insecure
scafctl catalog pull localhost:5000/solutions/my-solution@1.0.0 --insecure
```

### Supported Registries

scafctl works with any OCI-compliant registry:

| Registry | URL Format |
|----------|------------|
| GitHub Container Registry | `ghcr.io/OWNER` |
| Docker Hub | `docker.io/NAMESPACE` |
| Azure Container Registry | `REGISTRY.azurecr.io` |
| Amazon ECR | `ACCOUNT.dkr.ecr.REGION.amazonaws.com` |
| Google Artifact Registry | `REGION-docker.pkg.dev/PROJECT` |
| Harbor | `harbor.example.com/PROJECT` |
| Local Registry | `localhost:5000` |

## Next Steps

- [Getting Started](getting-started.md) - Basic scafctl usage
- [Actions Tutorial](actions-tutorial.md) - Building workflows
- [Resolver Tutorial](resolver-tutorial.md) - Data resolution patterns

