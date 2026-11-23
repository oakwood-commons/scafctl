# AGENTS.md

Canonical repo guidance for any author or tool working in scafctl. Copilot's
review contract lives separately in [`REVIEW.md`](REVIEW.md) (enforced on every
PR). On-demand skills live in `.github/skills/` -- load the one matching the
task before deep work in that area.

## Overview

Go-based CLI tool using CEL (Common Expression Language) for dynamic
configuration evaluation and template processing. Pre-production: breaking
changes are allowed (note them in the commit/PR); do not add backward
compatibility for removed behavior.

## Git

- **Conventional commits** (`feat:`, `fix:`, `docs:`, `chore:`, `!` for
  breaking).
- **Signing**: every commit is SSH-signed and carries a DCO `Signed-off-by:`
  trailer (`git commit -s -S`). Verify a signing key is loaded
  (`ssh-add -l`) before committing; if none is loaded, load it into the
  ssh-agent per your local machine setup rather than attempting a doomed
  commit.
- **Squash-merge repo**: every PR becomes one commit on `main` built from the
  **PR title and body** (per-commit messages are discarded). Do not split a
  branch into multiple commits for history's sake; put the effort into the PR
  title/body and keep unrelated concerns in separate PRs. The PR title must be
  a valid conventional-commit subject. **Whenever a new commit changes what the
  PR does, update the PR body in the same step** (`gh pr edit`) so the eventual
  squash commit stays accurate.
- Copilot reviews ready-for-review PRs on open (`review_on_push: false`).
  Wait for CI checks to pass before requesting a re-review, and only request a
  new Copilot round when a post-review fix makes substantive production changes
  (verify small surgical fixes locally); acknowledge nitpicks in a reply within
  the thread and resolve them -- resolving does not create a commit or trigger
  another review round.

## Build, test, lint

The project uses `task` (go-task/task). Always use `task lint` rather than raw
golangci-lint (it uses a pinned version).

~~~bash
# Build
go build -ldflags "-w -X main.BuildTime=$(date -u +%Y-%m-%dT%H:%M:%SZ) -X main.BuildVersion=dev -X main.Commit=$(git rev-parse HEAD)" -o dist/scafctl ./cmd/scafctl/scafctl.go

# Test
go test ./...                    # all tests (add -race for race detection)

# Lint
task lint                         # pinned golangci-lint
task lint:fix                    # lint + auto-fix
task lint:changed                 # only issues this branch introduced (vs merge base)
~~~

- Before every commit, run `task lint:changed` and confirm it reports **no new
  issues** -- plain `task lint` always exits non-zero due to pre-existing
  findings in untouched files; `lint:changed` is the gate.
- After any change, run `task test:e2e` (e2e is slow -- run it **once**, tee
  output to a file under `temp/`, and grep the file; never re-run just to read
  it differently). Targeted unit tests for iteration: `go test ./pkg/<pkg>/...`.
- Security scanning: `gosec ./...`.
- Scratch files go in the gitignored `temp/`; never commit them. Coverage
  profiles also go to `temp/` (`go test -coverprofile temp/cover.out ...`),
  never the repo root.

## Validate an issue before implementing it

An open issue is not a mandate. Confirm the bug/gap is real against the
current code (cite `file:line`) and worth doing before writing a solution.
Treat the reporter's "Proposed Fix"/"Files to Modify" as an unverified hint --
reporters may lack codebase access, so validate the actual best location and
approach. Aim for the best solution, not the easy one; take cues from
best-in-class CLIs (`git`, `docker`, `kubectl`, `gh`, `terraform`, `brew`).
The `planner` agent (`.opencode/agent/planner.md`) codifies this triage gate.

## Key patterns

- **CLI output**: `writer.FromContext(ctx)` -- never `fmt.Fprintf` directly.
  See `pkg/terminal/writer/`.
- **Data output**: `kvx.OutputOptions` for structured table/json/yaml/quiet
  output (see `pkg/terminal/kvx/`). Commands emitting an **array of objects**
  attach an interactive display schema via `kvx.WithOutputDisplaySchemaJSON`
  so `-i` renders a card/detail view. **Before debugging kvx list/`-i`
  rendering, load the `kvx-rendering` skill** (`.github/skills/kvx-rendering/`).
