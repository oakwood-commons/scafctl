---
title: "Multi-Platform Plugin Build Tutorial"
weight: 140
---

# Multi-Platform Plugin Build Tutorial

This tutorial walks through building and distributing multi-platform plugin
artifacts using OCI image indexes.

## Overview

Plugin artifacts (providers, auth handlers) are platform-specific binaries.
To distribute a single plugin that works on multiple OS/architecture
combinations, scafctl stores them as an **OCI image index** (also called a
"fat manifest"). At runtime, scafctl automatically selects the correct
binary for the current platform.

### Supported Platforms

| Platform | Description |
|----------|-------------|
| `linux/amd64` | Linux x86-64 |
| `linux/arm64` | Linux ARM64 (e.g. AWS Graviton) |
| `darwin/amd64` | macOS Intel |
| `darwin/arm64` | macOS Apple Silicon |
| `windows/amd64` | Windows x86-64 |

## Prerequisites

- scafctl CLI installed
- Go toolchain (for cross-compilation)
- Plugin source code that builds for multiple platforms

## Step 1: Cross-Compile the Plugin

Use Go's cross-compilation to build binaries for each target platform:

```bash
# Set the plugin name and version
PLUGIN_NAME=my-provider
VERSION=1.0.0

# Build for all supported platforms
GOOS=linux   GOARCH=amd64 go build -o dist/${PLUGIN_NAME}-linux-amd64   ./cmd/plugin
GOOS=linux   GOARCH=arm64 go build -o dist/${PLUGIN_NAME}-linux-arm64   ./cmd/plugin
GOOS=darwin  GOARCH=amd64 go build -o dist/${PLUGIN_NAME}-darwin-amd64  ./cmd/plugin
GOOS=darwin  GOARCH=arm64 go build -o dist/${PLUGIN_NAME}-darwin-arm64  ./cmd/plugin
GOOS=windows GOARCH=amd64 go build -o dist/${PLUGIN_NAME}-windows-amd64.exe ./cmd/plugin
```

## Step 2: Build Multi-Platform Artifact

Use `scafctl build plugin` to package all binaries into a single
multi-platform artifact:

{{< tabs "multi-platform-plugin-build-cmd-1" >}}
{{% tab "Bash" %}}
```bash
scafctl build plugin \
  --name my-provider \
  --kind provider \
  --version 1.0.0 \
  --platform linux/amd64=./dist/my-provider-linux-amd64 \
  --platform linux/arm64=./dist/my-provider-linux-arm64 \
  --platform darwin/amd64=./dist/my-provider-darwin-amd64 \
  --platform darwin/arm64=./dist/my-provider-darwin-arm64 \
  --platform windows/amd64=./dist/my-provider-windows-amd64.exe
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl build plugin `
  --name my-provider `
  --kind provider `
  --version 1.0.0 `
  --platform linux/amd64=./dist/my-provider-linux-amd64 `
  --platform linux/arm64=./dist/my-provider-linux-arm64 `
  --platform darwin/amd64=./dist/my-provider-darwin-amd64 `
  --platform darwin/arm64=./dist/my-provider-darwin-arm64 `
  --platform windows/amd64=./dist/my-provider-windows-amd64.exe
```
{{% /tab %}}
{{< /tabs >}}

This creates an OCI image index in the local catalog with one manifest per
platform.

### Output

```
  linux/amd64 → ./dist/my-provider-linux-amd64 (12.3 MB)
  linux/arm64 → ./dist/my-provider-linux-arm64 (11.8 MB)
  darwin/amd64 → ./dist/my-provider-darwin-amd64 (13.1 MB)
  darwin/arm64 → ./dist/my-provider-darwin-arm64 (12.5 MB)
  windows/amd64 → ./dist/my-provider-windows-amd64.exe (14.2 MB)
✓ Built my-provider@1.0.0 (5 platform(s))
  Digest: sha256:abc123...
  Catalog: ~/.local/share/scafctl/catalog/
  Platform: linux/amd64
  Platform: linux/arm64
  Platform: darwin/amd64
  Platform: darwin/arm64
  Platform: windows/amd64
```

## Step 3: Push to Remote Registry

Push the multi-platform artifact to an OCI registry:

