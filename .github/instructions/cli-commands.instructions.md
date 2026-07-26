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
- **Array output ships an interactive display schema by default** -- when a command
  emits an **array of objects** via kvx, attach an interactive display schema with
  `kvx.WithOutputDisplaySchemaJSON` so `-i` renders a card list + detail pane instead
  of a raw KEY/VALUE table. Prefer a `go:embed`-ed `<cmd>_schema.json` (a JSON Schema
  whose root `type` is `array`, decorated with `x-kvx-list`/`x-kvx-detail` extensions).
  See `pkg/cmd/scafctl/get/provider/provider.go` + `provider_schema.json` for the
  canonical pattern. Note the two distinct slots: `kvx.WithOutputSchemaJSON` only tunes
  **table column hints**, while `kvx.WithOutputDisplaySchemaJSON` drives the **interactive
  TUI** -- an array command wants the display schema (and may add both). **Single-object
  or scalar output does not need a schema** (e.g. `auth token`) -- the author may still
  add one, but it is not required.
- Use `cobra.Command` for command definition and flag binding
- Wire up `settings.Run` parameters from flags
- Always add new commands to CLI integration tests (`tests/integration/cli_test.go`)
- New or modified RunE functions must have test coverage (integration or unit) -- 0% patch coverage on CLI files is unacceptable
- Extract complex RunE logic into testable helper functions when direct cobra testing is impractical

## kvx Rendering: how the TUI actually works (READ THIS before debugging list/`-i` output)

kvx (`github.com/oakwood-commons/kvx`) is a **key-value explorer**. Do NOT build a
custom search/filter engine or a hand-rolled table -- kvx already provides
responsive tables, an interactive card/detail TUI, search (`/`), filter (`f`),
and CEL filtering (`-e`/`--where`). Wire the data in correctly and you get all of
it for free. The rules below are the result of a painful debugging session; heed
them to avoid repeating it.

### The render paths (what runs when)

`OutputOptions.Write(data)` (from `flags.ToKvxOutputOptions`) branches on format
and TTY (see `pkg/terminal/kvx/output.go` `Write` -> `writeKvx`):

- **`-o json`/`-o yaml`/`-o quiet`/`-o csv`/`-o toml`** -> structured serialization.
  Empty results MUST still emit a parseable document (`[]`, not `null`; pass a
  **non-nil** empty slice). Human "no results" text goes to **stderr** only.
- **`auto`/`table`/`list`, NON-interactive, real TTY** -> `renderTable` ->
  `tui.RenderTable` (bordered, responsive: table when it fits, list when narrow --
  this is `FormatAuto`). The **display schema is also passed here**, so column
  hints/order apply.
- **`auto`/`table`, NON-interactive, NOT a TTY** (piped, tests, CI) -> falls back
  to a plain **text** table (`writeText`). This is the flat `[index]` + key/value
  block you see when you pipe output or run in a non-TTY shell. **It is NOT the
  card list and NOT what a user sees in a terminal.** Do not judge the TUI by
  piped output.
- **`-i` (interactive), real TTY** -> `runInteractive` -> `tui.Run` -> the full
  bubbletea TUI (card list + detail drill-down + search/filter).
- **`-i`, NOT a TTY** -> hard error: `interactive mode requires a terminal`.