- **CLI layer**: commands are thin wiring (`pkg/cmd/scafctl/...`) -- no
  business logic.
- **HTTP client**: `pkg/httpc/` (see its README).
- **Paths**: xdg paths via `pkg/paths`.
- **Configuration**: `pkg/settings` for defaults; `pkg/config/` for app
  configuration.
- **CEL**: `celexp.EvaluateExpression()`; prefer the `expression` field over
  `expr`. When writing CEL extension functions, load the `cel-extensions`
  skill (`.github/skills/cel-extensions/`).

## MCP tools are the source of truth

scafctl ships an MCP server whose tools return facts about the solution spec,
functions, providers, and lint rules straight from the code. When authoring or
reasoning about solutions -- or writing docs/skills -- **consult these tools
instead of relying on (or hand-maintaining) inline catalogs**, which drift:

- **Spec / fields**: `get_solution_schema`, `explain_kind` (solution, resolver,
  action, workflow, spec, provider, schema, retry).
- **Functions**: `list_cel_functions`, `list_go_template_functions`.
- **Providers**: `list_providers`, `list_official_providers`,
  `get_provider_schema`.
- **Lint**: `list_lint_rules`, `explain_lint_rule`.
- **Concepts / runtime**: `explain_concepts` (including the `context` category)
  and `list_context_variables` for the injected-variable-per-phase matrix.
- **Validate before shipping**: `validate_expression`, `evaluate_cel`,
  `evaluate_go_template`, `lint_solution`, `extract_resolver_refs`.

**Caveat:** the MCP server reflects the **built binary** (`dist/scafctl`), not
your uncommitted working tree. If you just changed a function, provider, spec
field, or lint rule, rebuild and restart the server before querying it. When
in doubt, the source code is the ultimate authority.

## Business logic placement

Business logic never lives in CLI command packages (`pkg/cmd/scafctl/...`),
MCP handler files (`pkg/mcp/tools_*.go`), or API packages -- it goes in shared
domain packages (`pkg/...`). Endpoints, command handlers, and MCP tool
handlers validate input, call domain packages, and render output; nothing more.

## Embedder contract

scafctl is used as a **library by external CLIs**. Every new feature must be
consumable by embedders via `RootOptions` or domain package APIs.

- **No hardcoded "scafctl"**: use `settings.CliBinaryName` or
  `settings.Run.BinaryName` in context; guard against empty names by falling
  back to `CliBinaryName`.
- **`RootOptions` is the embedder API surface**: new CLI-level capabilities
  must be exposed as fields with sensible defaults -- config layers, hooks, and
  customization points must be wireable through it. New `RootOptions` fields
  need doc comments explaining the default behavior when unset.
- **Env var prefixes** come from `settings.SafeEnvPrefix()` -- never hardcode
  `SCAFCTL_`.
- **Test embedder scenarios**: include a test with a non-default binary name
  (e.g. `mycli`).
- Applies to every layer, not just commands: the API server is configured via
  `ServerOption` functions with composable middleware (embedders may add or
  replace it); MCP tool descriptions and the server name use the configured
  binary name, not a literal string.

## Auto-discovery and resolution

CLI commands pick the solution source with `load.Router.Select()` from
`pkg/solution/load` (load it with `Router.SelectionLoader()` so discovery is
not repeated), which:

1. Uses the explicit `-f` value if provided.
2. Uses the first positional argument when it is unambiguously a source (URL,
   registry reference, or versioned name `app@1.2.3`).
3. Auto-discovers solution files (`load.DiscoverLoader`) by searching folder
   prefixes (`scafctl/`, `.scafctl/`, `.`) combined with file names
   (`solution.yaml`, `solution.yml`, `scafctl.yaml`, `scafctl.yml`,
   `solution.json`, `scafctl.json`, `taskfile.yaml`, `taskfile.yml`). A
   taskfile candidate that parses as YAML but carries none of the recognized
   solution headers -- `apiVersion: scafctl.io/v1`, `kind: Solution`, or a
   non-null `spec` (e.g. a go-task taskfile) -- is skipped, with a note
   under `--verbose`; explicit `-f` keeps the strict error. Action
   file names (`actions.yaml`, `actions.yml`) are only searched in
   `DiscoveryModeAction` (used by `run action`).