{{< tabs "multi-platform-plugin-build-cmd-2" >}}
{{% tab "Bash" %}}
```bash
scafctl catalog push my-provider@1.0.0 --catalog ghcr.io/myorg/scafctl
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl catalog push my-provider@1.0.0 --catalog ghcr.io/myorg/scafctl
```
{{% /tab %}}
{{< /tabs >}}

The image index and all platform manifests are pushed together.

## Step 4: Runtime Platform Selection

When a solution references this plugin, scafctl automatically selects
the correct platform binary:

```yaml
# solution.yaml
metadata:
  name: my-solution
  version: 1.0.0

bundle:
  plugins:
    - name: my-provider
      kind: provider
      version: "^1.0.0"
```

On `darwin/arm64` (Apple Silicon), scafctl will:
1. Resolve `my-provider@^1.0.0` from the catalog chain
2. Detect the artifact is an OCI image index
3. Select the `darwin/arm64` manifest from the index
4. Fetch and cache the correct binary

## Building for a Subset of Platforms

You don't need to build for all platforms. For example, if your plugin
only supports Linux:

{{< tabs "multi-platform-plugin-build-cmd-3" >}}
{{% tab "Bash" %}}
```bash
scafctl build plugin \
  --name my-provider \
  --kind provider \
  --version 1.0.0 \
  --platform linux/amd64=./dist/linux-amd64/my-provider \
  --platform linux/arm64=./dist/linux-arm64/my-provider
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl build plugin `
  --name my-provider `
  --kind provider `
  --version 1.0.0 `
  --platform linux/amd64=./dist/linux-amd64/my-provider `
  --platform linux/arm64=./dist/linux-arm64/my-provider
```
{{% /tab %}}
{{< /tabs >}}

If a user on an unsupported platform tries to use this plugin, they'll get a
clear error:

```
Error: platform "darwin/arm64" not found in image index (available: [linux/amd64 linux/arm64])
```

## Auth Handler Example

The same workflow works for auth handler plugins:

{{< tabs "multi-platform-plugin-build-cmd-4" >}}
{{% tab "Bash" %}}
```bash
scafctl build plugin \
  --name github-auth \
  --kind auth-handler \
  --version 2.0.0 \
  --platform linux/amd64=./dist/github-auth-linux-amd64 \
  --platform darwin/arm64=./dist/github-auth-darwin-arm64
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl build plugin `
  --name github-auth `
  --kind auth-handler `
  --version 2.0.0 `
  --platform linux/amd64=./dist/github-auth-linux-amd64 `
  --platform darwin/arm64=./dist/github-auth-darwin-arm64
```
{{% /tab %}}
{{< /tabs >}}

## Overwriting Existing Versions

Use `--force` to overwrite an existing version:

{{< tabs "multi-platform-plugin-build-cmd-5" >}}
{{% tab "Bash" %}}
```bash
scafctl build plugin \
  --name my-provider \
  --kind provider \
  --version 1.0.0 \
  --platform linux/amd64=./dist/my-provider-linux-amd64 \
  --force
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl build plugin `
  --name my-provider `
  --kind provider `
  --version 1.0.0 `
  --platform linux/amd64=./dist/my-provider-linux-amd64 `
  --force
```
{{% /tab %}}
{{< /tabs >}}

## How It Works Internally

### OCI Image Index Structure

A multi-platform plugin is stored as an OCI image index:

```
index.json (image index)
├── manifest-linux-amd64 (image manifest)
│   ├── config.json
│   └── layer: linux-amd64 binary
├── manifest-darwin-arm64 (image manifest)
│   ├── config.json
│   └── layer: darwin-arm64 binary
└── manifest-windows-amd64 (image manifest)
    ├── config.json
    └── layer: windows-amd64 binary
```

### Platform Resolution Order

When fetching a plugin, scafctl uses this resolution strategy:

1. **OCI image index** — If the artifact is an image index, select the manifest
   matching the current platform's OS and architecture
2. **Annotation matching** — Fall back to matching the
   `dev.scafctl.plugin.platform` annotation on individual artifacts (legacy)
3. **Direct fetch** — Fall back to fetching the single artifact directly
   (single-platform artifacts without annotations)

### Content-Addressed Storage

Binary content is stored with content-addressable digests. If two platforms
share the same binary (e.g., a script), the blob is stored only once.
