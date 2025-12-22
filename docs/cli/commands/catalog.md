# scafctl catalog (Proposal)

Interact with solution catalogs: discover available solutions, fetch catalog metadata, and manage local cache entries. Catalogs host built artifacts produced by `scafctl build` and published via `scafctl publish`.

> **Status:** Proposal. The catalog subsystem and CLI exporter are not implemented yet. This document captures the intended user experience for future development.

## Usage

```
scafctl catalog <command> [flags]
```

## Available Subcommands

- `list` — List solutions available in the configured catalog(s)
- `search <query>` — Search catalog entries by name, tag, or metadata
- `show <solution[@version]>` — Display catalog metadata for a specific solution
- `pull <solution[@version]>` — Download an artifact into the local cache
- `remove <solution[@version]>` — Remove cached artifact locally
- `index` — Update local catalog index (sync)
- `login <catalog>` — (Future) authenticate against a catalog endpoint
- `logout <catalog>` — (Future) revoke catalog credentials

## Shared Flags

```
      --profile <name>     Use configuration profile (defaults to "default")
      --catalog <name|url> Select a specific catalog (overrides profile default)
      --output <format>    Output format: text (default), json, yaml
      --quiet              Suppress informational output
      --debug              Emit debug logs
```

Catalogs can be defined in the config file with aliases (see [Config Schema](../../schemas/config-schema.md)). The `--catalog` flag accepts either an alias (`prod`) or a full URL (`https://catalog.example.com`).

## Behavior Overview

- `list` and `search` query the catalog API and optionally cache results locally for offline use.
- `show` renders detailed metadata including version history, tags, maintainers, and required providers.
- `pull` downloads the artifact bundle into the local catalog cache (`~/.scafctl/catalog/<catalog>/<solution>/<version>`).
- `remove` clears cached artifacts without touching the remote catalog.
- `index` refreshes the local index to pick up new entries or invalidate removed ones.
- `login/logout` integrate with the auth subsystem when catalog endpoints require authentication.

## Examples (Planned Behavior)

### List available solutions

```
scafctl catalog list
```

### Search by tag

```
scafctl catalog search "tag:terraform"
```

### Show solution metadata

```
scafctl catalog show solution:terraform-multi-env@1.2.0
```

### Pull artifact locally

```
scafctl catalog pull solution:terraform-multi-env@1.2.0
```

### Remove cached artifact

```
scafctl catalog remove solution:terraform-multi-env@1.0.0
```

### Refresh local index for prod catalog

```
scafctl catalog index --catalog prod
```

## Interaction with Build & Publish

- `scafctl build` produces the artifact that `catalog pull` retrieves.
- `scafctl publish` pushes artifacts to the remote catalog. Catalog commands focus on discovery and consumption; publish remains a top-level command to support alternative destinations.

## Local Cache Layout (Proposed)

```
~/.scafctl/catalog/
  default/
    terraform-multi-env/
      1.0.0/
        artifact.tgz
        manifest.yaml
  prod/
    terraform-multi-env/
      1.2.0/
        artifact.tgz
        manifest.yaml
```

## Roadmap Notes

- **Offline support:** Allow `catalog list` to work offline using cached index data.
- **Multiple catalogs:** Support fallback order when multiple catalogs configured in a profile.
- **Catalog auth:** Reuse `scafctl auth` sessions to authenticate `catalog` calls.
- **Verification:** Validate artifact signatures/checksums during `pull`.
- **Generated Docs:** Once the CLI exporter exists, `scafctl catalog --help --format markdown` will populate this file.

### Related Documentation

- [CLI Config Command](./config.md)
- [CLI Auth Command](./auth.md)
- [Config Schema](../../schemas/config-schema.md)
- [Solution Schema](../../schemas/solution-schema.md)