4. With `ArgsAreNamesOrCatalog`, falls back to a bare positional name as a
   catalog reference when nothing is discovered.

Multi-match ambiguity handling uses risk levels:

- **Low-risk** (`DiscoveryRiskLow`): uses first match, emits a warning about
  other matches. Used by `run`, `lint`, `test`.
- **High-risk** (`DiscoveryRiskHigh`): returns `load.ErrAmbiguousSolution`,
  requiring `-f`. Used by `build`.

The MCP server's `list_solutions` tool uses `findSolutionFiles()`
(`DiscoverLoader.Discover()` over the working directory and client workspace
roots) and returns all discovered paths.

## Go conventions

- **Errors**: wrap with `fmt.Errorf("context: %w", err)`; never panic.
- **Struct tags**: always add JSON/YAML tags plus [Huma validation
  tags](https://huma.rocks/features/request-validation/#validation-tags):
  `doc` on all fields; `maxLength`, `example`, `pattern`, `patternDescription`
  on strings; `maximum`, `example` on integers; `maxItems` on arrays (no
  example); no `example` on objects/maps.
- **Special field types**: `expr` -> `Expression` (`pkg/celexp`); `tmpl` ->
  `GoTemplatingContent` (`pkg/gotmpl`).
- **Design principles**: accept interfaces, return structs; keep interfaces
  small (1-3 methods); define interfaces where they are used; constructor
  functions for dependency injection; functional options
  (`WithX(val) Option`); `context.Context` as first parameter; no
  package-level mutable state.
- **Secrets**: read from environment variables -- never hardcode.
- **Formatting**: gofmt and goimports are mandatory. In Copilot agent
  sessions, `.github/hooks/go-format.json` auto-formats edited `.go` files.
- **No magic values**: define constants or use settings for configuration
  values, including timeout defaults (`DefaultResolverTimeout`,
  `DefaultHTTPTimeout`) and conflict-strategy defaults (all exported constants
  with doc comments).
- **Go modules**: run `go mod tidy -v` after adding/removing dependencies;
  never edit `go.sum` manually; pin dependencies to versions, not branches;
  `go mod verify` for integrity.
- **Markdown**: ASCII only; use `--` instead of em dashes, straight quotes, and
  standard hyphens; use tilde fences when a code block contains backticks (4+
  backtick fences if tilde is unsuitable).

### Deprecating a spec field

scafctl has a reusable, struct-tag-driven deprecation mechanism -- no per-field
lint code. The worked example is the resolver/action `onError` enum superseded
by `continueOnError`:

1. Tag the OLD field deprecated and name its replacement:

   ~~~go
   OnError ErrorBehavior `json:"onError,omitempty" yaml:"onError,omitempty" deprecated:"true" deprecatedReplacement:"continueOnError" doc:"DEPRECATED: use continueOnError instead. ..."`
   ~~~

   - `deprecated:"true"` -- required; marks the field deprecated.
   - `deprecatedReplacement:"<newYamlKey>"` -- the replacement's YAML key;
     drives lint messaging and the schema `[DEPRECATED] (use <replacement>)`
     marker.
   - `deprecatedMessage:"<extra guidance>"` -- optional note appended to the
     lint warning.

2. Keep the old field fully functional (parse + translate) for at least one
   major version. Never delete a field in the same release you deprecate it.
   Provide a translation path and document the mapping in the new field's
   `doc` tag (worked example: `onError: continue` -> `continueOnError: true`,
   `onError: fail` -> `continueOnError: false`).

3. Add the NEW field. If it is a `*Condition`, do NOT add an `example` tag
   (Huma validates examples against the object schema and a scalar panics
   schema generation).

4. Plumbing is automatic: `pkg/schema/introspect.go` reads the tags into
   `FieldInfo`; `pkg/schema/format.go` renders the marker; Huma emits
   `deprecated: true`; the generic `deprecated-field` lint rule
   (`pkg/lint/deprecated.go`) emits a WARNING when the old field is set (add a
   traversal call there for new struct locations -- resolvers, workflow
   actions, and forEach are covered). If the fields are mutually exclusive,
   `deprecated-field-conflict` emits an ERROR when both are set (runtime: the
   new field wins).

5. Escalate by SEMVER, not wall-clock time: warn now -> error in the next major
   -> remove in the major after that. CI can opt into strict mode by treating
   lint warnings as failures.

6. Migrate first-party docs and examples to the new field, but keep ONE example
   of the deprecated field to demonstrate the warning, and keep a test asserting
   the old field still works and warns. Cross-reference the
   `deprecated-field` rule's explain text.

## Testing

- Standard `go test` with **table-driven tests**; `testify/assert` for
  assertions; mocks in `mock.go` files; run with `-race`.
- **Coverage targets**: every new or changed file has tests; every PR has
  **70%+ patch coverage** (enforced by Codecov). Domain packages (`pkg/...`)
  80%+; CLI commands (`pkg/cmd/...`) 65%+ package / 70%+ patch; critical
  business logic 90%+ package / 100% patch; generated code excluded. Never
  submit a new file with 0% coverage -- at minimum test the happy path and one
  error path. If a cobra RunE is hard to test directly, extract the core logic
  into a helper and test that.
- **Benchmarks** for new features and providers (`b.ReportAllocs()`,
  `b.ResetTimer()`, `for b.Loop()`).
- **Integration test scope**:

  | Feature type | Test location |
  | ------------- | --------------- |
  | CLI commands | `tests/integration/cli_test.go` |
  | Providers, resolvers, actions, plugins, solutions | `tests/integration/solutions/` |
  | REST endpoints, middleware, auth | `tests/integration/api_test.go` |

  API/server features are tested **only** in `tests/integration/api_test.go`,
  never in solution integration tests. New commands and new endpoints always
  land in their integration test file, and any new or changed provider,
  resolver, action, plugin, or solution gets a `tests/integration/solutions/`
  case.
- **Manual CLI verification**: automated tests can pass while the actual CLI
  is broken. When a command changes, build the binary (`go build ... -o
  dist/scafctl`) and exercise the changed commands against `./dist/scafctl`:
  happy path first, then error paths (bad input, missing args, invalid
  flags), with `--debug` to surface silent failures, `--verbose` on output
  paths, `--dry-run` where available, and `--help` to verify flag
  registration. Isolate state-writing commands with `XDG_DATA_HOME` /
  `XDG_CACHE_HOME` pointed at a temp dir, and verify writes with read
  commands afterwards.
- **kvx TUI assertions**: array commands cannot be visually verified by
  piping (a non-TTY shell only shows the flat text fallback). Use kvx's
  snapshot API -- see the `kvx-rendering` skill.

## Per-area rules

### CLI commands (`pkg/cmd/scafctl/**`)

- Commands are **thin wiring only**: they parse flags, call domain packages,
  and render output.
- Use `cobra.Command`; wire `settings.Run` parameters from flags.
- Use `kvx.OutputOptions` for structured data. Array-output commands attach a
  `go:embed`-ed `<cmd>_schema.json` display schema (see
  `pkg/cmd/scafctl/get/provider/provider.go` + `provider_schema.json` for the
  canonical pattern). Single-object or scalar output does not need a schema.
- Programmatic filtering is `-e`/`--expression` (CEL over the whole result,
  bound to `_`) and `-w`/`--where` (per-item CEL) -- already wired via
  `flags.ToKvxOutputOptions`; do not add a bespoke search/filter flag.
- Structured/quiet formats always emit a parseable document (`[]`, not
  `null`; non-nil empty slice); human "no results" text goes to stderr.
- Extract complex RunE logic into testable helpers.

### Diff subcommands (`pkg/cmd/scafctl/diff/**`)

Every `diff` verb renders through **one shared pipeline** (`writeDiffOutput` in
`output.go` + `pkg/diffreport`): a thin adapter `reportFrom<Kind>Diff(native)
*diffreport.Report` lives in the command's own file (build it with
`report.Add(...)` so the summary counts stay correct), rendered via
`writeDiffOutput(native, report)`. Default/`auto`/`text` -> shared human
report; `json`/`yaml` -> the native per-command struct (schema preserved);
everything else -> the flattened `report.Entries` via the shared
`diff_schema.json`. Output flags come from the standard
`flags.KvxOutputFlags` (`-o`/`-i`/`-e`/`-w`, wired with
`flags.AddKvxOutputFlagsToStruct`). Never hand-roll output formatting, add a
per-command `-o` switch, duplicate the shared schema/column hints, or add
bespoke styling; unit-test the adapter alongside its command.

