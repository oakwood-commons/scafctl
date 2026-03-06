# Multi-Platform Plugin Build Example

This example demonstrates building a multi-platform plugin artifact.

## Prerequisites

Create mock binaries (in real usage, these would be cross-compiled Go binaries):

```bash
mkdir -p dist
echo "linux-amd64-binary" > dist/my-provider-linux-amd64
echo "darwin-arm64-binary" > dist/my-provider-darwin-arm64
```

## Build Multi-Platform Artifact

```bash
scafctl build plugin \
  --name my-provider \
  --kind provider \
  --version 1.0.0 \
  --platform linux/amd64=dist/my-provider-linux-amd64 \
  --platform darwin/arm64=dist/my-provider-darwin-arm64
```

## Build All Supported Platforms

```bash
scafctl build plugin \
  --name my-provider \
  --kind provider \
  --version 1.0.0 \
  --platform linux/amd64=dist/my-provider-linux-amd64 \
  --platform linux/arm64=dist/my-provider-linux-arm64 \
  --platform darwin/amd64=dist/my-provider-darwin-amd64 \
  --platform darwin/arm64=dist/my-provider-darwin-arm64 \
  --platform windows/amd64=dist/my-provider-windows-amd64.exe
```

## Auth Handler Example

```bash
scafctl build plugin \
  --name github-auth \
  --kind auth-handler \
  --version 2.0.0 \
  --platform linux/amd64=dist/github-auth-linux-amd64 \
  --platform darwin/arm64=dist/github-auth-darwin-arm64
```

## Push to Remote Registry

```bash
scafctl catalog push my-provider@1.0.0 --catalog ghcr.io/myorg/scafctl
```

## Reference in a Solution

```yaml
metadata:
  name: my-solution
  version: 1.0.0

bundle:
  plugins:
    - name: my-provider
      kind: provider
      version: "^1.0.0"
```

At runtime, scafctl detects the current platform and selects the correct
binary from the OCI image index automatically.
