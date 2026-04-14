---
description: "CLI command layer rules for scafctl. Commands are thin wiring -- no business logic. Use Writer for output, kvx for data, cobra for flags. Use when editing CLI command packages."
applyTo: "pkg/cmd/scafctl/**/*.go"
---

# CLI Command Layer

Commands are **thin wiring only** -- they parse flags, call domain packages, and render output.

## Rules

- **No business logic** -- delegate to packages in `pkg/`
- Use `writer.FromContext(ctx)` for all terminal output, never `fmt.Fprintf`
- Use `kvx.OutputOptions` for structured data (table/json/yaml/quiet)
- Use `cobra.Command` for command definition and flag binding
- Wire up `settings.Run` parameters from flags
- Always add new commands to CLI integration tests (`tests/integration/cli_test.go`)
- New or modified RunE functions must have test coverage (integration or unit) -- 0% patch coverage on CLI files is unacceptable
- Extract complex RunE logic into testable helper functions when direct cobra testing is impractical

## Embedder Awareness

scafctl is used as a library by external CLIs. Commands must not assume the binary is called "scafctl".

- Read the binary name from `settings.Run.BinaryName` (via context), not a hardcoded string
- Subcommand `Short`/`Long` descriptions must use the configured app name, not "scafctl"
- New `RootOptions` fields need doc comments explaining the default behavior when unset
- Environment variable prefixes come from `settings.SafeEnvPrefix()` -- never hardcode `SCAFCTL_`
- New CLI-level features (config layers, hooks, customization points) must be wirable through `RootOptions`