Consequence: **you cannot see the real table or the `-i` TUI by piping into a
shell** (the AI's default shell is non-TTY). Piping only ever shows the flat text
fallback. Use snapshot rendering (below) to actually see the TUI.

### The `core.LoadObject` requirement (THE bug that wastes hours)

kvx's card/list view only activates when the data is a **homogeneous array of
objects** in kvx's *generic* form -- `[]interface{}` whose every element is
`map[string]interface{}` (see `internal/ui/list_view.go` `isHomogeneousObjectArray`
and `internal/ui/view_mode.go` `updateViewMode`). A typed struct slice
(`[]MyRow`) or even `[]map[string]any` is **NOT** that type, so the list view
does not trigger and kvx dumps the whole array as one raw JSON VALUE cell.

The real `View()` path fixes this for you: it calls **`core.LoadObject(data)`**
first, which converts typed structs into the generic `[]interface{}{map...}`
form. So in a real command you just pass your `[]MyRow` to `outputOpts.Write` and
it works. **But any snapshot/test harness that calls `tui.RenderSnapshot`/`tui.Run`
directly MUST call `core.LoadObject(data)` first** -- otherwise you get the JSON
blob and will wrongly conclude "the display schema is broken." It isn't; the data
shape is.

### Verifying the TUI without a terminal: `tui.RenderSnapshot`

To actually SEE the card list / detail / narrow-width behavior from a non-TTY
context (AI shell, unit test), render a snapshot:

~~~go
import (
    "github.com/oakwood-commons/kvx/pkg/core"
    "github.com/oakwood-commons/kvx/pkg/tui"
)

items, _ := mydomain.Scan("")            // your []Row
root, _ := core.LoadObject(items)        // REQUIRED: struct slice -> generic form
_, ds, _ := tui.ParseSchemaWithDisplay(mySchemaJSON) // parse the embedded schema
out := tui.RenderSnapshot(root, tui.Config{
    AppName:       "mytool get things",
    Width:         120, Height: 30,       // fixed size; try 60 wide to test narrow
    NoColor:       true,
    DisplaySchema: ds,
    StartKeys:     []string{"right"},     // simulate keypresses (drill-in = "right")
})
t.Log("\n" + out)                        // inspect the rendered frame
~~~

Notes:
- Validate the schema first: `tui.ParseSchemaWithDisplay(schema)` is the function
  kvx actually uses (via `resolveDisplaySchema`). A non-nil `*DisplaySchema` with
  a populated `List.TitleField` means `-i` will render the card list. (Do NOT use
  `tui.ParseDisplaySchema` -- it expects a different `displaySchema`-wrapped
  document and will error on the `x-kvx-*` root form.)
- Interactive drill-down from the list to the detail pane is triggered by
  **`right`** (not `enter`; `enter` opens search/filter). Startup-key simulation
  for drill-in is finicky in snapshots -- if it stays on the list, the detail
  config is still correct as long as `x-kvx-detail` parses.
- These snapshot harnesses are throwaway debugging tools; put them in `temp/` or a
  clearly-named `zz_*_test.go` you delete, or keep a real snapshot test if the
  view is worth locking down.

### Display schema field-name alignment (JSON == YAML == schema)

The row struct's field names surface in `-o json`, `-o yaml`, the table columns,
and the `x-kvx-*` schema. They must all match. **Add explicit `yaml:"..."` tags
matching the `json:"..."` tags** -- kvx's YAML path uses `yaml.Marshal`, which
lowercases keys by default (`displayName` -> `displayname`), producing output
inconsistent with JSON and the schema. (Canonical example: `get provider`'s
`Summary` struct carries matching json+yaml tags.)

### Cap EVERY visible column's width or the table silently collapses to KV

kvx's non-interactive table is responsive: if the computed column widths do not
fit the terminal, it **collapses to the flat per-row key/value list** (the same
`[index]` block as the non-TTY text fallback -- easy to misdiagnose as a TTY
bug). The trap: a visible column with **no `MaxWidth`** (commonly a
`description`) makes kvx assume it needs the full content width (60-90+ chars),
which blows the width budget and collapses the table even on a wide terminal.
This is exactly why a command can render fine as `get provider` (short columns)
but collapse as `get examples` (one uncapped long column).

Rules:
- Give **every** column in `ColumnOrder` a sensible `MaxWidth` in
  `ColumnHints`. Budget the sum (plus `#` + padding + borders, ~15 cols
  overhead) to fit a reasonable minimum terminal (target tabling at ~90-100
  cols; fewer/narrower columns table on narrower terminals).
- Use the flex column to fill wide terminals instead of leaving dead space:
  set `{MaxWidth: N, Flex: true}` on the one column that should absorb the
  remaining width (typically `description`). With `Flex: true`, `MaxWidth`
  becomes that column's **minimum** (not a cap) and the bordered table expands
  to the full terminal width; narrow terminals still truncate it with `...`.
  This gives "cap when space is tight, extend to fill when there's extra"
  without the uncapped-column collapse. (Do NOT confuse `Flex: true` with
  `MaxWidth: 0`/no cap -- the latter demands natural content width upfront and
  collapses the table; `Flex` cooperates with the width allocator.)
- Prefer **fewer columns** in the default table; move low-value fields (tags,
  long content) to `{Hidden: true}` and surface them in the `-i` detail view
  and/or `-o json`/`-o yaml` instead.
- Verify the threshold with a snapshot at several widths (render `tui.RenderTable`
  or run under `script -q /dev/null bash -c 'stty cols N; ./bin cmd'` for N in
  90/100/120) -- do NOT trust a single width, and remember `script`'s default pty
  is only 80 cols (narrower than most real terminals).

### Drilling into the `-i` detail view (and showing rich content there)

The interactive detail pane is driven by `x-kvx-detail.sections`; drill in from
the list with **`l`/`right`** (not `enter`). To let a user read a large field
(e.g. a full file's content) inside `-i` without a second command, add that field
to the row struct, add a `paragraph`-layout detail section for it in the schema,
and **hide it from the table** with `{Hidden: true}` (it still ships in
`-o json`/`-o yaml`). Example: `get examples` embeds each solution's YAML in a
`content` field, hidden from the table, rendered as a "Solution" detail section
so drilling into an example shows the actual solution.

### Columns vs. schema slots (two different knobs)- `kvx.WithOutputColumnOrder([...])` + `kvx.WithOutputColumnHints{...}` tune the
  **non-interactive table** (order, `MaxWidth`, `Priority`, `Hidden`). Use
  `{Hidden: true}` to keep an internal field (e.g. a fetch-handle `path`) out of
  the table while still emitting it in json/yaml and the `-i` detail pane.
- `kvx.WithOutputDisplaySchemaJSON(schema)` drives the **`-i` TUI** via
  `x-kvx-list` (card: `titleField`, `subtitleField`, `badgeFields`) and
  `x-kvx-detail` (drill-in sections). An array command typically sets BOTH.
- Do not force `-o list`/`WithLayout("list")` to "get the card view" -- `auto`
  already renders responsively and `-i` already renders cards. Forcing list mode
  is usually a mistake.

### Empty / not-found output discipline (applies to every array command)

- Structured/quiet: always write a parseable empty document (`[]`) to **stdout**;
  never emit human text on stdout in these modes.
- Human formats: put "no results"/guidance on **stderr** (`WarnStderrf`/
  `PlainStderrf`), leaving stdout clean.

### Search/CEL is built in -- don't reinvent it

Interactive `/` search and `f` filter come from kvx. Programmatic filtering is
`-e`/`--expression` (CEL over the whole result, bound to `_`) and `-w`/`--where`
(per-item CEL boolean). Wire the CEL provider (the shared `flags.ToKvxOutputOptions`
path already does) and these work; do not add a bespoke search/filter flag.

## Embedder Awareness

scafctl is used as a library by external CLIs. Commands must not assume the binary is called "scafctl".

- Read the binary name from `settings.Run.BinaryName` (via context), not a hardcoded string
- Subcommand `Short`/`Long` descriptions must use the configured app name, not "scafctl"
- New `RootOptions` fields need doc comments explaining the default behavior when unset
- Environment variable prefixes come from `settings.SafeEnvPrefix()` -- never hardcode `SCAFCTL_`
- New CLI-level features (config layers, hooks, customization points) must be wirable through `RootOptions`
