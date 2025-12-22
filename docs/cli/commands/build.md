# scafctl build (Proposal)

Build a solution into a catalog-ready artifact. This packages the solution, documentation, and tests into the format expected by the scafctl catalog.

## Usage

```
scafctl build [path] [flags]
```

- `path` defaults to the current directory and must contain a `solution.yaml` file.

## Flags

```
      --output <dir>       Destination directory for the build artifact (defaults to ./build)
      --no-cache           Recompute resolver defaults and metadata from scratch
      --quiet              Suppress informational output
      --debug              Emit debug logs
```

## Behavior

- Validates the solution schema.
- Resolves metadata such as `spec.catalog` entries.
- Bundles templates, tests, and auxiliary files into a `.tar.gz` (or similar) artifact.
- Emits a manifest describing the built solution and its checksum.

## Examples

### Build current solution

```
scafctl build .
```

### Build into a custom directory

```
scafctl build ./solutions/terraform --output ./dist
```

### Force a fresh build without caches

```
scafctl build . --no-cache
```

## Notes

- Build does not publish artifacts; use `scafctl publish` once a build succeeds.
- Build fails fast if validation errors are detected.
- When legacy schema elements are found, build surfaces the validator hint to run `scafctl migrate` before retrying.
