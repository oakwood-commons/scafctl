# Docker Credential Helper Configuration Examples

## Docker — Global Credential Store

Configure Docker to use scafctl for all registries:

```bash
scafctl credential-helper install --docker
```

Produces `~/.docker/config.json`:

```json
{
  "credsStore": "scafctl"
}
```

## Docker — Per-Registry

Use scafctl only for specific registries:

```bash
scafctl credential-helper install --docker --registry ghcr.io
scafctl credential-helper install --docker --registry quay.io
```

Produces:

```json
{
  "credHelpers": {
    "ghcr.io": "scafctl",
    "quay.io": "scafctl"
  }
}
```

## Podman — Global Credential Store

```bash
scafctl credential-helper install --podman
```

Updates `~/.config/containers/auth.json`:

```json
{
  "credsStore": "scafctl"
}
```

## Custom Bin Directory

Place the symlink in a custom directory:

```bash
scafctl credential-helper install --bin-dir /usr/local/bin --docker
```

## Direct Protocol Usage

Test the credential helper directly (useful for debugging):

```bash
# Store a credential
echo '{"ServerURL":"https://ghcr.io","Username":"oauth2","Secret":"mytoken"}' | docker-credential-scafctl store

# Retrieve it
echo "https://ghcr.io" | docker-credential-scafctl get

# List all
docker-credential-scafctl list

# Remove it
echo "https://ghcr.io" | docker-credential-scafctl erase
```
