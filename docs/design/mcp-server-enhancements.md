---
title: MCP Server Enhancements & CLI Parity
weight: 102
---

# MCP Server Enhancements & CLI Parity

This document is the implementation plan for the next phase of MCP server improvements and the introduction of CLI parity for MCP-only tools. It builds on the existing [MCP Server design](./mcp-server.md) and [Implementation Guide](./mcp-server-implementation-guide.md).

## Table of Contents

- [Motivation](#motivation)
- [Part 1: CLI Parity — Exposing MCP Tools as CLI Commands](#part-1-cli-parity--exposing-mcp-tools-as-cli-commands)
  - [Architecture: Shared Library Pattern](#architecture-shared-library-pattern)
  - [Phase 1A: `scafctl eval` Command Group](#phase-1a-scafctl-eval-command-group)
  - [Phase 1B: `scafctl new` Command](#phase-1b-scafctl-new-command)
  - [Phase 1C: `scafctl lint` Subcommands](#phase-1c-scafctl-lint-subcommands)
  - [Phase 1E: `scafctl examples` Command Group](#phase-1e-scafctl-examples-command-group)
  - [Phase 1F: Enhanced `--dry-run` Output](#phase-1f-enhanced---dry-run-output)
- [Part 2: New MCP Tools](#part-2-new-mcp-tools)
  - [Phase 2A: `extract_resolver_refs`](#phase-2a-extract_resolver_refs)
  - [Phase 2B: `generate_test_scaffold`](#phase-2b-generate_test_scaffold)
  - [Phase 2C: `list_tests`](#phase-2c-list_tests)
  - [Phase 2D: `show_snapshot`](#phase-2d-show_snapshot)
  - [Phase 2E: `diff_snapshots`](#phase-2e-diff_snapshots)
  - [Phase 2F: `catalog_inspect`](#phase-2f-catalog_inspect)
  - [Phase 2G: `list_auth_handlers`](#phase-2g-list_auth_handlers)
  - [Phase 2H: `get_config_paths`](#phase-2h-get_config_paths)
  - [Phase 2I: `validate_expressions` (Batch)](#phase-2i-validate_expressions-batch)
- [Part 3: New MCP Prompts](#part-3-new-mcp-prompts)
  - [Phase 3A: `analyze_execution` Prompt](#phase-3a-analyze_execution-prompt)
  - [Phase 3B: `migrate_solution` Prompt](#phase-3b-migrate_solution-prompt)
  - [Phase 3C: `optimize_solution` Prompt](#phase-3c-optimize_solution-prompt)
- [Part 4: New MCP Resources](#part-4-new-mcp-resources)
  - [Phase 4A: `solution://{name}/tests` Resource](#phase-4a-solutionnametests-resource)
- [Part 5: Architecture & Quality Improvements](#part-5-architecture--quality-improvements)
  - [Phase 5A: Extract Shared Libraries from Inline MCP Code](#phase-5a-extract-shared-libraries-from-inline-mcp-code)
  - [Phase 5B: Structured Error Context](#phase-5b-structured-error-context)
  - [Phase 5C: Tool Latency Hints](#phase-5c-tool-latency-hints)
  - [Phase 5D: `get_version` Tool](#phase-5d-get_version-tool)
- [Implementation Order](#implementation-order)
- [Testing Strategy](#testing-strategy)

---

## Motivation

The MCP server currently exposes 26 tools, 9 prompts, and 5 resources. After analyzing the full codebase, two categories of improvements have been identified:

1. **MCP-only tools that should also be CLI commands.** Several tools were built exclusively for the MCP server (`evaluate_cel`, `scaffold_solution`, `diff_solution`, etc.) but provide value to developers working without AI/MCP. These should be exposed as first-class CLI commands via a shared library layer.

2. **CLI capabilities not yet exposed via MCP.** Existing CLI commands like `get resolver refs`, `test init`, `test list`, `snapshot show/diff`, `catalog inspect`, `auth list`, and `config paths` provide high-value read-only operations that AI agents currently cannot use.

Additionally, several cross-cutting improvements (batch validation, structured errors, new prompts for post-execution analysis) would improve the overall developer and agent experience.

---

## Part 1: CLI Parity — Exposing MCP Tools as CLI Commands

### Architecture: Shared Library Pattern

**Problem:** Several MCP tools contain logic inline in `pkg/mcp/tools_*.go` that has no shared library counterpart. This means CLI commands cannot reuse the logic, and any fixes/improvements must be duplicated.

**Solution:** Extract shared logic into dedicated library packages. Both the MCP handler and the CLI command become thin wrappers over the same library.

```
┌─────────────────┐     ┌──────────────────┐
│  CLI Command     │     │  MCP Tool Handler │
│  (cobra handler) │     │  (MCP handler)    │
└────────┬────────┘     └────────┬─────────┘
         │                       │
         ▼                       ▼
┌──────────────────────────────────────────┐
│         Shared Library Package            │
│  (pkg/scaffold/, pkg/soldiff/, etc.)      │
│  - Pure functions, no CLI/MCP deps        │
│  - Structured input/output types          │
│  - Full test coverage                     │
└──────────────────────────────────────────┘
```

**Packages to extract:**

| Current Location | Extract To | Reason |
|------------------|-----------|--------|
| `pkg/mcp/tools_scaffold.go` → `buildScaffoldYAML()` | `pkg/scaffold/` | Solution scaffolding logic (~150 lines inline) |
| `pkg/mcp/tools_diff.go` → diff logic | `pkg/soldiff/` | Solution structural comparison (~150 lines inline) |
| `pkg/mcp/tools_examples.go` → `scanExamples()` | `pkg/examples/` | Embedded examples via `go:embed` + scanning/categorization |

Tools that already call shared library code (`evaluate_cel` → `pkg/celexp`, `evaluate_go_template` → `pkg/gotmpl`, `list_lint_rules` → `pkg/cmd/scafctl/lint`) need no extraction — they just need CLI command wrappers.

---

### Phase 1A: `scafctl eval` Command Group

**New commands:** `scafctl eval cel` and `scafctl eval template`

These expose the MCP `evaluate_cel` and `evaluate_go_template` tools as CLI commands. Developers frequently need to test CEL expressions and Go templates in isolation when building solutions.

#### `scafctl eval cel`

```
scafctl eval cel --expression 'size(name) > 3' --var name=hello
scafctl eval cel --expression 'items.filter(i, i.active)' --data '{"items": [{"name": "a", "active": true}]}'
scafctl eval cel --expression 'has(config.timeout)' --file config.json
```

**File:** `pkg/cmd/scafctl/eval/cel.go`

**Inputs:**
| Flag | Type | Required | Description |
|------|------|----------|-------------|
| `--expression`, `-e` | string | Yes | CEL expression to evaluate |
| `--var`, `-v` | string (repeatable) | No | Variables as `key=value` pairs |
| `--data`, `-d` | string | No | JSON data context (inline) |
| `--file`, `-f` | string | No | JSON/YAML file for data context |
| `-o` | string | No | Output format: `json`, `yaml`, `table` (default: `table`) |

**Implementation:**
- Parse flags → build `map[string]any` data context
- Call `celexp.EvaluateExpression()` from `pkg/celexp/context.go`
- Format and write output via `kvx.OutputOptions`

**Shared code:** `pkg/celexp` — already exists, no extraction needed.

#### `scafctl eval template`

```
scafctl eval template --template '{{ .name | upper }}' --var name=hello
scafctl eval template --template-file deploy.tmpl --data '{"env": "prod"}'
scafctl eval template --template '{{ ._.config.host }}' --file resolvers.json
```

**File:** `pkg/cmd/scafctl/eval/template.go`

**Inputs:**
| Flag | Type | Required | Description |
|------|------|----------|-------------|
| `--template`, `-t` | string | No* | Go template string (inline) |
| `--template-file` | string | No* | Go template file path |
| `--var`, `-v` | string (repeatable) | No | Variables as `key=value` pairs |
| `--data`, `-d` | string | No | JSON data context (inline) |
| `--file`, `-f` | string | No | JSON/YAML file for data context |
| `--show-refs` | bool | No | Also output referenced resolver fields |
| `-o` | string | No | Output format: `json`, `yaml`, `table` (default: `table`) |

*One of `--template` or `--template-file` is required.

**Implementation:**
- Parse flags → build data context
- Call `gotmpl.NewService(nil).Execute()` from `pkg/gotmpl`
- Optionally call `gotmpl.Service.GetReferences()` for `--show-refs`
- Format and write output via `kvx.OutputOptions`

**Shared code:** `pkg/gotmpl` — already exists, no extraction needed.

#### `scafctl eval validate`

```
scafctl eval validate --expression 'size(name) > 3' --type cel
scafctl eval validate --expression '{{ .name }}' --type go-template
```

**File:** `pkg/cmd/scafctl/eval/validate.go`

**Inputs:**
| Flag | Type | Required | Description |
|------|------|----------|-------------|
| `--expression`, `-e` | string | Yes | Expression to validate |
| `--type` | string | Yes | Expression type: `cel` or `go-template` |
| `-o` | string | No | Output format |

**Implementation:**
- CEL: use `cel.NewEnv().Parse()` to syntax-check
- Go template: use `template.New().Parse()` + `gotmpl.Service.GetReferences()`
- Report: valid/invalid, error details, referenced variables

---

### Phase 1B: `scafctl new` Command

**New command:** `scafctl new solution`

Exposes the MCP `scaffold_solution` tool as a CLI command. This is the highest-impact missing CLI command — creating new solutions currently requires copying examples or writing YAML from scratch.

```
scafctl new solution --name my-deploy --description "Deploy to Kubernetes" --features parameters,resolvers,actions --providers exec,http
scafctl new solution --name simple-transform --description "Text transformer" > solution.yaml
```

**File:** `pkg/cmd/scafctl/new/solution.go`

**Inputs:**
| Flag | Type | Required | Description |
|------|------|----------|-------------|
| `--name`, `-n` | string | Yes | Solution name (lowercase, hyphens, 3-60 chars) |
| `--description`, `-d` | string | Yes | Brief description |
| `--version` | string | No | Semver version (default: `1.0.0`) |
| `--features` | string (csv) | No | Features: `parameters`, `resolvers`, `actions`, `transforms`, `validation`, `tests`, `composition` |
| `--providers` | string (csv) | No | Include provider-specific examples |
| `--output`, `-o` | string | No | Output file path (default: stdout) |

**Prerequisite — Extract shared library:**

Create `pkg/scaffold/scaffold.go` (note: the shared library package remains `pkg/scaffold/` since it describes the operation; only the CLI command is `new`):

```go
package scaffold

type Options struct {
    Name        string
    Description string
    Version     string
    Features    map[string]bool
    Providers   []string
}

type Result struct {
    YAML     string
    Filename string
    Features []string
}

func Solution(opts Options) *Result { ... }
```

Move `buildScaffoldYAML()` and helpers from `pkg/mcp/tools_scaffold.go` into this package. Update the MCP handler to call `scaffold.Solution()`.

---

### Phase 1C: `scafctl lint` Subcommands

**New commands:** `scafctl lint rules` and `scafctl lint explain`

```
scafctl lint rules                           # list all rules (table)
scafctl lint rules --severity error          # filter by severity
scafctl lint rules --category naming         # filter by category
scafctl lint rules -o json                   # JSON output for tooling

scafctl lint explain missing-description     # explain a specific rule
scafctl lint explain unknown-provider-input  # detailed fix guidance
```

**Files:**
- `pkg/cmd/scafctl/lint/rules_cmd.go` — `scafctl lint rules`
- `pkg/cmd/scafctl/lint/explain_cmd.go` — `scafctl lint explain`

**Shared code:** `pkg/cmd/scafctl/lint/rules.go` already exports `ListRules()` and `GetRule()` — no extraction needed. CLI commands are thin wrappers.

**`scafctl lint rules` outputs:**
| Column | Description |
|--------|-------------|
| Rule | Rule name (e.g., `missing-description`) |
| Severity | `error`, `warning`, `info` |
| Category | `naming`, `structure`, `dependency`, etc. |
| Description | One-line summary |

**`scafctl lint explain` outputs:**
| Field | Description |
|-------|-------------|
| Rule | Rule name |
| Severity | `error` / `warning` / `info` |
| Category | Rule category |
| Description | Full description |
| Why It Matters | Impact explanation |
| How to Fix | Step-by-step fix instructions |
| Example | Correct YAML example |

---

### Phase 1E: `scafctl examples` Command Group

**New commands:** `scafctl examples list` and `scafctl examples get`

```
scafctl examples list                        # list all examples
scafctl examples list --category solutions   # filter by category
scafctl examples list -o json                # JSON output

scafctl examples get solutions/email-notifier/solution.yaml   # print example
scafctl examples get providers/http-resolver.yaml > my.yaml   # save to file
```

**Files:**
- `pkg/cmd/scafctl/examples/list.go`
- `pkg/cmd/scafctl/examples/get.go`

**Prerequisite — Extract shared library with `go:embed`:**

The current MCP implementation in `pkg/mcp/tools_examples.go` uses `runtime.Caller(0)` and directory-walking heuristics to locate the `examples/` directory at runtime. This is fragile — it only works when running from a repo checkout or development build. For `scafctl examples` to work as a distributed CLI command (installed binary, containers, CI), the examples must be embedded into the binary.

**Approach:** Use Go's `go:embed` directive, which is already an established pattern in this codebase (see `pkg/cmd/scafctl/config/init.go` for config templates). The `examples/` directory is 520KB / 87 files — trivially small for embedding.

**Why `go:embed` over alternatives:**

| Approach | Verdict | Reason |
|----------|---------|--------|
| `go:embed` | **Recommended** | Works offline, version-locked to binary, zero dependencies, established pattern in codebase |
| Fetch from GitHub API | Rejected | Requires network, rate limits, auth for private repos, version mismatch risk |
| Bundle as OCI catalog artifact | Rejected | Requires catalog configuration just to see examples — too much friction |
| Filesystem-only (current) | Rejected | Only works in development; non-starter for distributed binary |

**Implementation:**

Since `go:embed` can only access files in the embedding package's directory or subdirectories, the example YAML files need to be accessible from `pkg/examples/`. Two options:

1. **Copy at build time** (recommended): Add a build step to `taskfile.yaml` that copies `examples/` → `pkg/examples/files/` before `go build`. Add `pkg/examples/files/` to `.gitignore`.
2. **Symlink**: Create `pkg/examples/files` as a symlink to `../../examples`. NOTE: `go:embed` does **not** follow symlinks, so this only works if the build tool resolves symlinks first.

Create `pkg/examples/examples.go`:

```go
package examples

import "embed"

//go:embed files/*
var EmbeddedExamples embed.FS

type Example struct {
    Path        string `json:"path"`
    Category    string `json:"category"`
    Description string `json:"description"`
    Size        int64  `json:"size"`
}

// Scan walks the embedded examples filesystem and returns matching examples.
// If category is empty, returns all examples.
func Scan(category string) ([]Example, error) { ... }

// Read returns the contents of an embedded example file.
func Read(path string) (string, error) { ... }

// Categories returns the list of available example categories.
func Categories() []string { ... }
```

Key changes from the current MCP implementation:
- **No `FindExamplesDir()`** — the directory concept is replaced by `embed.FS`
- **No `runtime.Caller()` hacks** — examples are always available in the binary
- `Scan()` and `Read()` operate on `EmbeddedExamples` instead of `os.ReadDir`/`os.ReadFile`
- Examples are version-locked — `scafctl v0.15.0` always shows v0.15.0 examples

Move `scanExamples()` logic and description mapping from `pkg/mcp/tools_examples.go` into this package, adapted to use `fs.WalkDir` on `embed.FS`. Update both MCP handler and CLI command to use it.

**Build integration (`taskfile.yaml`):**

```yaml
tasks:
  embed:examples:
    desc: Copy examples into pkg/examples/files for go:embed
    cmds:
      - rm -rf pkg/examples/files
      - cp -r examples pkg/examples/files
    sources:
      - examples/**/*
    generates:
      - pkg/examples/files/**/*

  build:
    deps: [embed:examples]
    cmds:
      - go build -ldflags "..." -o dist/scafctl ./cmd/scafctl/scafctl.go
```

---

### Phase 1F: Enhanced `--dry-run` Output

**Enhancement:** Replace the current lightweight `--dry-run` output on `scafctl run solution` with the full rich report that the MCP `dry_run_solution` / `preview_resolvers` / `preview_action` tools produce.

Currently `--dry-run` shows basic action names, phases, and providers. The MCP tools show much richer data: mock provider behaviors, materialized inputs, deferred inputs, forEach metadata, retry config, and dependency graphs. There is no reason these should be separate — `--dry-run` *is* preview.

**Breaking change:** The `--dry-run` output format changes from a simple summary to a full structured report. This is intentional.

```
scafctl run solution -f ./solution.yaml --dry-run -r env=prod          # full dry-run report (table)
scafctl run solution -f ./solution.yaml --dry-run -r env=prod -o json  # machine-readable
```

**Enhanced `--dry-run` output includes:**

| Section | Current | Enhanced |
|---------|---------|----------|
| Actions | Name, phase, provider | + materialized inputs, deferred inputs, mock provider behavior, forEach metadata, retry config |
| Resolvers | Not shown | Full resolver materialization: values, phases, dependencies, transform chains |
| Dependencies | Not shown | Dependency graph (resolver + action) |
| Parameters | Not shown | Resolved parameter values |
| Validation | Not shown | Expression validation results, missing `dependsOn` warnings |

**Implementation:**

1. Extract the rich dry-run/preview logic from the MCP tools (`preview_resolvers`, `preview_action`, `dry_run_solution`) into a shared library package (e.g., `pkg/dryrun/`)
2. Define structured output types:

```go
package dryrun

type Report struct {
    Solution    string            `json:"solution"`
    Version     string            `json:"version"`
    Parameters  map[string]any    `json:"parameters"`
    Resolvers   []ResolverPreview `json:"resolvers"`
    Actions     []ActionPreview   `json:"actions"`
    Graph       GraphInfo         `json:"graph"`
    Validation  []ValidationIssue `json:"validation,omitempty"`
}

type ResolverPreview struct {
    Name         string   `json:"name"`
    Provider     string   `json:"provider"`
    Phase        int      `json:"phase"`
    DependsOn    []string `json:"dependsOn,omitempty"`
    MaterializedInputs map[string]any `json:"materializedInputs,omitempty"`
    DeferredInputs     []string       `json:"deferredInputs,omitempty"`
    Transforms   []string `json:"transforms,omitempty"`
}

type ActionPreview struct {
    Name         string         `json:"name"`
    Phase        int            `json:"phase"`
    Provider     string         `json:"provider"`
    MockBehavior string         `json:"mockBehavior"`
    Inputs       map[string]any `json:"inputs,omitempty"`
    DeferredInputs []string     `json:"deferredInputs,omitempty"`
    ForEach      *ForEachMeta   `json:"forEach,omitempty"`
    Retry        *RetryConfig   `json:"retry,omitempty"`
    DependsOn    []string       `json:"dependsOn,omitempty"`
}

func Generate(ctx context.Context, sol *solution.Solution, params map[string]any) (*Report, error) { ... }
```

3. Update `scafctl run solution --dry-run` to call `dryrun.Generate()` and render via `kvx.OutputOptions`
4. Update the MCP tools (`preview_resolvers`, `preview_action`, `dry_run_solution`) to call the same shared library
5. Support `-o table|json|yaml` on the CLI output

**Shared code:** `pkg/dryrun/` — new package extracted from MCP inline logic.

**MCP tool parity:** The three MCP tools remain as separate tools (agents benefit from granularity), but they all call into `pkg/dryrun/` internally. The CLI `--dry-run` produces the equivalent of calling all three.

---

## Part 2: New MCP Tools

### Phase 2A: `extract_resolver_refs`

**Priority: Critical** — Most impactful missing tool for AI-assisted solution authoring.

**Description:** Takes a Go template or CEL expression (inline text or file path) and returns all resolver references (`_.resolverName` patterns) found in it. This is essential for AI agents to correctly populate `dependsOn` fields.

**File:** `pkg/mcp/tools_refs.go`

**Tool Definition:**
```go
mcp.NewTool("extract_resolver_refs",
    mcp.WithDescription("Extract resolver references (_.resolverName patterns) from Go templates or CEL expressions. Returns a list of referenced resolver names, which should be used to populate the 'dependsOn' field. Accepts inline text or a file path."),
    mcp.WithTitleAnnotation("Extract Resolver References"),
    mcp.WithReadOnlyHintAnnotation(true),
    mcp.WithDestructiveHintAnnotation(false),
    mcp.WithIdempotentHintAnnotation(true),
    mcp.WithOpenWorldHintAnnotation(false),
    mcp.WithString("text",
        mcp.Description("Inline Go template or CEL expression text to analyze"),
    ),
    mcp.WithString("file",
        mcp.Description("Path to a template file to analyze"),
    ),
    mcp.WithString("type",
        mcp.Description("Expression type: 'go-template' (default) or 'cel'"),
    ),
)
```

**Response Schema:**
```json
{
  "source": "inline" | "file",
  "sourceType": "go-template" | "cel",
  "references": ["config", "environment", "credentials"],
  "count": 3,
  "details": [
    {"resolver": "config", "fields": ["host", "port"]},
    {"resolver": "environment", "fields": ["name"]},
    {"resolver": "credentials", "fields": []}
  ]
}
```

**Shared code:** `gotmpl.GetGoTemplateReferences()` and `celexp.Expression.GetUnderscoreVariables()` already exist. The MCP handler extracts resolver names from full paths (e.g., `_.config.host` → resolver `config`, field `host`).

**CLI counterpart:** Already exists as `scafctl get resolver refs`. No new CLI command needed.

**Update `serverInstructions`:** Add guidance:
```
When creating or editing Go templates (tmpl:) or CEL expressions (expr:) that reference resolvers,
call extract_resolver_refs to determine which resolver names are referenced, then use those
names in the dependsOn field.
```

---

### Phase 2B: `generate_test_scaffold`

**Priority: High** — Completes the test authoring workflow.

**Description:** Analyzes a solution's resolvers and workflow, then generates a starter test YAML that covers the basic cases. Pairs with the existing `add_tests` prompt.

**File:** `pkg/mcp/tools_test.go`

**Tool Definition:**
```go
mcp.NewTool("generate_test_scaffold",
    mcp.WithDescription("Analyze a solution and generate a starter functional test scaffold. Examines resolvers (types, parameters, transforms) and workflow actions (providers, dependencies) to produce test cases with appropriate assertions. The generated YAML can be added to the solution's spec.testing section."),
    mcp.WithTitleAnnotation("Generate Test Scaffold"),
    mcp.WithReadOnlyHintAnnotation(true),
    mcp.WithDestructiveHintAnnotation(false),
    mcp.WithIdempotentHintAnnotation(true),
    mcp.WithOpenWorldHintAnnotation(true),
    mcp.WithString("path",
        mcp.Required(),
        mcp.Description("Path to the solution file to generate tests for"),
    ),
)
```

**Response Schema:**
```json
{
  "yaml": "spec:\n  testing:\n    cases:\n      ...",
  "testCount": 5,
  "coverage": {
    "resolversWithTests": ["config", "environment"],
    "resolversWithoutTests": [],
    "actionsWithTests": ["deploy"],
    "actionsWithoutTests": []
  },
  "nextSteps": [
    "Review and customize the test assertions",
    "Add edge case tests for error conditions",
    "Run run_solution_tests to verify tests pass"
  ]
}
```

**Shared code:** `soltesting.Scaffold()` and `soltesting.ScaffoldToYAML()` from `pkg/solution/soltesting/scaffold.go`.

**CLI counterpart:** Already exists as `scafctl test init`. No new CLI command needed.

---

### Phase 2C: `list_tests`

**Priority: High** — Enables test discovery before execution.

**Description:** Discovers and lists all functional tests defined in solution files without executing them. Returns test names, tags, commands, and skip status.

**File:** `pkg/mcp/tools_test.go` (same file as `generate_test_scaffold`)

**Tool Definition:**
```go
mcp.NewTool("list_tests",
    mcp.WithDescription("Discover and list functional tests defined in solutions without executing them. Returns test names, tags, commands, expected behavior, and skip status. Use this to understand what tests exist before calling run_solution_tests."),
    mcp.WithTitleAnnotation("List Tests"),
    mcp.WithReadOnlyHintAnnotation(true),
    mcp.WithDestructiveHintAnnotation(false),
    mcp.WithIdempotentHintAnnotation(true),
    mcp.WithOpenWorldHintAnnotation(true),
    mcp.WithString("path",
        mcp.Description("Path to a solution file or directory containing solutions with tests"),
    ),
    mcp.WithString("filter",
        mcp.Description("Filter test names by glob pattern (e.g., 'smoke-*')"),
    ),
    mcp.WithString("tag",
        mcp.Description("Filter tests by tag (e.g., 'smoke', 'validation')"),
    ),
    mcp.WithBoolean("include_builtins",
        mcp.Description("Include built-in tests (lint, parse). Default: false"),
    ),
)
```

**Response Schema:**
```json
{
  "solutions": [
    {
      "solution": "my-solution",
      "file": "./my-solution.yaml",
      "tests": [
        {
          "name": "basic-resolve",
          "description": "Test basic resolver output",
          "command": ["render", "solution"],
          "tags": ["smoke"],
          "skip": false,
          "assertionCount": 3
        }
      ]
    }
  ],
  "totalTests": 5,
  "totalSolutions": 1
}
```

**Shared code:** `soltesting.DiscoverSolutions()` and `soltesting.FilterTests()` from `pkg/solution/soltesting/discovery.go`.

**CLI counterpart:** Already exists as `scafctl test list`. No new CLI command needed.

---

### Phase 2D: `show_snapshot`

**Priority: High** — Enables post-execution analysis.

**Description:** Loads a resolver execution snapshot file and returns structured summary data including solution metadata, timing, status, and per-resolver values/errors.

**File:** `pkg/mcp/tools_snapshot.go`

**Tool Definition:**
```go
mcp.NewTool("show_snapshot",
    mcp.WithDescription("Load and display a resolver execution snapshot. Shows solution metadata, execution timing, status (success/failure), parameter values, and per-resolver results (value, status, duration, provider). Use this to inspect past execution results for debugging."),
    mcp.WithTitleAnnotation("Show Snapshot"),
    mcp.WithReadOnlyHintAnnotation(true),
    mcp.WithDestructiveHintAnnotation(false),
    mcp.WithIdempotentHintAnnotation(true),
    mcp.WithOpenWorldHintAnnotation(false),
    mcp.WithString("path",
        mcp.Required(),
        mcp.Description("Path to the snapshot JSON file"),
    ),
    mcp.WithString("format",
        mcp.Description("Output detail level: 'summary' (default), 'resolvers' (include per-resolver data), 'full' (everything including raw values)"),
    ),
)
```

**Response Schema:**
```json
{
  "solution": "my-solution",
  "version": "1.0.0",
  "timestamp": "2026-02-20T15:30:00Z",
  "scafctlVersion": "0.15.0",
  "duration": "2.3s",
  "status": "success",
  "resolverCount": { "total": 10, "success": 9, "failed": 1, "skipped": 0 },
  "phases": 3,
  "parameters": { "env": "prod", "region": "us-east1" },
  "resolvers": [
    {
      "name": "config",
      "status": "success",
      "value": { "host": "api.example.com" },
      "duration": "150ms",
      "provider": "http",
      "phase": 1
    }
  ]
}
```

**Shared code:** `resolver.LoadSnapshot()` from `pkg/resolver/snapshot.go`.

**CLI counterpart:** Already exists as `scafctl snapshot show`. No new CLI command needed.

---

### Phase 2E: `diff_snapshots`

**Priority: High** — Enables regression detection.

**Description:** Compares two snapshot files and returns value changes, status changes, additions, and removals between runs.

**File:** `pkg/mcp/tools_snapshot.go` (same file as `show_snapshot`)

**Tool Definition:**
```go
mcp.NewTool("diff_snapshots",
    mcp.WithDescription("Compare two resolver execution snapshots and show differences. Identifies resolvers with changed values, status changes (success→failure), additions, and removals. Useful for detecting regressions between runs or understanding the impact of solution changes."),
    mcp.WithTitleAnnotation("Diff Snapshots"),
    mcp.WithReadOnlyHintAnnotation(true),
    mcp.WithDestructiveHintAnnotation(false),
    mcp.WithIdempotentHintAnnotation(true),
    mcp.WithOpenWorldHintAnnotation(false),
    mcp.WithString("before",
        mcp.Required(),
        mcp.Description("Path to the baseline (before) snapshot file"),
    ),
    mcp.WithString("after",
        mcp.Required(),
        mcp.Description("Path to the comparison (after) snapshot file"),
    ),
    mcp.WithBoolean("ignore_unchanged",
        mcp.Description("Omit unchanged resolvers from the response. Default: true"),
    ),
)
```

**Response Schema:**
```json
{
  "before": { "solution": "my-solution", "timestamp": "...", "status": "success" },
  "after":  { "solution": "my-solution", "timestamp": "...", "status": "success" },
  "changes": {
    "added": [{ "name": "new-resolver", "value": "..." }],
    "removed": [{ "name": "old-resolver", "value": "..." }],
    "changed": [
      {
        "name": "config",
        "fields": [
          { "field": "value.host", "before": "old.example.com", "after": "new.example.com" }
        ]
      }
    ],
    "statusChanges": [
      { "name": "api-call", "before": "success", "after": "failed" }
    ]
  },
  "summary": { "added": 1, "removed": 1, "changed": 1, "statusChanges": 1, "unchanged": 7 }
}
```

**Shared code:** `resolver.DiffSnapshotsWithOptions()` and formatting functions from `pkg/resolver/diff.go`.

**CLI counterpart:** Already exists as `scafctl snapshot diff`. No new CLI command needed.

---

### Phase 2F: `catalog_inspect`

**Priority: Medium** — Adds drill-down capability to catalog.

**Description:** Returns detailed metadata about a specific catalog artifact (name, version, kind, digest, size, creation time, annotations).

**File:** `pkg/mcp/tools_catalog.go` (add to existing file)

**Tool Definition:**
```go
mcp.NewTool("catalog_inspect",
    mcp.WithDescription("Show detailed metadata about a specific catalog artifact. Returns name, version, kind, digest, size, creation timestamp, catalog source, and annotations. Use catalog_list first to find artifact references."),
    mcp.WithTitleAnnotation("Catalog Inspect"),
    mcp.WithReadOnlyHintAnnotation(true),
    mcp.WithDestructiveHintAnnotation(false),
    mcp.WithIdempotentHintAnnotation(true),
    mcp.WithOpenWorldHintAnnotation(false),
    mcp.WithString("reference",
        mcp.Required(),
        mcp.Description("Artifact reference (e.g., 'my-solution:1.0.0' or 'my-solution@sha256:abc123')"),
    ),
)
```

**Shared code:** `catalog.ParseReference()`, `catalog.NewLocalCatalog()`, `localCatalog.Resolve()` from `pkg/catalog/`.

**CLI counterpart:** Already exists as `scafctl catalog inspect`. No new CLI command needed.

---

### Phase 2G: `list_auth_handlers`

**Priority: Medium** — Supplements existing `auth_status`.

**Description:** Lists all registered auth handlers with their supported authentication flows and capabilities. Complements `auth_status` which shows current credential *status* but not what mechanisms are *available*.

**File:** `pkg/mcp/tools_auth.go` (add to existing file)

**Tool Definition:**
```go
mcp.NewTool("list_auth_handlers",
    mcp.WithDescription("List all registered authentication handlers with their supported flows (device-code, client-credentials, etc.) and capabilities. Use this to understand what authentication mechanisms are available when configuring solutions that need specific auth providers. Use auth_status to check the current credential status for a specific handler."),
    mcp.WithTitleAnnotation("List Auth Handlers"),
    mcp.WithReadOnlyHintAnnotation(true),
    mcp.WithDestructiveHintAnnotation(false),
    mcp.WithIdempotentHintAnnotation(true),
    mcp.WithOpenWorldHintAnnotation(false),
)
```

**Shared code:** Auth registry `ListHandlers()` or iterating handler names via the auth registry from `pkg/auth/`.

**CLI counterpart:** Already exists as `scafctl auth list`. No new CLI command needed.

---

### Phase 2H: `get_config_paths`

**Priority: Medium** — Environment awareness for agents.

**Description:** Returns all XDG-compliant paths used by scafctl (config, data, cache, state, secrets, catalogs, plugins, logs, history).

**File:** `pkg/mcp/tools_config.go` (add to existing file)

**Tool Definition:**
```go
mcp.NewTool("get_config_paths",
    mcp.WithDescription("Return all file system paths used by scafctl, resolved for the current platform. Shows where config, data, cache, catalogs, plugins, secrets, and logs are stored. Useful for locating snapshots, cached artifacts, or diagnosing path-related issues."),
    mcp.WithTitleAnnotation("Get Config Paths"),
    mcp.WithReadOnlyHintAnnotation(true),
    mcp.WithDestructiveHintAnnotation(false),
    mcp.WithIdempotentHintAnnotation(true),
    mcp.WithOpenWorldHintAnnotation(false),
)
```

**Response Schema:**
```json
{
  "paths": [
    { "name": "config", "path": "/Users/user/.config/scafctl", "description": "Configuration files", "xdgVariable": "XDG_CONFIG_HOME" },
    { "name": "data", "path": "/Users/user/.local/share/scafctl", "description": "Application data" },
    { "name": "cache", "path": "/Users/user/.cache/scafctl", "description": "HTTP and build cache" },
    { "name": "catalogs", "path": "/Users/user/.local/share/scafctl/catalogs", "description": "Local artifact catalogs" },
    { "name": "snapshots", "path": "/Users/user/.local/share/scafctl/snapshots", "description": "Resolver execution snapshots" }
  ]
}
```

**Shared code:** Path resolution from `pkg/paths/`.

**CLI counterpart:** Already exists as `scafctl config paths`. No new CLI command needed.

---

### Phase 2I: `validate_expressions` (Batch)

**Priority: Medium** — Reduces round-trips for bulk validation.

**Description:** Validates multiple CEL/Go-template expressions in a single call. A solution file can have dozens of expressions across resolvers and actions — validating them one by one via `validate_expression` is expensive.

**File:** `pkg/mcp/tools_template.go` (add to existing file)

**Tool Definition:**
```go
mcp.NewTool("validate_expressions",
    mcp.WithDescription("Validate multiple CEL expressions and/or Go templates in a single call. Returns per-expression validation results. More efficient than calling validate_expression repeatedly when checking all expressions in a solution."),
    mcp.WithTitleAnnotation("Validate Expressions (Batch)"),
    mcp.WithReadOnlyHintAnnotation(true),
    mcp.WithDestructiveHintAnnotation(false),
    mcp.WithIdempotentHintAnnotation(true),
    mcp.WithOpenWorldHintAnnotation(false),
    mcp.WithArray("expressions",
        mcp.Required(),
        mcp.Description("Array of {expression, type} objects. Type is 'cel' or 'go-template'."),
    ),
)
```

**Input:**
```json
{
  "expressions": [
    { "expression": "size(name) > 3", "type": "cel" },
    { "expression": "{{ .name | upper }}", "type": "go-template" },
    { "expression": "items.filter(i, i.active", "type": "cel" }
  ]
}
```

**Response Schema:**
```json
{
  "results": [
    { "index": 0, "expression": "size(name) > 3", "type": "cel", "valid": true },
    { "index": 1, "expression": "{{ .name | upper }}", "type": "go-template", "valid": true, "references": ["name"] },
    { "index": 2, "expression": "items.filter(i, i.active", "type": "cel", "valid": false, "error": "missing closing parenthesis" }
  ],
  "summary": { "total": 3, "valid": 2, "invalid": 1 }
}
```

**CLI counterpart:** Could be exposed as `scafctl eval validate --batch` or accept multiple `--expression` flags.

---

## Part 3: New MCP Prompts

### Phase 3A: `analyze_execution` Prompt

**Priority: High** — Fills the post-execution analysis gap.

All existing prompts are pre-execution (create, debug, update, prepare). This prompt guides the agent through post-execution analysis when something went wrong.

**Arguments:**
| Argument | Required | Description |
|----------|----------|-------------|
| `snapshot_path` | Yes | Path to the snapshot from the failed/unexpected run |
| `previous_snapshot` | No | Path to a known-good snapshot for comparison |
| `problem` | No | Description of what went wrong |

**Prompt content guide:**
1. Call `show_snapshot` with the provided path to inspect results
2. Identify failed resolvers (status != success)
3. If `previous_snapshot` provided, call `diff_snapshots` to find what changed
4. For failed resolvers, check provider configuration (`get_provider_schema`)
5. Cross-reference with `auth_status` if HTTP/cloud providers are involved
6. Suggest specific fixes based on error patterns
7. If fixes are applied, suggest re-running and comparing with `diff_snapshots`

---

### Phase 3B: `migrate_solution` Prompt

**Priority: Medium** — Guides structural refactoring.

Different from `update_solution` (targeted changes). This prompt handles larger structural refactoring: adding composition, migrating from inline to file-based templates, splitting a monolith solution, upgrading patterns, etc.

**Arguments:**
| Argument | Required | Description |
|----------|----------|-------------|
| `path` | Yes | Path to the solution to migrate |
| `migration` | Yes | Type: `add-composition`, `extract-templates`, `split-solution`, `add-tests`, `upgrade-patterns` |
| `target_dir` | No | Target directory for split/extracted files |

**Prompt content guide (varies by migration type):**
1. Call `inspect_solution` to understand current structure
2. Call `lint_solution` to establish baseline (zero errors before migration)
3. Plan the migration based on type:
   - `add-composition`: identify natural groupings → create partial files → add compose references
   - `extract-templates`: find inline templates → extract to files → update references
   - `split-solution`: identify independent sections → create child solutions → parent references
4. Execute migration changes
5. Call `lint_solution` to verify no regressions
6. Call `preview_resolvers` to verify outputs unchanged
7. Call `diff_solution` on before/after to confirm only structural changes
8. If tests exist, call `run_solution_tests` to verify

---

### Phase 3C: `optimize_solution` Prompt

**Priority: Medium** — Performance and quality analysis.

**Arguments:**
| Argument | Required | Description |
|----------|----------|-------------|
| `path` | Yes | Path to the solution to optimize |
| `focus` | No | Focus area: `performance`, `readability`, `testing`, `all` (default: `all`) |

**Prompt content guide:**
1. Call `inspect_solution` to understand the solution structure
2. Call `render_solution` with `graph_type=resolver` to analyze the dependency graph
3. Call `render_solution` with `graph_type=action-deps` to analyze action dependencies
4. Call `lint_solution` to identify quality issues
5. Analyze for optimization opportunities:
   - **Performance:** Find serial chains that could run in parallel (independent resolvers without `dependsOn`), identify unnecessary `dependsOn` constraints, spot duplicate resolver work
   - **Readability:** Long resolver names, missing descriptions, complex nested expressions, inconsistent naming
   - **Testing:** Missing test coverage (compare resolvers/actions vs test cases), missing edge case tests
6. Call `extract_resolver_refs` on complex templates to verify `dependsOn` accuracy
7. Present prioritized recommendations with specific YAML changes
8. After applying changes, re-lint and re-preview to verify no regressions

---

## Part 4: New MCP Resources

### Phase 4A: `solution://{name}/tests` Resource

**Priority: Low** — Convenience resource.

Add a resource template that returns structured test case data for a solution.

```go
testsTemplate := mcp.NewResourceTemplate(
    "solution://{name}/tests",
    "Solution Tests",
    mcp.WithTemplateDescription("Returns the functional test cases defined in a solution as structured JSON. Includes test names, commands, assertions, tags, and configuration."),
    mcp.WithTemplateMIMEType("application/json"),
)
```

**File:** `pkg/mcp/resources.go` (add to existing file)

This complements the `list_tests` tool by providing a resource-oriented access pattern. Some MCP clients prefer reading resources over calling tools for data retrieval.

---

## Part 5: Architecture & Quality Improvements

### Phase 5A: Extract Shared Libraries from Inline MCP Code

**Priority: High** — Prerequisite for CLI parity (Part 1).

| Package | Source | Description |
|---------|--------|-------------|
| `pkg/scaffold/` | `pkg/mcp/tools_scaffold.go` | `buildScaffoldYAML()`, `featureKeys()`, provider template generation |
| `pkg/soldiff/` | `pkg/mcp/tools_diff.go` | Solution structural comparison, change classification |
| `pkg/examples/` | `pkg/mcp/tools_examples.go` | `scanExamples()`, category/description handling, embedded via `go:embed` |

Each extraction should:
1. Create the new package with exported types and functions
2. Add comprehensive unit tests in the new package
3. Update the MCP handler to call the shared code
4. Verify MCP tests still pass
5. Then build the CLI wrapper on top

**Special note for `pkg/examples/`:** This extraction also replaces the fragile `findExamplesDir()` filesystem-walking approach with `go:embed`. The `examples/` directory (520KB, 87 files) is copied into `pkg/examples/files/` at build time and embedded into the binary via `//go:embed files/*`. This follows the existing pattern in `pkg/cmd/scafctl/config/init.go` which embeds config templates. See [Phase 1E](#phase-1e-scafctl-examples-command-group) for full details.

---

### Phase 5B: Structured Error Context

**Priority: Medium** — Improves AI agent error handling.

Currently tool errors are flat strings via `mcp.NewToolResultError(err.Error())`. Add structured error context where applicable:

```go
type ToolError struct {
    Code       string   `json:"code"`                 // e.g., "SOLUTION_NOT_FOUND", "INVALID_EXPRESSION"
    Message    string   `json:"message"`              // Human-readable message
    Field      string   `json:"field,omitempty"`       // Affected field (if applicable)
    Suggestion string   `json:"suggestion,omitempty"`  // Suggested fix
    Related    []string `json:"related,omitempty"`     // Related tool calls that might help
}
```

Create a helper:
```go
func newStructuredError(code, message string, opts ...ErrorOption) *mcp.CallToolResult {
    // Build ToolError, marshal to JSON, return as mcp.NewToolResultError()
}
```

Apply to high-frequency error paths in:
- `lint_solution` — include rule name, severity, fix suggestion
- `preview_resolvers` — include resolver name, provider, error type
- `dry_run_solution` — include phase, resolver/action name, error details
- `evaluate_cel` / `evaluate_go_template` — include parse position, expected syntax

---

### Phase 5C: Tool Latency Hints

**Priority: Low** — Helps AI agents optimize tool selection.

Add latency categories to tool descriptions to help AI agents choose efficient tool call sequences:

| Category | Tools | Description |
|----------|-------|-------------|
| `⚡ instant` | `list_lint_rules`, `explain_lint_rule`, `explain_kind`, `get_solution_schema`, `get_config`, `get_config_paths`, `list_auth_handlers`, `validate_expression` | In-memory lookups, no I/O |
| `🔄 fast` | `lint_solution`, `inspect_solution`, `list_solutions`, `list_providers`, `get_provider_schema`, `catalog_list`, `catalog_inspect`, `diff_solution`, `list_tests`, `list_examples`, `get_example`, `scaffold_solution`, `extract_resolver_refs`, `generate_test_scaffold` | File I/O, parsing, no network |
| `🌐 variable` | `preview_resolvers`, `preview_action`, `dry_run_solution`, `run_solution_tests`, `evaluate_cel`, `render_solution` | May involve network calls depending on providers |

Update `serverInstructions` with a section:
```
Tool Latency Guide:
  - Instant (in-memory): list_lint_rules, explain_lint_rule, explain_kind, get_solution_schema, 
    get_config, get_config_paths, list_auth_handlers, validate_expression(s)
  - Fast (file I/O): lint_solution, inspect_solution, list_*, catalog_*, diff_solution, 
    scaffold_solution, extract_resolver_refs, generate_test_scaffold
  - Variable (may use network): preview_resolvers, preview_action, dry_run_solution, 
    run_solution_tests, evaluate_cel, render_solution
  Prefer instant/fast tools for initial analysis; use variable-latency tools for validation.
```

---

### Phase 5D: `get_version` Tool

**Priority: Low** — Environmental context.

The MCP `initialize` response includes the server version, but agents cannot query it after initialization. A lightweight tool helps agents give version-appropriate guidance.

```go
mcp.NewTool("get_version",
    mcp.WithDescription("Return the scafctl version, build time, and commit hash."),
    mcp.WithTitleAnnotation("Get Version"),
    mcp.WithReadOnlyHintAnnotation(true),
    mcp.WithDestructiveHintAnnotation(false),
    mcp.WithIdempotentHintAnnotation(true),
    mcp.WithOpenWorldHintAnnotation(false),
)
```

**Response:**
```json
{
  "version": "0.15.0",
  "commit": "abc123def",
  "buildTime": "2026-02-20T15:30:00Z"
}
```

---

## Implementation Order

Recommended execution order balancing impact, dependencies, and effort:

### Sprint 1: Shared Library Extraction (Prerequisite)
1. **Phase 5A:** Extract `pkg/scaffold/`, `pkg/soldiff/`, `pkg/examples/` from MCP inline code
2. Update MCP handlers to use extracted packages
3. Verify all existing MCP tests pass

### Sprint 2: Highest-Impact MCP Tools
4. **Phase 2A:** `extract_resolver_refs` tool
5. **Phase 2B:** `generate_test_scaffold` tool
6. **Phase 2C:** `list_tests` tool
7. Update `serverInstructions` with new tool guidance

### Sprint 3: CLI Parity — Core Commands
8. **Phase 1A:** `scafctl eval cel`, `scafctl eval template`, `scafctl eval validate`
9. **Phase 1B:** `scafctl new solution`
10. **Phase 1C:** `scafctl lint rules`, `scafctl lint explain`

### Sprint 4: Snapshot & Analysis Tools
11. **Phase 2D:** `show_snapshot` tool
12. **Phase 2E:** `diff_snapshots` tool
13. **Phase 3A:** `analyze_execution` prompt

### Sprint 5: CLI Parity — Additional Commands
14. **Phase 1E:** `scafctl examples list`, `scafctl examples get`
15. **Phase 1F:** Enhanced `--dry-run` output (full rich report replaces lightweight summary)

### Sprint 6: Supplementary MCP Enhancements
16. **Phase 2F:** `catalog_inspect` tool
17. **Phase 2G:** `list_auth_handlers` tool
18. **Phase 2H:** `get_config_paths` tool
19. **Phase 2I:** `validate_expressions` batch tool

### Sprint 7: Prompts, Resources & Polish
20. **Phase 3B:** `migrate_solution` prompt
21. **Phase 3C:** `optimize_solution` prompt
22. **Phase 4A:** `solution://{name}/tests` resource
23. **Phase 5B:** Structured error context
24. **Phase 5C:** Tool latency hints in server instructions
25. **Phase 5D:** `get_version` tool

---

## Testing Strategy

### Unit Tests

Every new tool, prompt, and CLI command must have unit tests following existing patterns:

- **MCP tools:** `pkg/mcp/tools_*_test.go` — test handler functions with mock server context
- **CLI commands:** `pkg/cmd/scafctl/<cmd>/*_test.go` — test with `testify/assert`, mock IOStreams
- **Shared libraries:** `pkg/<package>/*_test.go` — pure function tests with table-driven cases
- **Prompts:** `pkg/mcp/prompts_test.go` — verify prompt arguments and message content

### Integration Tests

- Add new CLI commands to `tests/integration/cli_test.go`
- Create solution integration tests in `tests/integration/solutions/` for new tool workflows
- For MCP tools that wrap existing CLI capabilities, verify output parity between CLI and MCP

### Test Coverage Targets

| Component | Minimum Coverage |
|-----------|-----------------|
| Shared library packages (`pkg/scaffold/`, `pkg/soldiff/`, `pkg/examples/`) | 90% |
| MCP tool handlers | 85% |
| CLI command handlers | 80% |
| MCP prompts | 70% (content verification) |

---

## Summary

| Category | Count | Details |
|----------|-------|---------|
| New MCP Tools | 9 | `extract_resolver_refs`, `generate_test_scaffold`, `list_tests`, `show_snapshot`, `diff_snapshots`, `catalog_inspect`, `list_auth_handlers`, `get_config_paths`, `validate_expressions`, `list_go_template_functions` |
| New MCP Prompts | 3 | `analyze_execution`, `migrate_solution`, `optimize_solution` |
| New MCP Resources | 1 | `solution://{name}/tests` |
| New CLI Commands | 9 | `eval cel`, `eval template`, `eval validate`, `new solution`, `lint rules`, `lint explain`, `examples list`, `examples get`, enhanced `--dry-run` |
| Library Extractions | 3 | `pkg/scaffold/`, `pkg/soldiff/`, `pkg/examples/` |
| Architecture Improvements | 4 | Structured errors, latency hints, `get_version`, shared library pattern |
| Estimated Sprints | 7 | ~1-2 weeks each depending on team capacity |

---

## Part 6: MCP Protocol Feature Adoption

This section covers enhancements that leverage additional MCP protocol capabilities from the mcp-go SDK v0.44.0. These features improve the user experience, provide richer metadata, and enable more interactive workflows.

### 6A: Progress Notifications for Long-Running Tools

**File**: `pkg/mcp/progress.go`

Tools that take significant time now send real-time progress notifications to the MCP client using `notifications/progress`. Implemented via a `progressReporter` helper that:
- Extracts the progress token from `request.Params.Meta.ProgressToken`
- Falls back to logger output if no token is provided
- Reports progress with step count, total, and human-readable messages

**Tools with progress**: `preview_resolvers`, `run_solution_tests`, `dry_run_solution`

### 6B: Tool Icons

**File**: `pkg/mcp/icons.go`

All 36 tools, 12 prompts, and 6 resources now have SVG icons using data URIs. Icons are categorized by function (solution, provider, CEL, template, etc.) and use distinct colors for visual differentiation. Supported by MCP clients like VS Code, Claude Desktop, and Cursor.

### 6C: Output Schemas

**File**: `pkg/mcp/output_schemas.go`

11 tools now declare their output JSON Schema via `mcp.WithRawOutputSchema()`, enabling MCP clients to validate and render structured output:
- `list_solutions`, `inspect_solution`, `lint_solution`, `render_solution`, `preview_resolvers`
- `get_version`, `evaluate_cel`, `auth_status`, `get_config`, `get_config_paths`, `dry_run_solution`

### 6D: ResourceLink in Tool Results

Tools that return structured data about solutions or providers now include `ResourceLink` items pointing to related MCP resources:
- `inspect_solution` → links to `solution://{path}`, `solution://{path}/schema`, `solution://{path}/graph`
- `list_providers` → link to `provider://reference`
- `preview_resolvers` → links to `solution://{path}`, `solution://{path}/graph`

### 6E: Content Annotations (Audience/Priority)

The `get_run_command` tool now annotates its output content with audience hints:
- Command text → `assistant` audience (high priority) for LLM consumption
- Explanation text → `user` audience for human display

### 6F: Resource Annotations

All resource templates and resources have audience and priority annotations:
- Solution YAML content → both `user` and `assistant`, priority 0.7
- Provider reference → `assistant` only, priority 0.8
- Schema/graph/tests → `assistant`, priority 0.5-0.6

### 6G: Deferred Tool Loading

Rarely-used tools are marked with `mcp.WithDeferLoading(true)` to reduce initial load time:
- `show_snapshot`, `diff_snapshots`, `diff_solution`, `extract_resolver_refs`, `explain_lint_rule`

### 6H: Elicitation for Interactive Workflows

**File**: `pkg/mcp/capabilities.go`

The `preview_resolvers` tool now uses MCP elicitation to prompt users for missing parameter values. When a solution has `parameter` provider resolvers without provided values, the tool sends an `ElicitationRequest` with a JSON Schema describing the required fields.

### 6I: Roots Discovery for Workspace Awareness

**File**: `pkg/mcp/capabilities.go`

The `list_solutions` tool uses MCP roots discovery to find solution files in workspace directories when the catalog returns empty results. This enables workspace-aware behavior in MCP clients.

### 6J: Resource Recovery and Task Capabilities

**File**: `pkg/mcp/server.go`

- `server.WithResourceRecovery()` — automatic panic recovery in resource handlers
- `server.WithTaskCapabilities(true, true, true)` — enables async task listing, cancellation, and tool-call tasks

### 6K: MCP Log Streaming

**File**: `pkg/mcp/progress.go`

A `sendLog()` helper enables real-time log streaming to connected MCP clients via `notifications/message`, supporting log levels (info, warning, error) and named loggers.

