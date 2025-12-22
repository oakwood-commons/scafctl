# scafctl publish (Proposal)

Publish a previously built solution artifact to a catalog endpoint.

## Usage

```
scafctl publish <artifact> [flags]
```

- `<artifact>` refers to the file produced by `scafctl build` (e.g., `./build/terraform-multi-env.tgz`).

## Flags

```
      --catalog <url>      Catalog endpoint to receive the artifact (required unless configured)
      --token <value>      Authentication token or credential reference
      --force              Overwrite existing catalog entries
      --quiet              Suppress informational output
      --debug              Emit debug logs
```

## Behavior

- Validates the artifact manifest before upload.
- Pushes the artifact to the configured catalog.
- Registers metadata (version, tags, maintainers) with the catalog API.
- Optionally overwrites existing entries when `--force` is provided.

## Examples

### Publish to default catalog

```
scafctl publish ./build/terraform-multi-env.tgz
```

### Publish to a specific catalog endpoint

```
scafctl publish ./build/terraform-multi-env.tgz --catalog https://catalog.example.com --token $CATALOG_TOKEN
```

### Force an overwrite

```
scafctl publish ./build/terraform-multi-env.tgz --force
```

## Notes

- Publishing requires network connectivity and appropriate credentials.
- Catalog endpoints and tokens can be configured via environment variables or config files (planned).
