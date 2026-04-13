---
title: "Docker Credential Helper Tutorial"
weight: 41
---

# Docker Credential Helper Tutorial

This tutorial shows how to use scafctl as a Docker/Podman credential helper, exposing scafctl's AES-256-GCM encrypted credential store to the container ecosystem.

## Prerequisites

- scafctl installed and available in your PATH
- Docker, Podman, or Buildah installed
- Familiarity with [Authentication Tutorial](auth-tutorial.md)

## Table of Contents

1. [Overview](#overview)
2. [Quick Setup](#quick-setup)
3. [Manual Setup](#manual-setup)
4. [Per-Registry Configuration](#per-registry-configuration)
5. [How It Works](#how-it-works)
6. [Using with Podman/Buildah](#using-with-podmanbuildah)
7. [Verification](#verification)
8. [Composing with catalog login](#composing-with-catalog-login)
9. [Uninstall](#uninstall)
10. [Troubleshooting](#troubleshooting)

## Overview

The Docker credential helper protocol allows external programs to manage registry credentials for Docker, Podman, and Buildah. By using scafctl as a credential helper, you get:

- **Encrypted-at-rest credentials** -- scafctl stores all secrets using AES-256-GCM encryption
- **Single credential store** -- credentials stored via `scafctl catalog login` are automatically available to Docker
- **No plaintext files** -- unlike Docker's default `config.json` auth storage

## Quick Setup

Install scafctl as the credential helper for Docker:

```bash
scafctl credential-helper install --docker
```

This creates a `docker-credential-scafctl` symlink in `~/.local/bin` and updates `~/.docker/config.json` to use scafctl as the credential store.

For Podman:

```bash
scafctl credential-helper install --podman
```

## Manual Setup

### 1. Create the Symlink

```bash
scafctl credential-helper install
```

This creates `~/.local/bin/docker-credential-scafctl` pointing to your scafctl binary. Ensure `~/.local/bin` is on your `PATH`:

```bash
export PATH="$HOME/.local/bin:$PATH"
```

### 2. Configure Docker

Add to `~/.docker/config.json`:

```json
{
  "credsStore": "scafctl"
}
```

This tells Docker to use scafctl for all registry credential lookups.

## Per-Registry Configuration

Instead of making scafctl the global credential store, you can configure it for specific registries only:

```bash
scafctl credential-helper install --docker --registry ghcr.io
```

This adds a `credHelpers` entry to Docker's config:

```json
{
  "credHelpers": {
    "ghcr.io": "scafctl"
  }
}
```

You can add multiple registries:

```bash
scafctl credential-helper install --docker --registry ghcr.io
scafctl credential-helper install --docker --registry quay.io
```

## How It Works

The credential helper protocol uses four stdin/stdout commands:

| Command | Input | Output | Description |
|---------|-------|--------|-------------|
| `get` | Server URL (text) | JSON credential | Retrieve credentials |
| `store` | JSON credential | -- | Store credentials |
| `erase` | Server URL (text) | -- | Remove credentials |
| `list` | -- | JSON map | List all credentials |

### Credential Format

```json
{
  "ServerURL": "https://ghcr.io",
  "Username": "oauth2",
  "Secret": "gho_xxxxxxxxxxxx"
}
```

### Error Format

```json
{
  "message": "credentials not found"
}
```

### Lookup Order

When Docker calls `get`, scafctl checks credentials in this order:

1. **credhelper namespace** -- credentials stored via `docker login` (through scafctl)
2. **Native credential store** -- credentials stored via `scafctl catalog login`

This means credentials from either source are available to Docker.

## Using with Podman/Buildah

Podman and Buildah use the same credential helper protocol:

```bash
scafctl credential-helper install --podman
```

Or manually configure `~/.config/containers/auth.json`:

```json
{
  "credHelpers": {
    "ghcr.io": "scafctl"
  }
}
```

## Verification

Test the credential helper directly:

```bash
# List all stored credentials
docker-credential-scafctl list

# Get credentials for a specific registry
echo "https://ghcr.io" | docker-credential-scafctl get

# Verify Docker can use it
docker pull ghcr.io/your-org/your-image:latest
```

## Composing with catalog login

Credentials stored via `scafctl catalog login` are automatically available through the credential helper:

```bash
# Login via scafctl
scafctl catalog login ghcr.io --username oauth2 --password @- < token.txt

# Docker can now pull from ghcr.io without separate docker login
docker pull ghcr.io/your-org/your-image:latest
```

## Uninstall

Remove the credential helper integration:

```bash
# Remove symlink and Docker config entry
scafctl credential-helper uninstall --docker

# Or just the symlink
scafctl credential-helper uninstall

# For Podman
scafctl credential-helper uninstall --podman
```

## Troubleshooting

### "docker-credential-scafctl: command not found"

Ensure `~/.local/bin` (or your custom `--bin-dir`) is on your PATH:

```bash
export PATH="$HOME/.local/bin:$PATH"
```

### "credentials not found"

1. Verify the credential exists: `docker-credential-scafctl list`
2. Check if you logged in with scafctl: `scafctl catalog login <registry>`
3. Check the server URL matches exactly (including scheme)

### Master key issues

The credential helper uses scafctl's encrypted secrets store. If the master key is not available, all operations will fail. Ensure:

```bash
# Check if the secrets store is accessible
scafctl secrets list
```

### Permission denied on symlink

If you can't write to `~/.local/bin`, specify a different directory:

```bash
scafctl credential-helper install --bin-dir /usr/local/bin --docker
```
