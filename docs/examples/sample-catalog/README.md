# Sample Catalog

This directory demonstrates a complete catalog structure for scafctl. It shows how to organize solutions, datasources, releases, and providers with multiple versions, platforms, and metadata.

## Structure Overview

```
sample-catalog/
  catalog.json              # Optional: top-level package discovery
  solutions/
    web-app/                # Solution package
      index.json            # Version index
      v1.0.0/               # Stable version
      v1.1.0-beta.1/        # Prerelease version
  datasources/
    customer-data/          # Datasource package
      index.json
      v0.2.0/
  releases/
    cli-tool/               # Multi-platform release
      index.json
      v2.1.0/
        cli-tool_2.1.0_windows_amd64.zip
        cli-tool_2.1.0_linux_amd64_gnu.tar.gz
        cli-tool_2.1.0_linux_amd64_musl.tar.gz
        cli-tool_2.1.0_darwin_arm64.tar.gz
        checksums.txt
  providers/
    git/                    # Provider plugin
      index.json
      v0.3.0/
```

## Usage Examples

### Add this catalog as a local source

```bash
scafctl catalog add-source sample-local ./docs/examples/sample-catalog
```

### List all packages

```bash
scafctl catalog list --source sample-local
```

### Resolve a solution

```bash
scafctl catalog get solution/web-app@^1.0.0 --source sample-local
```

### Get a platform-specific release

```bash
# Auto-detect current platform
scafctl catalog get release/cli-tool@^2.1.0 --source sample-local

# Specify Linux with musl
scafctl catalog get release/cli-tool@^2.1.0 --platform linux/amd64 --libc musl --source sample-local
```

### Include prereleases

```bash
scafctl catalog get solution/web-app@^1.1.0 --pre --source sample-local
```

## Notes

- All artifact files (`.tgz`, `.zip`, etc.) are referenced but not included in this example.
- In a real catalog, these files would contain the actual packaged artifacts.
- The JSON indexes demonstrate the schema and metadata structure.
- Checksums and signatures are placeholders for demonstration.

## See Also

- [Catalog Design](../../design/catalog.md)
- [Catalog Schema](../../schemas/catalog-index.md)
- [Catalog CLI Commands](../../cli/commands/catalog.md)
