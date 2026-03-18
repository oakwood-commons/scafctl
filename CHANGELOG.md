# Changelog

All notable changes to this project will be documented in this file.

## [unreleased]

### 🚀 Features

- *(dryrun)* [**breaking**] Replace static `MockBehavior` string with dynamic `WhatIf` function on provider `Descriptor` for context-aware dry-run messages
- *(dryrun)* [**breaking**] Remove resolver-level dry-run (`run resolver --dry-run` removed); use `run solution --dry-run` or `run resolver --graph` instead
- *(dryrun)* [**breaking**] Resolvers now execute normally during dry-run (side-effect-free) instead of being mocked, providing real data for WhatIf messages
- *(dryrun)* Add `--verbose` flag to `run solution --dry-run` to include materialized inputs in report
- *(plugin)* Add `DescribeWhatIf` gRPC RPC so plugin providers generate WhatIf messages identical to builtin providers
- *(mcp)* [**breaking**] Rename `mock_data` parameter to `resolver_overrides` in `dry_run_solution` MCP tool
- *(file)* [**breaking**] Add conflict resolution strategies to file provider (`onConflict` input with five strategies: `error`, `overwrite`, `skip`, `skip-unchanged`, `append`)
- *(file)* [**breaking**] Change file provider default write behavior from silent-overwrite to `skip-unchanged` (SHA256 content comparison)
- *(file)* [**breaking**] Change `filesWritten` semantics in `write-tree` output from `len(entries)` to `created + overwritten + appended` (files actually written to disk)
- *(file)* Add `backup` input for `.bak` file creation before mutating existing files (overwrite, skip-unchanged when content differs, append)
- *(file)* Add `append` strategy with `dedupe` option for line-level deduplication (e.g., `.gitignore` management)
- *(file)* Add `failFast` input for `error` strategy in `write-tree` (default: collect all conflicts, then fail)
- *(file)* Add per-file status reporting (`created`, `overwritten`, `skipped`, `unchanged`, `appended`) in `write` and `write-tree` outputs
- *(file)* Add per-entry `onConflict`, `backup`, `dedupe` overrides in `write-tree` entries
- *(cli)* Add `--on-conflict` flag to `run solution` and `run provider` commands
- *(cli)* Add `--backup` flag to `run solution` and `run provider` commands
- *(file)* Add conflict-aware dry-run with `_plannedStatus` and `_strategy` reporting
- *(auth)* [**breaking**] Add builtin GitHub authentication handler (#78)
- *(auth)* Add GCP authentication handler (#80)
- Prepare codebase for MCP server integration (#81)
- *(mcp)* Add MCP server implementation with tools for solutions, providers, CEL, auth, catalog, schema, and examples (#82)
- [**breaking**] Consolidate test schema under \spec.testing\ and add mock HTTP… (#84)
- *(auth/gcp)* [**breaking**] Native browser OAuth for GCP login, add gcloud-adc flow (#85)
- Add schema validation to lint, Entra group membership, and AADS… (#86)
- Add auto and list output formats to kvx (#87)
- *(mcp)* Add solution developer experience tools (Phase 5) (#88)
- *(kvx,provider)* Replace bespoke TUI with schema-driven kvx card-l… (#95)
- *(auth)* Add diagnose command, enhance list/status/token/login com… (#96)

### 🐛 Bug Fixes

- *(auth)* Preserve login-time client ID during token rotation (#79)

### 💼 Other

- *(deps)* Bump github.com/danielgtaylor/huma/v2 from 2.37.1 to 2.37.2 (#93)
- *(deps)* Bump github.com/oakwood-commons/kvx from 0.4.0 to 0.6.0 (#92)
- *(deps)* Bump goreleaser/goreleaser-action from 6 to 7 (#91)

### 🚜 Refactor

- [**breaking**] Remove all deprecated wrappers, aliases, and dead code (#117)
  - Delete `pkg/cmd/scafctl/resolver/` package; use `run resolver` instead
  - Delete forwarding wrappers in `pkg/cmd/scafctl/run/common.go`; callers use `pkg/solution/execute` directly
  - Delete `pkg/cmd/scafctl/secrets/validation.go`; callers use `pkg/secrets` and `pkg/secrets/crypto` directly
  - Delete type aliases from `cache/info.go`, `cache/clear.go`, `config/paths.go`, `get/resolver/refs.go`; callers use domain packages (`pkg/cache`, `pkg/paths`, `pkg/resolver/refs`)
  - Remove deprecated wrappers from `get/celfunction/celfunction.go`; callers use `pkg/provider/detail` directly
  - Delete dead `fallbackKeyring` struct from `pkg/secrets/keyring.go`
  - Rename `ProviderDescriptor.Deprecated` → `ProviderDescriptor.IsDeprecated` (JSON tag `deprecated` preserved for wire compatibility)

### 🧪 Testing

- Add solution integration tests for exclusive actions, forEach f… (#83)

## [0.4.0] - 2026-02-17

### 🚀 Features

- [**breaking**] Add object type coercion, single final-value coercion, and action streaming output (#77)

## [0.3.0] - 2026-02-16

### 🚀 Features

- *(build)* Add incremental build caching and remote artifact auto-caching (#47)
- *(soltesting)* Implement functional testing framework (#49)
- *(testing)* Add subprocess executor and functional integration test suite (#63)
- *(test)* Add test init command and watch mode for functional testing (#64)

### 🚜 Refactor

- *(cmd)* [**breaking**] Eliminate Root() package-level state for parallel exection (#48)

### ⚙️ Miscellaneous Tasks

- Prepare repository for public release (#65)
- Block fork PR workflows and update Go dependencies (#76)

## [0.2.0] - 2026-02-12

### 🚀 Features

- *(secrets)* [**breaking**] Add file-based keyring fallback with user-facing warnings (#35)
- *(provider)* Add directory builtin provider with documentation (#36)
- *(examples)* Add k8s-clusters forEach template rendering example (#37)
- *(logging)* [**breaking**] Overhaul CLI logging with quiet-by-default and user-controlled output (#38)
- *(exec)* [**breaking**] Add cross-platform embedded POSIX shell interpreter (#39)
- *(run)* [**breaking**] Add 'run resolver' command for debugging and inspection  (#40)
- *(paths)* [**breaking**] Use CLI conventions for XDG paths on macOS (#41)
- *(run resolver)* [**breaking**] Add graph, snapshot, dry-run, and skip-transform modes (#42)
- *(cli)* Add 'scafctl run provider' command for direct provider execution (#43)

### 🐛 Bug Fixes

- *(release)* Migrate from deprecated brews to homebrew_casks (#33)

### 📚 Documentation

- [**breaking**] Comprehensive Hugo site improvements and release pipeline enhancements (#34)

## [0.1.0] - 2026-02-10

### 🚀 Features

- *(solution)* Add cldctl get solution command (#1)
- *(cel)* Start cel implementation (#2)
- *(cel)* Implement custom CEL extension functions (#3)
- *(gotmpl)* Implement Go templating service with customizable options (#5)
- *(celexp)* [**breaking**] Add comprehensive testing, helpers, and validation features (#14)
- New design docs (#13)
- *(httpc)* [**breaking**] Add production-grade HTTP client enhancements (#16)
- *(design)* [**breaking**] Add provider capabilities and execution context support (#17)
- *(provider)* Add comprehensive provider system (#19)
- *(spec)* [**breaking**] Add shared spec package with ValueRef, type coercion, and snapshot support (#20)
- *(cmd)* [**breaking**] Integrate kvx library for interactive result viewing (#22)
- [**breaking**] Major release with catalog system, bundling, and public release prep (#24)

### 💼 Other

- *(deps)* Update kvx to latest version (#25)

### 📚 Documentation

- *(resolvers)* Clarify execution semantics and add implementation guidance (#18)
- *(internal)* [**breaking**] Consolidate internal docs into design/ (#32)

### ⚙️ Miscellaneous Tasks

- *(start)* Where it all starts
- *(actions)* Add github actions (#4)
- Update solution struct (#6)
- Update design docs (#7)
- Update docs (#8)
- *(docs)* Fix some issues copilot found (#10)
- Rename package for org rename (#11)
- Make copilot instructions less verbose (#12)
- *(docs)* Fix docs (#15)
- Update license (#21)

<!-- generated by git-cliff -->
