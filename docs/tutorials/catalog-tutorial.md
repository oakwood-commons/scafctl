---
title: "Catalog Tutorial"
weight: 60
---

# Catalog Tutorial

This tutorial walks you through using scafctl's local catalog to build, version, inspect, export, and share solutions. You'll start by building your first solution into the catalog and progressively work through versioning, cleanup, air-gapped transfers, remote registries, tagging, and advanced bundling with file dependencies.

## Prerequisites

- scafctl installed and available in your PATH
- Basic familiarity with YAML syntax
- Completion of the [Resolver Tutorial](resolver-tutorial.md)

## Table of Contents

1. [Building Your First Solution](#building-your-first-solution)
2. [Running from the Catalog](#running-from-the-catalog)
3. [Listing and Inspecting](#listing-and-inspecting)
4. [Managing Multiple Versions](#managing-multiple-versions)
5. [Deleting and Pruning](#deleting-and-pruning)
6. [Exporting and Importing](#exporting-and-importing)
7. [Tagging Artifacts](#tagging-artifacts)
8. [Remote Registries](#remote-registries)
9. [Bundling File Dependencies](#bundling-file-dependencies)
10. [Verifying and Extracting Bundles](#verifying-and-extracting-bundles)
11. [Comparing Bundle Versions](#comparing-bundle-versions)

---

## Building Your First Solution

Let's build a simple solution and store it in the local catalog so you can run it by name from anywhere.

### Step 1: Create the Solution File

Create a file called `greeting.yaml`:

```yaml
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: greeting
  version: 1.0.0
  description: A simple greeting solution
spec:
  resolvers:
    name:
      type: string
      resolve:
        with:
          - provider: parameter
            inputs:
              key: name
          - provider: static
            inputs:
              value: World
    message:
      type: string
      dependsOn:
        - name
      resolve:
        with:
          - provider: static
            inputs:
              value:
                expr: "'Hello, ' + _.name + '!'"
```

This solution accepts a `name` parameter (defaulting to "World") and produces a greeting message.

### Step 2: Build It into the Catalog

{{< tabs "catalog-tutorial-cmd-1" >}}
{{% tab "Bash" %}}
```bash
scafctl build solution greeting.yaml
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl build solution greeting.yaml
```
{{% /tab %}}
{{< /tabs >}}

Expected output:

```
 ✅ Built greeting@1.0.0
 💡   Digest: sha256:abc123...
 💡   Catalog: ~/.local/share/scafctl/catalog
```

The solution is now stored in your local catalog. The version (`1.0.0`) was read from `metadata.version` in the YAML file.

### Step 3: Override the Version

You can also specify the version on the command line, which overrides `metadata.version`:

{{< tabs "catalog-tutorial-cmd-2" >}}
{{% tab "Bash" %}}
```bash
scafctl build solution greeting.yaml --version 1.0.1
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl build solution greeting.yaml --version 1.0.1
```
{{% /tab %}}
{{< /tabs >}}

Expected output:

```
 ✅ Built greeting@1.0.1
 💡   Digest: sha256:def456...
 💡   Catalog: ~/.local/share/scafctl/catalog
```

### What You Learned

- `scafctl build solution FILE` packages a solution YAML into the local OCI catalog
- The name and version come from `metadata.name` and `metadata.version` by default
- Use `--version` to override the version at build time
- Use `--name` to override the name at build time

---

## Running from the Catalog

Once a solution is in the catalog, you can run it by name instead of providing a file path.

### Step 1: Run by Name

{{< tabs "catalog-tutorial-cmd-3" >}}
{{% tab "Bash" %}}
```bash
scafctl run resolver -f greeting -o yaml --hide-execution
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl run resolver -f greeting -o yaml --hide-execution
```
{{% /tab %}}
{{< /tabs >}}

Expected output:

```yaml
message: Hello, World!
name: World
```

No file path needed — scafctl looked up `greeting` in the catalog and found the highest version.

### Step 2: Pass a Parameter

{{< tabs "catalog-tutorial-cmd-4" >}}
{{% tab "Bash" %}}
```bash
scafctl run resolver -f greeting -o yaml --hide-execution -r name=Alice
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl run resolver -f greeting -o yaml --hide-execution -r name=Alice
```
{{% /tab %}}
{{< /tabs >}}

Expected output:

```yaml
message: Hello, Alice!
name: Alice
```

### Step 3: Run a Specific Version

When you have multiple versions, you can pin to a specific one:

{{< tabs "catalog-tutorial-cmd-5" >}}
{{% tab "Bash" %}}
```bash
scafctl run resolver -f greeting@1.0.0 -o yaml --hide-execution -r name=Bob
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl run resolver -f greeting@1.0.0 -o yaml --hide-execution -r name=Bob
```
{{% /tab %}}
{{< /tabs >}}

Expected output:

```yaml
message: Hello, Bob!
name: Bob
```

### Step 4: Use an Expression to Filter Output

Use `-e` to extract just the value you care about:

{{< tabs "catalog-tutorial-cmd-6" >}}
{{% tab "Bash" %}}
```bash
scafctl run resolver -f greeting -o yaml -e '_.message' -r name=Carol
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl run resolver -f greeting -o yaml -e '_.message' -r name=Carol
```
{{% /tab %}}
{{< /tabs >}}

Expected output:

```yaml
Hello, Carol!
```

### What You Learned

- `scafctl run resolver -f NAME` runs a solution from the catalog by name
- Without a version, it picks the highest semantic version available
- Use `NAME@VERSION` to pin a specific version
- Parameters work the same way as with file-based solutions (`-r key=value`)
- Use `-e` to filter output to specific values

---

## Listing and Inspecting

### Step 1: List Everything in the Catalog

{{< tabs "catalog-tutorial-cmd-7" >}}
{{% tab "Bash" %}}
```bash
scafctl catalog list -o yaml
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl catalog list -o yaml
```
{{% /tab %}}
{{< /tabs >}}

Expected output:

```yaml
- name: greeting
  version: 1.0.0
  kind: solution
  digest: sha256:abc123...
  createdAt: "2026-02-17 10:00:00"
  catalog: local
- name: greeting
  version: 1.0.1
  kind: solution
  digest: sha256:def456...
  createdAt: "2026-02-17 10:01:00"
  catalog: local
```

### Step 2: Filter by Name

{{< tabs "catalog-tutorial-cmd-8" >}}
{{% tab "Bash" %}}
```bash
scafctl catalog list --name greeting -o yaml
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl catalog list --name greeting -o yaml
```
{{% /tab %}}
{{< /tabs >}}

This shows only artifacts with the name `greeting`.

### Step 3: Inspect a Specific Artifact

{{< tabs "catalog-tutorial-cmd-9" >}}
{{% tab "Bash" %}}
```bash
scafctl catalog inspect greeting -o yaml
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl catalog inspect greeting -o yaml
```
{{% /tab %}}
{{< /tabs >}}

Expected output:

```yaml
name: greeting
version: 1.0.1
kind: solution
digest: sha256:def456...
size: 573
createdAt: "2026-02-17 10:01:00"
catalog: local
annotations:
    dev.scafctl.artifact.name: greeting
    dev.scafctl.artifact.type: solution
    org.opencontainers.image.created: "2026-02-17T10:01:00Z"
    org.opencontainers.image.source: greeting.yaml
    org.opencontainers.image.version: 1.0.1
```

Without a version, `inspect` shows the highest version. Pin a version with `greeting@1.0.0`.

### Step 4: Use a CEL Expression to Extract Fields

{{< tabs "catalog-tutorial-cmd-10" >}}
{{% tab "Bash" %}}
```bash
scafctl catalog inspect greeting -o yaml -e '_.annotations'
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl catalog inspect greeting -o yaml -e '_.annotations'
```
{{% /tab %}}
{{< /tabs >}}

Expected output:

```yaml
dev.scafctl.artifact.name: greeting
dev.scafctl.artifact.type: solution
org.opencontainers.image.created: "2026-02-17T10:01:00Z"
org.opencontainers.image.source: greeting.yaml
org.opencontainers.image.version: 1.0.1
```

### What You Learned

- `scafctl catalog list` shows every artifact in the catalog
- `--name` filters by solution name
- `scafctl catalog inspect NAME` shows detailed metadata for a specific artifact
- Without a version, inspect/list show all versions or the highest version respectively
- `-e` with CEL expressions can extract sub-fields from the output

---

## Managing Multiple Versions

### Step 1: Create a v2 of the Solution

Create a file called `greeting-v2.yaml`:

```yaml
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: greeting
  version: 2.0.0
  description: An improved greeting solution with timestamps
spec:
  resolvers:
    name:
      type: string
      resolve:
        with:
          - provider: parameter
            inputs:
              key: name
          - provider: static
            inputs:
              value: World
    timestamp:
      type: string
      resolve:
        with:
          - provider: static
            inputs:
              value:
                expr: "timestamp(now)"
    message:
      type: string
      dependsOn:
        - name
        - timestamp
      resolve:
        with:
          - provider: static
            inputs:
              value:
                expr: "'Hello, ' + _.name + '! The time is ' + _.timestamp"
```

### Step 2: Build v2

{{< tabs "catalog-tutorial-cmd-11" >}}
{{% tab "Bash" %}}
```bash
scafctl build solution greeting-v2.yaml
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl build solution greeting-v2.yaml
```
{{% /tab %}}
{{< /tabs >}}

Expected output:

```
 ✅ Built greeting@2.0.0
 💡   Digest: sha256:789abc...
 💡   Catalog: ~/.local/share/scafctl/catalog
```

### Step 3: Verify Both Versions Exist

{{< tabs "catalog-tutorial-cmd-12" >}}
{{% tab "Bash" %}}
```bash
scafctl catalog list --name greeting -o yaml
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl catalog list --name greeting -o yaml
```
{{% /tab %}}
{{< /tabs >}}

Expected output:

```yaml
- name: greeting
  version: 1.0.0
  kind: solution
  ...
- name: greeting
  version: 1.0.1
  kind: solution
  ...
- name: greeting
  version: 2.0.0
  kind: solution
  ...
```

### Step 4: Run Without a Version

{{< tabs "catalog-tutorial-cmd-13" >}}
{{% tab "Bash" %}}
```bash
scafctl run resolver -f greeting -o yaml --hide-execution -r name=Alice
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl run resolver -f greeting -o yaml --hide-execution -r name=Alice
```
{{% /tab %}}
{{< /tabs >}}

Expected output:

```yaml
message: Hello, Alice! The time is 2026-02-17T10:05:00Z
name: Alice
timestamp: "2026-02-17T10:05:00Z"
```

Without a version, scafctl runs the **highest semantic version** — in this case `2.0.0`.

### Step 5: Pin to the Old Version

{{< tabs "catalog-tutorial-cmd-14" >}}
{{% tab "Bash" %}}
```bash
scafctl run resolver -f greeting@1.0.0 -o yaml --hide-execution -r name=Alice
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl run resolver -f greeting@1.0.0 -o yaml --hide-execution -r name=Alice
```
{{% /tab %}}
{{< /tabs >}}

Expected output:

```yaml
message: Hello, Alice!
name: Alice
```

The v1 solution doesn't have a timestamp — confirming you're running the original version.

### Step 6: Try to Overwrite an Existing Version

{{< tabs "catalog-tutorial-cmd-15" >}}
{{% tab "Bash" %}}
```bash
scafctl build solution greeting-v2.yaml --version 2.0.0
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl build solution greeting-v2.yaml --version 2.0.0
```
{{% /tab %}}
{{< /tabs >}}

Expected output:

```
 ❌ artifact greeting@2.0.0 already exists in catalog "local" (use --force to overwrite)
```

Use `--force` to overwrite:

{{< tabs "catalog-tutorial-cmd-16" >}}
{{% tab "Bash" %}}
```bash
scafctl build solution greeting-v2.yaml --version 2.0.0 --force
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl build solution greeting-v2.yaml --version 2.0.0 --force
```
{{% /tab %}}
{{< /tabs >}}

### What You Learned

- Multiple versions of the same solution coexist in the catalog
- Without a version, `run` picks the highest semantic version
- Use `NAME@VERSION` to pin to a specific version
- Use `--force` to overwrite an existing version

---

## Deleting and Pruning

### Step 1: Delete a Specific Version

{{< tabs "catalog-tutorial-cmd-17" >}}
{{% tab "Bash" %}}
```bash
scafctl catalog delete greeting@1.0.1 --kind solution
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl catalog delete greeting@1.0.1 --kind solution
```
{{% /tab %}}
{{< /tabs >}}

Expected output:

```
 ✅ Deleted greeting@1.0.1
```

You must specify both the name and version. The `--kind solution` flag tells scafctl which artifact kind to delete.

### Step 2: Verify It's Gone

{{< tabs "catalog-tutorial-cmd-18" >}}
{{% tab "Bash" %}}
```bash
scafctl catalog list --name greeting -o yaml
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl catalog list --name greeting -o yaml
```
{{% /tab %}}
{{< /tabs >}}

The `1.0.1` entry should no longer appear.

### Step 3: Prune Orphaned Data

After deleting artifacts, blob data may remain on disk. Clean it up:

{{< tabs "catalog-tutorial-cmd-19" >}}
{{% tab "Bash" %}}
```bash
scafctl catalog prune
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl catalog prune
```
{{% /tab %}}
{{< /tabs >}}

Expected output:

```
 ✅ Pruned catalog
 💡   Removed manifests: 1
 💡   Removed blobs: 2
 💡   Reclaimed: 1.5 KB
```

### Step 4: Delete Multiple Versions

Clean up the remaining test artifacts:

{{< tabs "catalog-tutorial-cmd-20" >}}
{{% tab "Bash" %}}
```bash
scafctl catalog delete greeting@1.0.0 --kind solution
scafctl catalog delete greeting@2.0.0 --kind solution
scafctl catalog prune
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl catalog delete greeting@1.0.0 --kind solution
scafctl catalog delete greeting@2.0.0 --kind solution
scafctl catalog prune
```
{{% /tab %}}
{{< /tabs >}}

### What You Learned

- `scafctl catalog delete NAME@VERSION --kind solution` removes a single version
- You must specify the version — this prevents accidental bulk deletion
- `scafctl catalog prune` removes orphaned blobs and reclaims disk space
- Always prune after deleting to free up storage

---

## Exporting and Importing

The `save` and `load` commands let you transfer catalog artifacts between machines — useful for air-gapped environments where there's no network access to a registry.

### Step 1: Build a Solution to Export

First, rebuild the greeting solution:

{{< tabs "catalog-tutorial-cmd-21" >}}
{{% tab "Bash" %}}
```bash
scafctl build solution greeting.yaml
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl build solution greeting.yaml
```
{{% /tab %}}
{{< /tabs >}}

### Step 2: Export to a Tar Archive

{{< tabs "catalog-tutorial-cmd-22" >}}
{{% tab "Bash" %}}
```bash
scafctl catalog save greeting@1.0.0 -o greeting-v1.tar
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl catalog save greeting@1.0.0 -o greeting-v1.tar
```
{{% /tab %}}
{{< /tabs >}}

Expected output:

```
 ✅ Saved greeting@1.0.0 to greeting-v1.tar (5.5 KB)
```

The archive uses the standard **OCI Image Layout** format.

### Step 3: Delete the Local Copy

Simulate receiving the tar on a different machine by deleting the local version:

{{< tabs "catalog-tutorial-cmd-23" >}}
{{% tab "Bash" %}}
```bash
scafctl catalog delete greeting@1.0.0 --kind solution
scafctl catalog prune
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl catalog delete greeting@1.0.0 --kind solution
scafctl catalog prune
```
{{% /tab %}}
{{< /tabs >}}

### Step 4: Verify It's Gone

{{< tabs "catalog-tutorial-cmd-24" >}}
{{% tab "Bash" %}}
```bash
scafctl catalog list --name greeting -o yaml
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl catalog list --name greeting -o yaml
```
{{% /tab %}}
{{< /tabs >}}

Expected output (empty or no greeting entries).

### Step 5: Import from the Tar Archive

{{< tabs "catalog-tutorial-cmd-25" >}}
{{% tab "Bash" %}}
```bash
scafctl catalog load --input greeting-v1.tar
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl catalog load --input greeting-v1.tar
```
{{% /tab %}}
{{< /tabs >}}

Expected output:

```
 ✅ Loaded artifact from greeting-v1.tar
```

### Step 6: Confirm It Was Loaded

{{< tabs "catalog-tutorial-cmd-26" >}}
{{% tab "Bash" %}}
```bash
scafctl catalog list --name greeting -o yaml
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl catalog list --name greeting -o yaml
```
{{% /tab %}}
{{< /tabs >}}

Expected output:

```yaml
- name: greeting
  version: 1.0.0
  kind: solution
  digest: sha256:abc123...
  createdAt: "2026-02-17 10:00:00"
  catalog: local
```

### Step 7: Try Loading Again (Conflict)

{{< tabs "catalog-tutorial-cmd-27" >}}
{{% tab "Bash" %}}
```bash
scafctl catalog load --input greeting-v1.tar
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl catalog load --input greeting-v1.tar
```
{{% /tab %}}
{{< /tabs >}}

Expected output:

```
 ❌ artifact already exists (use --force to overwrite)
```

Use `--force` to overwrite:

{{< tabs "catalog-tutorial-cmd-28" >}}
{{% tab "Bash" %}}
```bash
scafctl catalog load --input greeting-v1.tar --force
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl catalog load --input greeting-v1.tar --force
```
{{% /tab %}}
{{< /tabs >}}

### Air-Gapped Transfer Workflow

Here's how the full workflow looks in practice:

{{< tabs "catalog-tutorial-cmd-29" >}}
{{% tab "Bash" %}}
```bash
# On the connected machine:
scafctl build solution deploy.yaml --version 1.0.0
scafctl catalog save deploy@1.0.0 -o deploy-v1.tar
cp deploy-v1.tar /Volumes/USB/

# Transfer USB to the air-gapped machine, then:
scafctl catalog load --input /Volumes/USB/deploy-v1.tar
scafctl run resolver -f deploy -o yaml --hide-execution -r env=prod
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# On the connected machine:
scafctl build solution deploy.yaml --version 1.0.0
scafctl catalog save deploy@1.0.0 -o deploy-v1.tar
Copy-Item deploy-v1.tar /Volumes/USB/

# Transfer USB to the air-gapped machine, then:
scafctl catalog load --input /Volumes/USB/deploy-v1.tar
scafctl run resolver -f deploy -o yaml --hide-execution -r env=prod
```
{{% /tab %}}
{{< /tabs >}}

### What You Learned

- `scafctl catalog save NAME@VERSION -o FILE` exports an artifact as an OCI tar archive
- `scafctl catalog load --input FILE` imports an artifact from a tar archive
- Use `--force` to overwrite if the artifact already exists
- This workflow enables air-gapped transfers without any registry access

---

## Tagging Artifacts

Tags let you create freeform aliases for specific versions. Common uses include marking a version as "stable", "latest", or "production".

### Step 1: Tag a Version as Stable

Make sure you have `greeting@1.0.0` in the catalog, then tag it:

{{< tabs "catalog-tutorial-cmd-30" >}}
{{% tab "Bash" %}}
```bash
scafctl catalog tag greeting@1.0.0 stable
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl catalog tag greeting@1.0.0 stable
```
{{% /tab %}}
{{< /tabs >}}

Expected output:

```
 ✅ Tagged greeting@1.0.0 as "stable"
```

### Step 2: View the Tag in the Catalog

{{< tabs "catalog-tutorial-cmd-31" >}}
{{% tab "Bash" %}}
```bash
scafctl catalog list --name greeting -o yaml
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl catalog list --name greeting -o yaml
```
{{% /tab %}}
{{< /tabs >}}

The tag creates an alias that points to the same digest as the original version.

### Step 3: Tag for Different Environments

{{< tabs "catalog-tutorial-cmd-32" >}}
{{% tab "Bash" %}}
```bash
scafctl catalog tag greeting@1.0.0 production
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl catalog tag greeting@1.0.0 production
```
{{% /tab %}}
{{< /tabs >}}

You can create as many tags as needed. Tags are freeform strings — they cannot be valid semver versions (use `scafctl build` for that).

### What You Learned

- `scafctl catalog tag NAME@VERSION ALIAS` creates a named alias pointing to a specific version
- Tags are useful for marking releases as stable, production, etc.
- Tags can also be created in remote registries with `--catalog`

---

## Remote Registries

scafctl supports pushing and pulling artifacts to/from OCI-compliant container registries like GitHub Container Registry (ghcr.io), Docker Hub, Azure Container Registry, and others.

### Step 1: Set Up Authentication

scafctl reads container credentials from the same locations as Docker and Podman. The easiest way to authenticate is with Docker or Podman's login command.

**Using GitHub CLI (recommended):**

```bash
gh auth login -s write:packages -s read:packages -s delete:packages
gh auth token | docker login ghcr.io -u YOUR_GITHUB_USERNAME --password-stdin
```

**Using a Personal Access Token:**

1. Go to [GitHub Settings → Developer settings → Personal access tokens → Tokens (classic)](https://github.com/settings/tokens)
2. Generate a new token with `write:packages` and `read:packages` scopes
3. Log in:

```bash
echo "YOUR_GITHUB_TOKEN" | docker login ghcr.io -u YOUR_GITHUB_USERNAME --password-stdin
```

scafctl checks these credential locations in order:

| Priority | Location |
|----------|----------|
| 1 | `$DOCKER_CONFIG/config.json` |
| 2 | `~/.docker/config.json` |
| 3 | `$XDG_RUNTIME_DIR/containers/auth.json` |
| 4 | `~/.config/containers/auth.json` |

### Step 2: Push a Solution to a Remote Registry

Make sure `greeting@1.0.0` is in your local catalog, then push it:

{{< tabs "catalog-tutorial-cmd-33" >}}
{{% tab "Bash" %}}
```bash
scafctl catalog push greeting@1.0.0 --catalog ghcr.io/myorg/scafctl
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl catalog push greeting@1.0.0 --catalog ghcr.io/myorg/scafctl
```
{{% /tab %}}
{{< /tabs >}}

Expected output:

```
 ✅ Pushed greeting@1.0.0
```

The artifact is stored at: `ghcr.io/myorg/scafctl/solutions/greeting:1.0.0`

The path structure is: `<registry>/<repository>/solutions/<name>:<version>`

### Step 3: Push with a Different Name

{{< tabs "catalog-tutorial-cmd-34" >}}
{{% tab "Bash" %}}
```bash
scafctl catalog push greeting@1.0.0 --as hello-world --catalog ghcr.io/myorg/scafctl
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl catalog push greeting@1.0.0 --as hello-world --catalog ghcr.io/myorg/scafctl
```
{{% /tab %}}
{{< /tabs >}}

This pushes the same artifact under a different name in the remote registry.

### Step 4: Pull from a Remote Registry

On a different machine (or after deleting the local copy):

{{< tabs "catalog-tutorial-cmd-35" >}}
{{% tab "Bash" %}}
```bash
scafctl catalog pull ghcr.io/myorg/scafctl/solutions/greeting@1.0.0
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl catalog pull ghcr.io/myorg/scafctl/solutions/greeting@1.0.0
```
{{% /tab %}}
{{< /tabs >}}

Expected output:

```
 ✅ Pulled greeting@1.0.0
```

The artifact is now in your local catalog. You can run it with:

{{< tabs "catalog-tutorial-cmd-36" >}}
{{% tab "Bash" %}}
```bash
scafctl run resolver -f greeting -o yaml --hide-execution -r name=Alice
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl run resolver -f greeting -o yaml --hide-execution -r name=Alice
```
{{% /tab %}}
{{< /tabs >}}

### Step 5: Pull with a Different Local Name

{{< tabs "catalog-tutorial-cmd-37" >}}
{{% tab "Bash" %}}
```bash
scafctl catalog pull ghcr.io/myorg/scafctl/solutions/greeting@1.0.0 --as my-greeting
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl catalog pull ghcr.io/myorg/scafctl/solutions/greeting@1.0.0 --as my-greeting
```
{{% /tab %}}
{{< /tabs >}}

This stores the artifact locally under the name `my-greeting`.

### Step 6: Delete from a Remote Registry

{{< tabs "catalog-tutorial-cmd-38" >}}
{{% tab "Bash" %}}
```bash
scafctl catalog delete ghcr.io/myorg/scafctl/solutions/greeting@1.0.0
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl catalog delete ghcr.io/myorg/scafctl/solutions/greeting@1.0.0
```
{{% /tab %}}
{{< /tabs >}}

> [!NOTE]
> **Note:** Not all registries support OCI DELETE. GitHub Container Registry (ghcr.io) requires deletion through the web interface at `https://github.com/orgs/YOUR_ORG/packages`. Docker Hub, Azure Container Registry, Harbor, and Amazon ECR support API-based deletion.

### Troubleshooting

**403 Forbidden errors:**

{{< tabs "catalog-tutorial-cmd-39" >}}
{{% tab "Bash" %}}
```bash
# Enable debug logging to see which auth config is being used
scafctl catalog push greeting@1.0.0 --catalog ghcr.io/myorg -d
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# Enable debug logging to see which auth config is being used
scafctl catalog push greeting@1.0.0 --catalog ghcr.io/myorg -d
```
{{% /tab %}}
{{< /tabs >}}

Check that:
1. Your token has `write:packages` scope
2. You're logged in: `docker login ghcr.io`
3. The org/repo exists and you have access

**Insecure registries (HTTP):**

For local testing with registries that don't use HTTPS:

{{< tabs "catalog-tutorial-cmd-40" >}}
{{% tab "Bash" %}}
```bash
scafctl catalog push greeting@1.0.0 --catalog localhost:5000 --insecure
scafctl catalog pull localhost:5000/solutions/greeting@1.0.0 --insecure
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl catalog push greeting@1.0.0 --catalog localhost:5000 --insecure
scafctl catalog pull localhost:5000/solutions/greeting@1.0.0 --insecure
```
{{% /tab %}}
{{< /tabs >}}

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

### What You Learned

- `scafctl catalog push NAME@VERSION --catalog REGISTRY` pushes to a remote registry
- `scafctl catalog pull REGISTRY/solutions/NAME@VERSION` pulls to your local catalog
- `--as` lets you rename artifacts during push or pull
- `--force` overwrites existing artifacts
- `--insecure` allows HTTP connections for local testing
- Authentication uses standard Docker/Podman credential files

---

## Bundling File Dependencies

When a solution references local files (via the `file` provider), those files need to be packaged into the bundle so the solution works when run from the catalog. This tutorial walks you through building a solution with file dependencies.

### Step 1: Create the Project Structure

Create a directory with the following files:

```bash
mkdir -p deploy-app/templates deploy-app/configs
```

Create `deploy-app/configs/dev.yaml`:

```yaml
name: my-app
namespace: dev
replicas: 1
image: my-app:latest
port: 8080
```

Create `deploy-app/configs/prod.yaml`:

```yaml
name: my-app
namespace: production
replicas: 3
image: my-app:1.2.0
port: 8080
```

Create `deploy-app/templates/deployment.yaml`:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: {{ .name }}
  namespace: {{ .namespace }}
spec:
  replicas: {{ .replicas }}
  selector:
    matchLabels:
      app: {{ .name }}
  template:
    metadata:
      labels:
        app: {{ .name }}
    spec:
      containers:
        - name: {{ .name }}
          image: {{ .image }}
          ports:
            - containerPort: {{ .port }}
```

### Step 2: Create the Solution File

Create `deploy-app/solution.yaml`:

```yaml
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: deploy-app
  version: 1.0.0
  description: Renders a Kubernetes deployment for a given environment

bundle:
  include:
    - "configs/**/*.yaml"

spec:
  resolvers:
    environment:
      type: string
      description: "Target environment (dev or prod)"
      resolve:
        with:
          - provider: parameter
            inputs:
              key: environment
          - provider: static
            inputs:
              value: dev

    config:
      type: any
      description: "Environment-specific configuration"
      dependsOn:
        - environment
      resolve:
        with:
          - provider: file
            inputs:
              path:
                expr: "'configs/' + _.environment + '.yaml'"
              format: yaml

    deployment-template:
      type: string
      description: "Kubernetes deployment template"
      resolve:
        with:
          - provider: file
            inputs:
              path: "templates/deployment.yaml"

    rendered-deployment:
      type: string
      description: "Rendered deployment manifest"
      dependsOn:
        - deployment-template
        - config
      resolve:
        with:
          - provider: go-template
            inputs:
              template:
                rslvr: deployment-template
              data:
                rslvr: config
```

Notice the `bundle.include` section — this is needed because `config` uses a **dynamic path** (computed via CEL expression at runtime). scafctl can't statically discover which config files to bundle, so you tell it to include all YAML files under `configs/`.

The `deployment-template` resolver uses a **static path** (`templates/deployment.yaml`), so scafctl discovers it automatically — no `bundle.include` entry needed.

### Step 3: Preview What Gets Bundled

{{< tabs "catalog-tutorial-cmd-41" >}}
{{% tab "Bash" %}}
```bash
scafctl build solution deploy-app/solution.yaml --dry-run
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl build solution deploy-app/solution.yaml --dry-run
```
{{% /tab %}}
{{< /tabs >}}

Expected output:

```
Bundle analysis for deploy-app/solution.yaml:
  Static analysis discovered:
    templates/deployment.yaml
  Explicit includes (bundle.include):
    configs/dev.yaml
    configs/prod.yaml
  ⚠️ Dynamic paths detected (ensure these are covered by bundle.include):
    resolver 'config' (file provider): expr in 'configs/' + _.environment + '.yaml'
  Total: 3 bundled file(s)
💡 Dry run: would build deploy-app@1.0.0
```

The dry-run shows:
- **Static analysis discovered** — files scafctl found by analyzing your resolvers
- **Explicit includes** — files matched by your `bundle.include` patterns
- **Dynamic paths** — warnings about paths that can't be statically resolved

### Step 4: Build the Solution

{{< tabs "catalog-tutorial-cmd-42" >}}
{{% tab "Bash" %}}
```bash
scafctl build solution deploy-app/solution.yaml
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl build solution deploy-app/solution.yaml
```
{{% /tab %}}
{{< /tabs >}}

Expected output:

```
 💡   Bundled 3 file(s) (1.0 KB, deduplicated: 1 layer(s))
 ✅ Built deploy-app@1.0.0
 💡   Digest: sha256:abc123...
 💡   Catalog: ~/.local/share/scafctl/catalog
```

### Step 5: Run from the Catalog with Dev Config

{{< tabs "catalog-tutorial-cmd-43" >}}
{{% tab "Bash" %}}
```bash
scafctl run resolver -f deploy-app -o yaml -e '_.["rendered-deployment"]' -r environment=dev
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl run resolver -f deploy-app -o yaml -e '_.["rendered-deployment"]' -r environment=dev
```
{{% /tab %}}
{{< /tabs >}}

Expected output:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: my-app
  namespace: dev
spec:
  replicas: 1
  selector:
    matchLabels:
      app: my-app
  template:
    metadata:
      labels:
        app: my-app
    spec:
      containers:
        - name: my-app
          image: my-app:latest
          ports:
            - containerPort: 8080
```

### Step 6: Switch to Prod

{{< tabs "catalog-tutorial-cmd-44" >}}
{{% tab "Bash" %}}
```bash
scafctl run resolver -f deploy-app -o yaml -e '_.["rendered-deployment"]' -r environment=prod
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl run resolver -f deploy-app -o yaml -e '_.["rendered-deployment"]' -r environment=prod
```
{{% /tab %}}
{{< /tabs >}}

Expected output:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: my-app
  namespace: production
spec:
  replicas: 3
  selector:
    matchLabels:
      app: my-app
  template:
    metadata:
      labels:
        app: my-app
    spec:
      containers:
        - name: my-app
          image: my-app:1.2.0
          ports:
            - containerPort: 8080
```

The config values (namespace, replicas, image) changed based on the environment file — all loaded from the bundled files inside the catalog artifact.

### Step 7: Add Exclude Patterns

Suppose you add test files that you don't want in the bundle. Update `deploy-app/solution.yaml` to add an exclude pattern:

```yaml
bundle:
  include:
    - "configs/**/*.yaml"
  exclude:
    - "**/*_test.yaml"
```

Now any file ending in `_test.yaml` will be excluded, even if it matches an include pattern.

### What You Learned

- The `file` provider references local files that must be bundled
- Static paths (literal strings) are auto-discovered during build
- Dynamic paths (CEL expressions, Go templates) require explicit `bundle.include` patterns
- `--dry-run` shows exactly what would be bundled, including warnings for dynamic paths
- `bundle.exclude` filters out files that match include patterns (e.g., test files)
- Bundled solutions are self-contained — all file dependencies travel with the artifact

---

## Nested Bundle Support

When a parent solution references sub-solutions via the `solution` provider, scafctl automatically discovers and bundles the sub-solution files recursively. This means nested solutions are fully self-contained — everything a sub-solution needs is included in the parent's bundle.

### Step 1: Create the Project Structure

```bash
mkdir -p nested-demo/sub/templates
```

Create `nested-demo/parent-config.txt`:

```
parent data content
```

Create `nested-demo/sub/child.yaml`:

```yaml
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: nested-child
  version: 1.0.0
  description: Child sub-solution with its own local template

spec:
  resolvers:
    child-template:
      type: string
      resolve:
        with:
          - provider: file
            inputs:
              operation: read
              path: "templates/greeting.tmpl"
    child-greeting:
      type: string
      resolve:
        with:
          - provider: cel
            inputs:
              expression: "'hello from child'"
```

Create `nested-demo/sub/templates/greeting.tmpl`:

```
Hello from the child template!
```

### Step 2: Create the Parent Solution

Create `nested-demo/parent.yaml`:

```yaml
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: nested-demo
  version: 1.0.0
  description: Parent solution referencing a child sub-solution

spec:
  resolvers:
    parent-config:
      resolve:
        with:
          - provider: file
            inputs:
              operation: read
              path: "parent-config.txt"

    child-result:
      type: any
      resolve:
        with:
          - provider: solution
            inputs:
              source: "./sub/child.yaml"
```

### Step 3: Preview the Bundle

{{< tabs "catalog-tutorial-cmd-45" >}}
{{% tab "Bash" %}}
```bash
scafctl build solution nested-demo/parent.yaml --dry-run
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl build solution nested-demo/parent.yaml --dry-run
```
{{% /tab %}}
{{< /tabs >}}

Expected output:

```
Bundle analysis for nested-demo/parent.yaml:
  Static analysis discovered:
    parent-config.txt
    sub/child.yaml
    sub/templates/greeting.tmpl
  Total: 3 bundled file(s)
💡 Dry run: would build nested-demo@1.0.0
```

Notice that scafctl **recursively discovered** the child sub-solution (`sub/child.yaml`) and its file dependency (`sub/templates/greeting.tmpl`). No `bundle.include` is needed — the solution provider reference is detected by static analysis.

### Step 4: Build and Run

{{< tabs "catalog-tutorial-cmd-46" >}}
{{% tab "Bash" %}}
```bash
scafctl build solution nested-demo/parent.yaml
scafctl run resolver -f nested-demo -o json
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl build solution nested-demo/parent.yaml
scafctl run resolver -f nested-demo -o json
```
{{% /tab %}}
{{< /tabs >}}

### How It Works

1. **Static analysis** — `scafctl build` parses the parent solution and finds the `solution` provider reference to `./sub/child.yaml`
2. **Recursive discovery** — It then parses `sub/child.yaml` and discovers its own file dependencies (`templates/greeting.tmpl`)
3. **Path normalization** — All paths are normalized relative to the parent bundle root (`sub/templates/greeting.tmpl` not `templates/greeting.tmpl`)
4. **Circular reference detection** — If solution A references B and B references A, the build fails with a clear error

### What You Learned

- Sub-solutions referenced via the `solution` provider are automatically discovered during `build`
- All nested file dependencies are included in the parent bundle — no extra `bundle.include` needed
- Path normalization ensures sub-solution paths resolve correctly within the bundle
- Circular sub-solution references are detected and reported at build time
- `--dry-run` shows the full recursive file tree

---

## Verifying and Extracting Bundles

After building a bundle, you can verify its integrity and examine its contents.

### Step 1: Verify the Bundle

{{< tabs "catalog-tutorial-cmd-47" >}}
{{% tab "Bash" %}}
```bash
scafctl bundle verify deploy-app@1.0.0
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl bundle verify deploy-app@1.0.0
```
{{% /tab %}}
{{< /tabs >}}

Expected output:

```
 💡 Verifying deploy-app@1.0.0...
  Static paths:
  Bundle includes (glob coverage): ✅
    ✓ configs/**/*.yaml
 ✅ Verification passed: 1 item(s) checked
```

This checks that:
- All files referenced in the solution exist in the bundle
- Glob patterns in `bundle.include` cover the expected files

### Step 2: List Bundle Contents

See what files are inside the bundle without extracting them:

{{< tabs "catalog-tutorial-cmd-48" >}}
{{% tab "Bash" %}}
```bash
scafctl bundle extract deploy-app@1.0.0 --list-only
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl bundle extract deploy-app@1.0.0 --list-only
```
{{% /tab %}}
{{< /tabs >}}

Expected output:

```
  templates/deployment.yaml    (500 B)
  configs/dev.yaml             (100 B)
  configs/prod.yaml            (105 B)
💡 Total: 3 file(s), 705 B
```

### Step 3: Extract to a Directory

Extract the bundled files to inspect them:

{{< tabs "catalog-tutorial-cmd-49" >}}
{{% tab "Bash" %}}
```bash
scafctl bundle extract deploy-app@1.0.0 --output-dir ./extracted
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl bundle extract deploy-app@1.0.0 --output-dir ./extracted
```
{{% /tab %}}
{{< /tabs >}}

Check the extracted files:

```bash
ls -R extracted/
```

You'll see the full directory structure preserved:

```
extracted/
├── configs/
│   ├── dev.yaml
│   └── prod.yaml
└── templates/
    └── deployment.yaml
```

### Step 4: Extract Files for a Specific Resolver

You can extract only the files needed by a specific resolver:

{{< tabs "catalog-tutorial-cmd-50" >}}
{{% tab "Bash" %}}
```bash
scafctl bundle extract deploy-app@1.0.0 --resolver config --output-dir ./config-only
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl bundle extract deploy-app@1.0.0 --resolver config --output-dir ./config-only
```
{{% /tab %}}
{{< /tabs >}}

This uses static analysis to determine which files the `config` resolver references.

### Step 5: Clean Up

```bash
rm -rf extracted/ config-only/
```

### What You Learned

- `scafctl bundle verify` checks that a bundle contains all required files
- `scafctl bundle extract --list-only` shows bundle contents without extracting
- `scafctl bundle extract --output-dir DIR` extracts files to a directory
- `--resolver NAME` extracts only files needed by a specific resolver
- Use `--flatten` to extract all files to a single directory (no subdirectories)

---

## Comparing Bundle Versions

When you release a new version of a bundled solution, `bundle diff` shows exactly what changed.

### Step 1: Create a v2 with Changes

Add a new config file and modify the template. First, create `deploy-app/configs/staging.yaml`:

```yaml
name: my-app
namespace: staging
replicas: 2
image: my-app:1.2.0-rc1
port: 8080
```

Then update `deploy-app/solution.yaml` to bump the version:

```yaml
metadata:
  name: deploy-app
  version: 2.0.0
```

### Step 2: Build v2

{{< tabs "catalog-tutorial-cmd-51" >}}
{{% tab "Bash" %}}
```bash
scafctl build solution deploy-app/solution.yaml
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl build solution deploy-app/solution.yaml
```
{{% /tab %}}
{{< /tabs >}}

Expected output:

```
 💡   Bundled 4 file(s) (1.2 KB, deduplicated: 1 layer(s))
 ✅ Built deploy-app@2.0.0
 💡   Digest: sha256:xyz789...
 💡   Catalog: ~/.local/share/scafctl/catalog
```

Notice it now bundles 4 files (the new staging config was picked up by `configs/**/*.yaml`).

### Step 3: Compare the Two Versions

{{< tabs "catalog-tutorial-cmd-52" >}}
{{% tab "Bash" %}}
```bash
scafctl bundle diff deploy-app@1.0.0 deploy-app@2.0.0
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl bundle diff deploy-app@1.0.0 deploy-app@2.0.0
```
{{% /tab %}}
{{< /tabs >}}

The output shows files added, modified, and removed between the two versions.

### Step 4: Show Only File Changes

{{< tabs "catalog-tutorial-cmd-53" >}}
{{% tab "Bash" %}}
```bash
scafctl bundle diff deploy-app@1.0.0 deploy-app@2.0.0 --files-only
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl bundle diff deploy-app@1.0.0 deploy-app@2.0.0 --files-only
```
{{% /tab %}}
{{< /tabs >}}

### Step 5: Show Only Solution Structure Changes

{{< tabs "catalog-tutorial-cmd-54" >}}
{{% tab "Bash" %}}
```bash
scafctl bundle diff deploy-app@1.0.0 deploy-app@2.0.0 --solution-only
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl bundle diff deploy-app@1.0.0 deploy-app@2.0.0 --solution-only
```
{{% /tab %}}
{{< /tabs >}}

This shows only changes to the solution YAML itself (resolvers added/removed, actions changed, etc.).

### Step 6: Get Diff Output as YAML

{{< tabs "catalog-tutorial-cmd-55" >}}
{{% tab "Bash" %}}
```bash
scafctl bundle diff deploy-app@1.0.0 deploy-app@2.0.0 -o yaml
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl bundle diff deploy-app@1.0.0 deploy-app@2.0.0 -o yaml
```
{{% /tab %}}
{{< /tabs >}}

### Step 7: Clean Up

{{< tabs "catalog-tutorial-cmd-56" >}}
{{% tab "Bash" %}}
```bash
scafctl catalog delete deploy-app@1.0.0 --kind solution
scafctl catalog delete deploy-app@2.0.0 --kind solution
scafctl catalog prune
rm -rf deploy-app/
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl catalog delete deploy-app@1.0.0 --kind solution
scafctl catalog delete deploy-app@2.0.0 --kind solution
scafctl catalog prune
rm -rf deploy-app/
```
{{% /tab %}}
{{< /tabs >}}

### What You Learned

- `scafctl bundle diff REF_A REF_B` compares two versions of a bundled solution
- `--files-only` shows only file-level changes (added, modified, removed)
- `--solution-only` shows only solution structure changes (resolvers, actions)
- `-o yaml` or `-o json` gives machine-readable diff output

---

## Using the Catalog with the MCP Server

When using AI agents (VS Code Copilot, Claude, Cursor), the MCP server provides catalog tools:

- **`catalog_list`** — List catalog entries filtered by kind and name
- **`catalog_inspect`** — Get detailed metadata for a specific catalog artifact — version, kind, digest, created timestamp, and dependency list

The AI can inspect catalog artifacts, look up solution versions, and help you manage your catalog.

## Next Steps

- [Go Templates Tutorial](go-templates-tutorial.md) — Generate structured text with Go templates
- [Snapshots Tutorial](snapshots-tutorial.md) — Capture and compare execution snapshots
- [Functional Testing Tutorial](functional-testing.md) — Write and run automated tests
- [Configuration Tutorial](config-tutorial.md) — Manage application configuration
- [MCP Server Tutorial](mcp-server-tutorial.md) — AI-assisted catalog management