### API server (`pkg/api/**`)

- chi for routing, Huma for OpenAPI-driven request/response handling with
  struct-tag validation; `ServerOption` functional options; structured errors
  via `pkg/api/errors.go`.

### MCP tools (`pkg/mcp/tools_*.go`)

- **Thin wrappers**: parse inputs, call domain packages, format results; never
  panic (return `mcp.NewToolResultText()` / `mcp.NewToolResultError()`).
- Register in `register*Tools()` methods on `*Server`; `mcp.NewTool()` with
  typed parameters, Huma-style descriptions and constraints, and
  `mcp.With*HintAnnotation()` metadata.
- Before adding a tool, confirm the capability is not already covered -- the
  server already exposes broad reference tools (`explain_concepts`,
  `explain_kind`, `get_solution_schema`, `list_providers` /
  `get_provider_schema`, `list_cel_functions` /
  `list_go_template_functions`, `list_lint_rules` / `explain_lint_rule`,
  `list_context_variables`). Prefer extending or pointing at those over a
  bespoke handler.

### Providers (`pkg/provider/**`)

- Stateless execution primitives implementing `Descriptor()` (name, version,
  API version, schema, capabilities) and `Execute(ctx, input)`.
- Package: `pkg/provider/builtin/<name>provider/` (e.g. `envprovider`;
  main file matches the provider name, e.g. `env.go`); register in
  `pkg/provider/builtin/builtin.go`.
