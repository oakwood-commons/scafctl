---
title: "Embedder Guide: Catalog Default Configuration"
---

# Embedder Guide: Catalog Default Configuration

This document explains how embedders (e.g., cldctl) should consume scafctl's
catalog configuration system. It covers the reserved name enforcement model,
how to add organization-specific catalogs, and how to migrate away from custom
merge logic.

## Background

scafctl ships two built-in catalog entries in its embedded `defaults.yaml`:

- **`local`** -- a filesystem catalog for locally-cached artifacts
- **`official`** -- the official OCI catalog at `ghcr.io/oakwood-commons`

These names are **reserved**. Their configuration is enforced by scafctl at
load time and cannot be overridden by user config files or embedder base
configs.

## Reserved Name Enforcement

When `config.Manager.Load()` runs, it calls `mergeDefaultCatalogEntries`
which enforces the following rule:

- If a catalog entry uses a **reserved name** (`local` or `official`), all
  fields are overwritten with the values from scafctl's embedded defaults.
- If a catalog entry uses a **non-reserved name**, missing fields are
  backfilled from defaults without overwriting user values.

This prevents users from redirecting `official` to an untrusted registry.

The same enforcement applies at the file level when `EnsureDefaults` merges
entries into an existing config file on disk.

### Checking Reserved Names

~~~go
import "github.com/oakwood-commons/scafctl/pkg/config"

if config.IsReservedCatalogName("official") {
    // true -- this name is owned by scafctl defaults
}

if config.IsReservedCatalogName("ford-internal") {
    // false -- safe to use as an embedder catalog name
}
~~~

## How Embedders Should Add Custom Catalogs

### Step 1: Call `EnsureDefaults` for file-level setup

On startup, call `EnsureDefaults` to create or update the user's config file.
This writes the full embedded defaults on first run, and merges missing entries
(with reserved name enforcement) on subsequent runs.

~~~go
import "github.com/oakwood-commons/scafctl/pkg/config"

configPath := "/path/to/config.yaml" // or use paths.ConfigFile()
if err := config.EnsureDefaults(configPath); err != nil {
    return fmt.Errorf("ensuring config defaults: %w", err)
}
~~~

### Step 2: Inject embedder catalogs via `WithBaseConfig`

Use `WithBaseConfig` to add organization-specific catalogs as a config layer.
This layer is merged after scafctl's built-in defaults but before the user's
config file, so users can still override non-reserved values.

~~~go
baseConfig := []byte(`
catalogs:
  - name: ford-internal
    type: oci
    url: oci://ghcr.io/ford-cloud/catalog
    authProvider: github
    discoveryStrategy: auto
settings:
  defaultCatalog: ford-internal
`)

mgr := config.NewManager(configPath, config.WithBaseConfig(baseConfig))
cfg, err := mgr.Load()
if err != nil {
    return fmt.Errorf("loading config: %w", err)
}
~~~

**Do not use reserved names** (`local`, `official`) in your base config.
Those entries will be overwritten by scafctl's defaults at load time.

### Step 3: Remove custom merge logic

Embedders should **not** implement their own catalog merge functions. The
`mergeCatalogs` function in cldctl's `pkg/config/defaults.go` should be
deleted. It is redundant and contains a bug that panics when catalog entries
have nested map values:

~~~
panic: runtime error: comparing uncomparable type map[string]interface {}
~~~

All merge logic is handled by scafctl's `EnsureDefaults` (file level) and
`mergeDefaultCatalogEntries` (runtime level).

## Config Layer Ordering

The final configuration is built from these layers, last wins:

| Layer | Source | Reserved names enforced? |
|-------|--------|--------------------------|
| 1. Built-in defaults | `setDefaults()` | N/A (source of truth) |
| 2. Embedder base config | `WithBaseConfig(data)` | Yes, after merge |
| 3. User config file | `~/.config/scafctl/config.yaml` | Yes, after merge |
| 4. Environment variables | `SCAFCTL_*` / `CLDCTL_*` | N/A (scalar only) |
| 5. Reserved enforcement | `mergeDefaultCatalogEntries` | **Enforced** |

Viper replaces arrays entirely when merging layers, so catalogs defined in
layer 2 may be lost if the user's config file (layer 3) also defines a
`catalogs` array. The `mergeDefaultCatalogEntries` step re-adds any missing
default entries after Viper's merge is complete.

## Available API

| Function | Purpose |
|----------|---------|
| `config.EnsureDefaults(path)` | Write/merge config file with defaults |
| `config.NewManager(path, opts...)` | Create a config manager |
| `config.WithBaseConfig(data)` | Inject embedder config layer |
| `config.WithEnvPrefix(prefix)` | Override env var prefix (e.g., `CLDCTL`) |
| `config.IsReservedCatalogName(name)` | Check if a catalog name is reserved |
| `config.DefaultsYAML()` | Get a copy of the embedded defaults |
| `config.EmbeddedCatalogDefaults()` | Get parsed default catalog entries |

## Migration Checklist

- [ ] Delete cldctl's `mergeCatalogs` function
- [ ] Replace file-level merge with `config.EnsureDefaults(configPath)`
- [ ] Move cldctl-specific catalogs into a `WithBaseConfig` call
- [ ] Verify no cldctl catalog uses a reserved name (`local`, `official`)
- [ ] Run `cldctl catalog index push --dry-run` to confirm no panic
