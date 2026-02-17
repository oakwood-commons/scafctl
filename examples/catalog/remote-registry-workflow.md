# Remote Registry Workflow Example

This example demonstrates how to share solutions between teams using a remote OCI registry.

## Prerequisites

1. **scafctl installed** - See [Getting Started](../../docs/tutorials/getting-started.md)
2. **Container registry access** - Docker Hub, GitHub Container Registry, or other OCI registry
3. **Authenticated** - `docker login` or `podman login` to your registry

## Step 1: Authenticate to GitHub Container Registry

```bash
# Create a Personal Access Token at:
# https://github.com/settings/tokens/new?scopes=write:packages,read:packages

# Login with Docker
echo "YOUR_TOKEN" | docker login ghcr.io -u YOUR_USERNAME --password-stdin

# Or with Podman
echo "YOUR_TOKEN" | podman login ghcr.io -u YOUR_USERNAME --password-stdin
```

## Step 2: Build and Push a Solution

```bash
# Build the example solution
scafctl build solution examples/resolver-demo.yaml --version 1.0.0

# Verify it's in the local catalog
scafctl catalog list

# Push to remote registry
scafctl catalog push resolver-demo@1.0.0 --catalog ghcr.io/YOUR_ORG/scafctl
```

## Step 3: Pull and Run on Another Machine

```bash
# On a different machine (or fresh catalog)

# Pull from remote registry
scafctl catalog pull ghcr.io/YOUR_ORG/scafctl/solutions/resolver-demo@1.0.0

# Verify it's in local catalog
scafctl catalog list

# Run the resolver-only solution
scafctl run resolver resolver-demo
```

## CI/CD Integration Example

### GitHub Actions Workflow

```yaml
name: Publish Solution

on:
  push:
    tags:
      - 'v*'

jobs:
  publish:
    runs-on: ubuntu-latest
    permissions:
      packages: write
      contents: read

    steps:
      - uses: actions/checkout@v4

      - name: Install scafctl
        run: |
          curl -sL https://github.com/oakwood-commons/scafctl/releases/latest/download/scafctl_linux_amd64.tar.gz | tar xz
          sudo mv scafctl /usr/local/bin/

      - name: Login to GitHub Container Registry
        run: echo "${{ secrets.GITHUB_TOKEN }}" | docker login ghcr.io -u ${{ github.actor }} --password-stdin

      - name: Build and Push
        run: |
          VERSION=${GITHUB_REF#refs/tags/v}
          scafctl build solution solution.yaml --version $VERSION
          scafctl catalog push my-solution@$VERSION --catalog ghcr.io/${{ github.repository_owner }}/scafctl
```

### Consuming in Another Workflow

```yaml
name: Deploy

on:
  workflow_dispatch:
    inputs:
      version:
        description: 'Solution version'
        required: true
        default: '1.0.0'

jobs:
  deploy:
    runs-on: ubuntu-latest
    permissions:
      packages: read

    steps:
      - name: Install scafctl
        run: |
          curl -sL https://github.com/oakwood-commons/scafctl/releases/latest/download/scafctl_linux_amd64.tar.gz | tar xz
          sudo mv scafctl /usr/local/bin/

      - name: Login to GitHub Container Registry
        run: echo "${{ secrets.GITHUB_TOKEN }}" | docker login ghcr.io -u ${{ github.actor }} --password-stdin

      - name: Pull and Run Solution
        run: |
          scafctl catalog pull ghcr.io/${{ github.repository_owner }}/scafctl/solutions/my-solution@${{ inputs.version }}
          scafctl run solution my-solution -r environment=production
```

## Registry URL Examples

| Registry | Push Command |
|----------|--------------|
| GitHub | `scafctl catalog push my-solution@1.0.0 --catalog ghcr.io/myorg/scafctl` |
| Docker Hub | `scafctl catalog push my-solution@1.0.0 --catalog docker.io/myorg/scafctl` |
| Azure ACR | `scafctl catalog push my-solution@1.0.0 --catalog myregistry.azurecr.io/scafctl` |
| Local | `scafctl catalog push my-solution@1.0.0 --catalog localhost:5000 --insecure` |

## Troubleshooting

### View Debug Logs

```bash
scafctl catalog push my-solution@1.0.0 --catalog ghcr.io/myorg --log-level -1
```

This shows which auth config file is being used and credential resolution details.

### Check Auth Config

```bash
# Docker
cat ~/.docker/config.json | jq '.auths'

# Podman
cat ~/.config/containers/auth.json | jq '.auths'
```

### Test Registry Access

```bash
# Test that you can push images
docker push ghcr.io/YOUR_ORG/test:latest

# If this works but scafctl doesn't, check log-level -1 output
```
