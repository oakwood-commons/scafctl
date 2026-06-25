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

## How the Helper Is Installed (Unix vs. Windows)

`credential-helper install` makes scafctl discoverable to Docker/Podman as
`docker-credential-scafctl`. The mechanism differs by platform:

- **Unix (Linux, macOS):** a `docker-credential-scafctl` **symlink** is created
  in the bin directory pointing at the scafctl binary.
- **Windows:** symlinks usually require elevation, so scafctl writes an
  elevation-free **forwarding shim** named `docker-credential-scafctl.cmd`
  instead. The shim simply forwards to `scafctl credential-helper <verb>`. If a
  symlink cannot be created on Unix either, scafctl falls back to the same shim.

```powershell
# Windows
scafctl credential-helper install --bin-dir "$env:USERPROFILE\bin" --docker
```

> The bin directory must be on your `PATH` so Docker/Podman can locate the
> helper. On Windows the `.cmd` extension must be present in `PATHEXT` (it is by
> default). `scafctl credential-helper uninstall` removes the symlink or the
> managed shim; it refuses to delete a path it did not create.

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
