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
