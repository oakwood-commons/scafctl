---
description: "Diff subcommand rules for scafctl. New `diff` verbs reuse the shared diffreport + writeDiffOutput pipeline; never hand-roll output formatting. Use when adding or editing a diff subcommand."
applyTo: "pkg/cmd/scafctl/diff/**/*.go"
---

# Diff Subcommand Layer

The `diff` verb (`solution`, `bundle`, `snapshot`, ...) renders every subcommand
through **one shared output pipeline** so all diffs look and behave the same. A
new `diff` subcommand MUST reuse it rather than printing its own format.

## Required pattern

1. **Map the domain diff into the shared model.** Write a thin adapter
   `reportFrom<Kind>Diff(native) *diffreport.Report` that converts the command's
   native diff result into a `diffreport.Report` (a list of `diffreport.Entry`
   with `Group`/`Path`/`Kind`/`Before`/`After`/`Detail`). Use `report.Add(...)`
   so the summary counts stay correct. Keep the adapter in the **command's own
   file** (e.g. `reportFromSolutionDiff` lives in `solution.go`), next to its
   domain import -- not in `output.go`.
2. **Render through `writeDiffOutput`.** Call the shared helper in `output.go`
   with both the `native` struct and the `*diffreport.Report`. Do not add a
   per-command `-o` switch, custom renderer, or lipgloss styling.

`writeDiffOutput` already implements the house output contract:

- **default / `auto` / `text`** -> the shared human diff report
  (`diffreport.Render`).
- **`json` / `yaml`** -> the **native per-command struct** (serialized shape is
  preserved; do not swap it for the flattened entries).
- **everything else** (`table`/`list`/`tree`/`csv`/`toml`/`mermaid`/`-i`) -> the
  flattened `report.Entries`, driven by the shared `diff_schema.json` display
  schema and columnar table hints.

## Do / Don't

- DO expose output via the standard `flags.KvxOutputFlags` (`-o`/`-i`/`-e`/`-w`),
  wired with `flags.AddKvxOutputFlagsToStruct`.
- DO preserve the native struct for `json`/`yaml` so existing consumers keep
  their schema.
- DON'T duplicate `diff_schema.json`, the column hints, or the format-routing
  switch per command -- they are shared on purpose.
- DON'T introduce bespoke formats (e.g. a git-style `unified`); the shared
  report is the one human format.
- Unit-test the adapter alongside its command (`reportFrom<Kind>Diff` ->
  expected entries/summary). Format routing is covered centrally by
  `output_test.go`.

See `output.go` (`writeDiffOutput`, `isDefaultReportFormat`), the `pkg/diffreport`
package, and `solution.go`/`bundle.go`/`snapshot.go` for the canonical examples.