- Define `ProviderName` / `Version` constants; use `schemahelper` for JSON
  schema generation; Huma validation tags on all Descriptor fields.
- Define a testable ops interface (e.g. `EnvOps`) injected via constructor;
  `DefaultXxxOps` for real implementations; mocks in `mock.go`.
- Always include benchmark tests (`*_benchmark_test.go`).
- Verify provider names and schemas against the live catalog with
  `list_providers` / `get_provider_schema` (rebuild first if you changed a
  provider), not a hand-maintained list.

### Settings & configuration (`pkg/settings`, `pkg/config`)

- `settings.CliBinaryName` fallback; `settings.SanitizeBinaryName()` to
  normalize; `settings.SafeEnvPrefix()` for env prefixes.
- xdg paths for all file locations: config `xdg.ConfigHome`/binary name; cache
  `xdg.CacheHome`/binary name; data `xdg.DataHome`/binary name.
- Functions producing paths, cache keys, or display strings accept a binary
  name parameter or read from context.
- `config.Config` for runtime application configuration; loading supports
  layered overrides (file, env, flags); always validate after loading.

### Docs & examples (`docs/`, `examples/`, `pkg/docs`, `pkg/examples`)

- Always create and update documentation, tutorials, and examples for new
  features, providers, and commands -- including MCP server tools when
  applicable.
- Reader-facing docs must stand alone -- website and checkout readers cannot
  invoke MCP tools, so keep committed reference material complete. Use the
  MCP tools (see the list above) to **verify or generate** that reference, not
  as the sole reader-facing source.
- **Publish hygiene**: this repo is public. Before creating/editing an issue,
  PR, or comment, or committing any file, check it against the
  `publish-hygiene` skill (`.github/skills/publish-hygiene/SKILL.md`).

## Review contract

Copilot code review follows [`REVIEW.md`](REVIEW.md) -- enforced on every PR
regardless of the author's tooling. Reviewer-side guidance (what is substantive
vs. a nit, what not to flag) lives there.
