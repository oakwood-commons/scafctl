# catalog

Manage catalog sources and resolve versioned resources (solutions, datasources, releases, providers).

## Synopsis

```
scafctl catalog <subcommand> [flags]
```

## Subcommands

- `add-source` - Add a catalog source
- `list` - List available packages
- `get` - Resolve and fetch a resource by version constraint
- `verify` - Verify integrity and signatures of a resource
- `remove-source` - Remove a catalog source

---

## catalog add-source

Add a catalog source (local folder, bucket, or HTTP URL) with priority.

### Synopsis

```
scafctl catalog add-source <name> <uri> [flags]
```

### Arguments

- `<name>` - Source name (must be unique)
- `<uri>` - Local path, bucket URI, or HTTP(S) URL
  - Examples: `/path/to/catalog`, `file:///path/to/catalog`, `s3://bucket/prefix`, `gs://bucket/prefix`, `https://example.com/catalog/`

### Flags

- `--priority <int>` - Source priority (lower numbers = higher priority, default: 100)
- `--config <path>` - Path to scafctl config file (default: `~/.config/scafctl/config.yaml`)

### Examples

```bash
# Add a local catalog source
scafctl catalog add-source local-dev /home/user/my-catalog

# Add an S3 bucket source with high priority
scafctl catalog add-source corp-bucket s3://my-corp-bucket/catalog --priority 10

# Add an HTTP read-only catalog
scafctl catalog add-source public https://catalog.example.com/
```

---

## catalog list

List available packages from catalog sources.

### Synopsis

```
scafctl catalog list [flags]
```

### Flags

- `--kind <string>` - Filter by resource kind: `solution`, `datasource`, `release`, `provider`
- `--source <name>` - Query a specific source (default: all configured sources)
- `--output <format>` - Output format: `table` (default), `json`, `yaml`
- `--no-cache` - Force refresh, ignore cached indexes

### Examples

```bash
# List all packages
scafctl catalog list

# List only solutions
scafctl catalog list --kind solution

# List from a specific source
scafctl catalog list --source corp-bucket --output json
```

---

## catalog get

Resolve a version by constraint and fetch the resource.

### Synopsis

```
scafctl catalog get <kind>/<name>@<constraint> [flags]
```

### Arguments

- `<kind>/<name>@<constraint>` - Resource reference
  - Kind: `solution`, `datasource`, `release`, `provider`
  - Name: Package name
  - Constraint: SemVer constraint (e.g., `^1.2.0`, `~1.3.2`, `>=1.0.0 <2.0.0`)

### Flags

- `--pre` - Include prerelease versions (alpha, beta, rc)
- `--source <name>` - Pin to a specific catalog source
- `--output <path>` - Download artifact to this path (default: print resolved descriptor)
- `--verify` - Verify digest and signature (default: true)
- `--no-cache` - Force re-download, ignore local cache

#### Platform flags (releases only)

- `--platform <os>/<arch>` - Target platform (default: current runtime)
  - OS: `linux`, `windows`, `darwin`
  - Arch: `amd64`, `arm64`, `arm`
- `--libc <gnu|musl>` - Libc flavor (Linux only, default: auto-detect)
- `--variant <string>` - Arch variant (e.g., `armv7`)

### Examples

```bash
# Resolve latest stable 1.x solution
scafctl catalog get solution/my-solution@^1.0.0

# Include prereleases
scafctl catalog get solution/my-solution@^1.3.0 --pre

# Download a release for the current platform
scafctl catalog get release/tool-x@^2.1.0 --output ./tool-x

# Get a specific platform artifact
scafctl catalog get release/tool-x@^2.1.0 --platform linux/amd64 --libc musl --output ./tool-x-musl

# Pin to a source and disable verification
scafctl catalog get provider/git@>=0.2.0 --source local-dev --verify=false
```

### Output

By default, prints the resolved descriptor as JSON:

```json
{
  "kind": "solution",
  "name": "my-solution",
  "version": "1.2.3",
  "path": "solutions/my-solution/v1.2.3/package.tgz",
  "digest": "sha256:1f2d...",
  "size": 123456,
  "source": "corp-bucket"
}
```

For releases, includes selected artifact:

```json
{
  "kind": "release",
  "name": "tool-x",
  "version": "2.1.0",
  "artifact": {
    "os": "linux",
    "arch": "amd64",
    "libc": "musl",
    "path": "releases/tool-x/v2.1.0/tool-x_2.1.0_linux_amd64_musl.tar.gz",
    "digest": "sha256:3333...",
    "size": 19994455
  },
  "source": "corp-bucket"
}
```

---

## catalog verify

Verify the integrity and signature of a resource version.

### Synopsis

```
scafctl catalog verify <kind>/<name>@<version> [flags]
```

### Arguments

- `<kind>/<name>@<version>` - Exact version (no constraint, must be a specific SemVer)

### Flags

- `--source <name>` - Pin to a specific catalog source
- `--signature-verify` - Verify detached signature if present (default: true when signature exists)

### Examples

```bash
# Verify a solution version
scafctl catalog verify solution/my-solution@1.2.3

# Verify with signature check
scafctl catalog verify release/tool-x@2.1.0 --signature-verify
```

---

## catalog remove-source

Remove a catalog source from the configuration.

### Synopsis

```
scafctl catalog remove-source <name> [flags]
```

### Arguments

- `<name>` - Source name to remove

### Flags

- `--config <path>` - Path to scafctl config file (default: `~/.config/scafctl/config.yaml`)

### Examples

```bash
# Remove a source
scafctl catalog remove-source local-dev
```

---

## Global Flags

All catalog commands inherit global scafctl flags:

- `--verbose`, `-v` - Increase verbosity (can be repeated: `-vv`, `-vvv`)
- `--quiet` - Suppress non-error output

---

## Error Handling

Common exit codes:

- `0` - Success
- `1` - General error (invalid arguments, network failure)
- `2` - Not found (no version satisfying constraint)
- `3` - Verification failure (digest or signature mismatch)

Examples of errors:

```bash
# No matching version
$ scafctl catalog get solution/my-solution@^2.0.0
Error: no version satisfying constraint "^2.0.0" found for solution/my-solution
  - Searched sources: corp-bucket, local-dev
  - Available versions: 1.2.3, 1.3.0-beta.1

# Platform not available
$ scafctl catalog get release/tool-x@^2.1.0 --platform linux/s390x
Error: no artifact for platform linux/s390x in release/tool-x@2.1.0
  - Available platforms: windows/amd64, linux/amd64, darwin/arm64

# Digest mismatch
$ scafctl catalog get solution/my-solution@1.2.3
Error: digest verification failed for solution/my-solution@1.2.3
  - Expected: sha256:1f2d...
  - Actual:   sha256:9999...
```

---

## Configuration

Sources are stored in `~/.config/scafctl/config.yaml` (or via `--config` flag):

```yaml
catalog:
  sources:
    - name: corp-bucket
      uri: s3://my-corp-bucket/catalog
      priority: 10
    - name: local-dev
      uri: /home/user/my-catalog
      priority: 50
  cache:
    dir: ~/.cache/scafctl/catalog
    ttl: 3600  # seconds
```

---

## See Also

- [publish](publish.md) - Publish resources to a catalog
- [Catalog Design](../../design/catalog.md)
- [Catalog Schema](../../schemas/catalog-index.md)
