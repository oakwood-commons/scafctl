# kvx Integration Plan for scafctl

## Executive Summary

This document outlines the implementation plan for integrating the **kvx** data viewer library into scafctl. The kvx library provides both a non-interactive table view and an interactive TUI for exploring structured data like JSON and YAML, allowing users to navigate, search, and inspect complex nested structures.

### Integration Goals

1. **POC (Phase 1)**: Integrate kvx into `scafctl run solution` command
2. **Generalization (Phase 2)**: Create a shared output handler for kvx support across all commands
3. **Full Rollout (Phase 3)**: Add kvx output to all data-outputting commands

### Key Design Decisions

1. **Default Output**: Commands that currently default to JSON/YAML will default to **kvx non-interactive table mode** when output is a terminal
2. **Interactive Flag**: `-i/--interactive` flag launches the full TUI for deep exploration
3. **Expression Flag**: `-e/--expression` flag accepts CEL expressions to filter/transform output data
4. **CEL Integration**: kvx will use scafctl's custom CEL functions (not just kvx built-ins)
5. **Backward Compatibility**: `-o json` and `-o yaml` flags continue to work for piping/scripting

---

## Collaboration with kvx Developer

### First Third-Party Integration

This integration marks the **first time a third-party application will integrate with kvx**. The kvx developer is actively collaborating on this effort and has committed to making changes to kvx as needed to ensure a seamless integration experience.

### Integration Approach

Given the collaborative relationship with the kvx developer:

1. **No Workarounds in scafctl**: If we encounter issues, limitations, or friction points during integration, we will **not** create workarounds in scafctl. Instead, these will be raised as issues for the kvx developer to address directly.

2. **Issue Tracking**: Any integration challenges should be documented and raised for review. This includes:
   - API ergonomics issues
   - Missing features or capabilities
   - Documentation gaps
   - Performance concerns
   - Error handling improvements
   - Breaking changes needed

3. **Recommendations Welcome**: The kvx developer is actively seeking feedback and recommendations for improvements. We should document:
   - Enhancement suggestions based on our integration experience
   - API design feedback
   - Feature requests that would benefit embedders
   - Best practices we discover that could be documented upstream

### Issue Log

| Issue | Description | Status | kvx Issue # |
|-------|-------------|--------|-------------|
| Module path | Update from `module kvx` to `module github.com/oakwood-commons/kvx`. | ✅ Resolved | - |
| Replace directive leak | kvx had a `replace` directive for `charm.land/bubbles/v2` that didn't resolve for consumers. | ✅ Resolved (v0.1.0) | - |
| Charmbracelet dependency conflict | kvx brings in `charmbracelet/x/cellbuf@v0.0.13` which requires `charmbracelet/x/ansi@v0.8.0`, but other kvx deps require `ansi@v0.11.x`. The cellbuf package fails to compile with newer ansi API (missing `SlowBlink`, changed `Italic()` signature, etc.). kvx needs to update its charmbracelet dependencies to use compatible versions. | ✅ Resolved | - |
| Custom CEL provider not used in TUI | When using `tui.SetExpressionProvider()` with a custom CEL provider (created via `tui.NewCELExpressionProvider()`), the TUI does not use it for expression evaluation. Direct provider calls work (e.g., `provider.Evaluate("guid.new()", data)` returns a UUID), but typing `guid.new()` in the TUI gives "undeclared reference to 'guid'". The global `exprProvider` in `internal/ui/expr_provider.go` is set correctly before `tui.Run()` is called. Investigation suggests the TUI may not be using the global provider for evaluation. | ✅ Resolved (v0.1.2) | - |
| Ultraviolet API mismatch | When upgrading kvx, Go's dependency resolution can pull in a newer `charmbracelet/ultraviolet` version with breaking API changes (`*uv.Buffer` → `*uv.RenderBuffer`). This causes compilation failures in `bubbletea/v2@v2.0.0-rc.2` which was built against the older API. **Root cause**: Pre-release versions (rc.2, commit hashes) without stable releases make dependency coordination fragile. **Workaround**: scafctl pins `ultraviolet@v0.0.0-20251116181749-377898bcce38`. **Recommendation**: kvx should pin ultraviolet to a specific working version in `go.mod`, or upgrade to a bubbletea version compatible with newer ultraviolet. | 🔵 Open | - |
| _TBD_ | _Issues will be logged here as they arise during implementation_ | - | - |

### Recommendations Log

| Recommendation | Description | Status | kvx Issue # |
|----------------|-------------|--------|-------------|
| Scalar value output | `RenderTable` should detect scalar values (string, number, bool) and output them directly without table formatting. Currently consumers must implement this check themselves. See workaround in `pkg/terminal/kvx/viewer.go`. | 🔵 Open | - |
| _TBD_ | _Recommendations will be logged here as they arise during implementation_ | - | - |

---

## Library Analysis

### kvx Capabilities

| Feature | Description |
|---------|-------------|
| **Non-Interactive Table** | Bordered table output for quick data viewing (default) |
| **Interactive TUI** | Navigate data with arrow keys, expand/collapse nested structures |
| **CEL Expression Support** | Filter and evaluate data using CEL expressions |
| **Multi-format Input** | Auto-detects JSON, YAML, NDJSON, CSV |
| **Output Formats** | table, json, yaml, raw, csv |
| **Search** | Live search across keys and values |
| **Theming** | Built-in themes (midnight, dark, warm, cool) |
| **Embedding API** | Clean Go API for embedding in other applications |

### kvx Public API

```go
// Core loading functions
core.LoadFile(path string) (interface{}, error)
core.LoadRoot(input string) (interface{}, error)
core.LoadRootBytes(data []byte) (interface{}, error)
core.LoadObject(value interface{}) (interface{}, error)  // ← Best for scafctl

// Core Engine for non-interactive use
core.New(opts ...Option) (*Engine, error)
engine.Evaluate(expr string, root interface{}) (interface{}, error)
engine.RenderTable(node interface{}, noColor bool, keyColWidth, valueColWidth int) string

// TUI functions (interactive mode)
tui.DefaultConfig() Config
tui.Run(root interface{}, cfg Config, opts ...tea.ProgramOption) error
tui.WithIO(in io.Reader, out io.Writer) []tea.ProgramOption

// Expression customization
tui.SetExpressionProvider(p ExpressionProvider)
tui.NewCELExpressionProvider(env *cel.Env, hints map[string]string) ExpressionProvider
```

### Prerequisites

The kvx library maintainer will update the module path from `module kvx` to `module github.com/oakwood-commons/kvx`. This plan assumes this fix is complete before implementation begins.

---

## Phase 1: POC - `scafctl run solution` Integration

### Objective
Replace the default JSON output of `scafctl run solution` with kvx's table view, add `-i` for interactive mode, and add `-e` for CEL expression filtering.

### New Command Behavior

```bash
# Default: kvx table output (when terminal)
scafctl run solution -f ./solution.yaml

# Interactive TUI mode
scafctl run solution -f ./solution.yaml -i

# Filter with CEL expression
scafctl run solution -f ./solution.yaml -e '_.database'

# Combined: filter then explore interactively  
scafctl run solution -f ./solution.yaml -e '_.database' -i

# Traditional JSON/YAML (for piping/scripting)
scafctl run solution -f ./solution.yaml -o json
scafctl run solution -f ./solution.yaml -o yaml | yq .database
```

### Implementation Steps

#### 1.1 Add kvx Dependency

```bash
go get github.com/oakwood-commons/kvx@latest
```

#### 1.2 Create kvx Output Package

Create a new package to handle kvx integration with scafctl's CEL environment:

**File: `pkg/terminal/kvx/viewer.go`**

```go
package kvx

import (
	"fmt"
	"io"
	"os"

	"github.com/google/cel-go/cel"
	"github.com/oakwood-commons/kvx/pkg/core"
	"github.com/oakwood-commons/kvx/pkg/tui"
	"github.com/oakwood-commons/scafctl/pkg/celexp"
)

// ViewerOptions configures the kvx viewer
type ViewerOptions struct {
	AppName     string    // Application name shown in TUI (default: "scafctl")
	Width       int       // Terminal width (0 = auto-detect)
	Height      int       // Terminal height (0 = auto-detect)
	NoColor     bool      // Disable colors
	In          io.Reader // Input reader (default: os.Stdin)
	Out         io.Writer // Output writer (default: os.Stdout)
	Expression  string    // CEL expression to filter/transform data
	Interactive bool      // Launch interactive TUI
	HelpTitle   string    // Custom help title
	HelpLines   []string  // Custom help text lines
}

// DefaultViewerOptions returns sensible defaults for scafctl
func DefaultViewerOptions() *ViewerOptions {
	return &ViewerOptions{
		AppName: "scafctl",
		In:      os.Stdin,
		Out:     os.Stdout,
	}
}

// Option is a functional option for configuring the viewer
type Option func(*ViewerOptions)

// WithAppName sets the application name
func WithAppName(name string) Option {
	return func(o *ViewerOptions) { o.AppName = name }
}

// WithNoColor disables colors
func WithNoColor(noColor bool) Option {
	return func(o *ViewerOptions) { o.NoColor = noColor }
}

// WithIO sets custom input/output
func WithIO(in io.Reader, out io.Writer) Option {
	return func(o *ViewerOptions) {
		o.In = in
		o.Out = out
	}
}

// WithExpression sets a CEL expression to filter/transform data
func WithExpression(expr string) Option {
	return func(o *ViewerOptions) { o.Expression = expr }
}

// WithInteractive enables interactive TUI mode
func WithInteractive(interactive bool) Option {
	return func(o *ViewerOptions) { o.Interactive = interactive }
}

// WithHelp sets custom help text for interactive mode
func WithHelp(title string, lines []string) Option {
	return func(o *ViewerOptions) {
		o.HelpTitle = title
		o.HelpLines = lines
	}
}

// View displays data using kvx (table or interactive based on options)
func View(data any, opts ...Option) error {
	options := DefaultViewerOptions()
	for _, opt := range opts {
		opt(options)
	}

	// Load data using kvx's LoadObject
	root, err := core.LoadObject(data)
	if err != nil {
		return fmt.Errorf("failed to load data: %w", err)
	}

	// Apply CEL expression filter if provided
	if options.Expression != "" {
		root, err = evaluateWithScafctlCEL(options.Expression, root)
		if err != nil {
			return fmt.Errorf("expression evaluation failed: %w", err)
		}
	}

	// Interactive mode: launch TUI
	if options.Interactive {
		if !IsTerminal(options.Out) {
			return fmt.Errorf("interactive mode requires a terminal; use -o json or -o yaml for piped output")
		}
		return runInteractive(root, options)
	}

	// Non-interactive: render table
	return renderTable(root, options)
}

// renderTable outputs data as a bordered table (non-interactive)
func renderTable(root any, options *ViewerOptions) error {
	engine, err := core.New(core.WithSortOrder(core.SortAscending))
	if err != nil {
		return fmt.Errorf("failed to create engine: %w", err)
	}

	// Auto-detect terminal width or use provided
	keyWidth, valueWidth := 30, 50
	if options.Width > 0 {
		keyWidth = options.Width / 3
		valueWidth = options.Width - keyWidth - 10
	}

	output := engine.RenderTable(root, options.NoColor, keyWidth, valueWidth)
	fmt.Fprintln(options.Out, output)
	return nil
}

// runInteractive launches the kvx TUI
func runInteractive(root any, options *ViewerOptions) error {
	// Set up scafctl's CEL provider for the TUI
	if err := setupScafctlCELProvider(); err != nil {
		return fmt.Errorf("failed to setup CEL provider: %w", err)
	}

	cfg := tui.DefaultConfig()
	cfg.AppName = options.AppName
	cfg.NoColor = options.NoColor
	cfg.Width = options.Width
	cfg.Height = options.Height

	if options.HelpTitle != "" {
		cfg.HelpAboutTitle = options.HelpTitle
	}
	if len(options.HelpLines) > 0 {
		cfg.HelpAboutLines = options.HelpLines
	}

	teaOpts := tui.WithIO(options.In, options.Out)
	return tui.Run(root, cfg, teaOpts...)
}

// IsTerminal checks if the writer is a terminal
func IsTerminal(out io.Writer) bool {
	if f, ok := out.(*os.File); ok {
		stat, err := f.Stat()
		if err != nil {
			return false
		}
		return (stat.Mode() & os.ModeCharDevice) != 0
	}
	return false
}
```

**File: `pkg/terminal/kvx/cel.go`**

```go
package kvx

import (
	"github.com/google/cel-go/cel"
	"github.com/oakwood-commons/kvx/pkg/tui"
	"github.com/oakwood-commons/scafctl/pkg/celexp"
)

// evaluateWithScafctlCEL evaluates a CEL expression using scafctl's CEL environment
func evaluateWithScafctlCEL(expr string, root any) (any, error) {
	// Create scafctl CEL context with all custom functions
	celCtx, err := celexp.NewContext(
		celexp.WithVariable("_", root), // kvx convention: _ is root
	)
	if err != nil {
		return nil, err
	}

	result, err := celCtx.EvaluateExpression(expr)
	if err != nil {
		return nil, err
	}

	return result, nil
}

// setupScafctlCELProvider configures kvx TUI to use scafctl's CEL environment
func setupScafctlCELProvider() error {
	// Get scafctl's base CEL environment
	celCtx, err := celexp.NewContext()
	if err != nil {
		return err
	}

	// Add the _ variable for kvx compatibility
	env, err := celCtx.Env().Extend(
		cel.Variable("_", cel.DynType),
	)
	if err != nil {
		return err
	}

	// Create hints for scafctl-specific functions
	hints := map[string]string{
		// Add hints for commonly used scafctl CEL functions
		"base64Encode": "e.g. base64Encode(_.secret)",
		"base64Decode": "e.g. base64Decode(_.encoded)",
		"jsonEncode":   "e.g. jsonEncode(_.config)",
		"yamlEncode":   "e.g. yamlEncode(_.config)",
		// Add more as needed based on celexp package
	}

	provider := tui.NewCELExpressionProvider(env, hints)
	tui.SetExpressionProvider(provider)

	return nil
}
```

#### 1.3 Modify `scafctl run solution` Command

**Changes to `pkg/cmd/scafctl/run/solution.go`:**

1. Update fields and valid output types:
```go
// ValidOutputTypes defines the supported output formats
var ValidOutputTypes = []string{"table", "json", "yaml", "quiet"}

type SolutionOptions struct {
	// ... existing fields ...
	Interactive bool   // -i: launch kvx TUI
	Expression  string // -e: CEL expression to filter output
}
```

2. Add new flags in `CommandSolution`:
```go
cCmd.Flags().BoolVarP(&options.Interactive, "interactive", "i", false, 
    "Launch interactive viewer to explore results (requires terminal)")
cCmd.Flags().StringVarP(&options.Expression, "expression", "e", "",
    "CEL expression to filter/transform output (e.g., '_.database' or '_.items.filter(x, x.enabled)')")
cCmd.Flags().StringVarP(&options.Output, "output", "o", "table", 
    fmt.Sprintf("Output format: %s", strings.Join(ValidOutputTypes, ", ")))
```

3. Update `writeOutput` method:
```go
func (o *SolutionOptions) writeOutput(_ context.Context, results map[string]any) error {
	if o.Output == "quiet" {
		return nil
	}

	// Use kvx for table output (default) or when interactive flag is set
	if o.Output == "table" || o.Output == "" || o.Interactive {
		return kvx.View(results,
			kvx.WithAppName("scafctl run solution"),
			kvx.WithNoColor(o.CliParams.NoColor),
			kvx.WithIO(o.IOStreams.In, o.IOStreams.Out),
			kvx.WithExpression(o.Expression),
			kvx.WithInteractive(o.Interactive),
			kvx.WithHelp("scafctl run solution", []string{
				"Resolver Results Viewer",
				"",
				"Use arrow keys to navigate",
				"F3: Search | F6: CEL Expression",
				"F5: Copy path | F10: Quit",
			}),
		)
	}

	// Traditional JSON/YAML output for piping
	var data []byte
	var err error

	// Apply expression filter if provided (for json/yaml output too)
	outputData := any(results)
	if o.Expression != "" {
		outputData, err = kvx.EvaluateExpression(o.Expression, results)
		if err != nil {
			return fmt.Errorf("expression evaluation failed: %w", err)
		}
	}

	switch o.Output {
	case "yaml":
		data, err = yaml.Marshal(outputData)
	case "json":
		data, err = json.MarshalIndent(outputData, "", "  ")
	default:
		return fmt.Errorf("unsupported output format: %s", o.Output)
	}

	if err != nil {
		return fmt.Errorf("failed to marshal output: %w", err)
	}

	fmt.Fprintln(o.IOStreams.Out, string(data))
	return nil
}
```

#### 1.4 Update Help Text

```go
Long: `Execute a solution file by running all defined resolvers in dependency order.

Resolvers are organized into phases based on their dependencies. Resolvers within 
the same phase execute concurrently. 

OUTPUT MODES:
  table        Bordered table view (default when terminal)
  json         JSON output (for piping/scripting)
  yaml         YAML output (for piping/scripting)
  quiet        Suppress output (exit code only)

FLAGS:
  -i, --interactive    Launch interactive TUI for deep exploration
  -e, --expression     Filter output with CEL expression (uses scafctl CEL functions)
  -o, --output         Force specific output format

CEL EXPRESSIONS:
  Use -e to filter or transform the output data before display:
    -e '_.database'                    Select specific resolver result
    -e '_.items.filter(x, x.enabled)'  Filter arrays
    -e 'size(_.results)'               Compute values

Examples:
  # Run solution (default: table view)
  scafctl run solution -f ./solution.yaml

  # Explore results interactively
  scafctl run solution -f ./solution.yaml -i

  # Filter to specific resolver result
  scafctl run solution -f ./solution.yaml -e '_.database'

  # Filter then explore interactively
  scafctl run solution -f ./solution.yaml -e '_.config' -i

  # JSON output for piping
  scafctl run solution -f ./solution.yaml -o json | jq .

  # YAML output with expression filter
  scafctl run solution -f ./solution.yaml -e '_.secrets' -o yaml`
```

### POC Success Criteria

- [ ] Default output is kvx table view (bordered, colored)
- [ ] `-i` flag launches interactive TUI
- [ ] `-e '_.resolver_name'` filters to specific resolver result
- [ ] `-o json` and `-o yaml` still work for piping
- [ ] scafctl CEL functions work in expressions (e.g., `base64Encode`)
- [ ] Graceful fallback to JSON when output is not a terminal
- [ ] No regressions in existing functionality

---

## Phase 2: Generalized Output Handler

### Objective
Create a shared output infrastructure that any command can use to support kvx table/interactive mode with CEL filtering.

### Implementation Steps

#### 2.1 Create Output Types Package

**File: `pkg/terminal/output/formats.go`**

```go
package output

// OutputFormat represents supported output formats
type OutputFormat string

const (
	OutputFormatTable       OutputFormat = "table"       // kvx table (default for terminal)
	OutputFormatJSON        OutputFormat = "json"        // JSON output
	OutputFormatYAML        OutputFormat = "yaml"        // YAML output
	OutputFormatQuiet       OutputFormat = "quiet"       // No output
)

// BaseOutputFormats returns the common output formats for data commands
func BaseOutputFormats() []string {
	return []string{"table", "json", "yaml", "quiet"}
}

// IsStructuredFormat returns true if format is meant for piping (json/yaml)
func IsStructuredFormat(format OutputFormat) bool {
	return format == OutputFormatJSON || format == OutputFormatYAML
}
```

#### 2.2 Extend Output Package with kvx Support

**File: `pkg/terminal/output/kvx_output.go`**

```go
package output

import (
	"encoding/json"
	"fmt"

	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/kvx"
	"gopkg.in/yaml.v3"
)

// KvxOutputOptions configures kvx-enabled output behavior
type KvxOutputOptions struct {
	IOStreams   *terminal.IOStreams
	Format      OutputFormat
	Interactive bool   // -i flag: launch TUI
	Expression  string // -e flag: CEL expression filter
	NoColor     bool
	AppName     string   // For TUI title
	HelpTitle   string   // For TUI help
	HelpLines   []string // For TUI help content
}

// Write outputs data in the configured format with kvx support
func (o *KvxOutputOptions) Write(data any) error {
	// Quiet mode: no output
	if o.Format == OutputFormatQuiet {
		return nil
	}

	// Determine if we should use kvx
	useKvx := o.Format == OutputFormatTable || o.Format == "" || o.Interactive

	if useKvx {
		// Check terminal requirement for table/interactive output
		if !kvx.IsTerminal(o.IOStreams.Out) {
			// Auto-fallback to JSON when piped
			if o.Format == OutputFormatTable || o.Format == "" {
				return o.writeJSON(data)
			}
			return fmt.Errorf("interactive mode requires a terminal; use -o json or -o yaml")
		}

		return kvx.View(data,
			kvx.WithAppName(o.AppName),
			kvx.WithNoColor(o.NoColor),
			kvx.WithIO(o.IOStreams.In, o.IOStreams.Out),
			kvx.WithExpression(o.Expression),
			kvx.WithInteractive(o.Interactive),
			kvx.WithHelp(o.HelpTitle, o.HelpLines),
		)
	}

	// Apply expression filter for json/yaml output too
	outputData := data
	if o.Expression != "" {
		var err error
		outputData, err = kvx.EvaluateExpression(o.Expression, data)
		if err != nil {
			return fmt.Errorf("expression evaluation failed: %w", err)
		}
	}

	// Traditional JSON/YAML output
	switch o.Format {
	case OutputFormatYAML:
		return o.writeYAML(outputData)
	case OutputFormatJSON:
		return o.writeJSON(outputData)
	default:
		return fmt.Errorf("unsupported output format: %s", o.Format)
	}
}

func (o *KvxOutputOptions) writeJSON(data any) error {
	bytes, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}
	fmt.Fprintln(o.IOStreams.Out, string(bytes))
	return nil
}

func (o *KvxOutputOptions) writeYAML(data any) error {
	bytes, err := yaml.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal YAML: %w", err)
	}
	fmt.Fprintln(o.IOStreams.Out, string(bytes))
	return nil
}
```

#### 2.3 Create Shared Flags Helper

**File: `pkg/cmd/flags/output.go`**

```go
package flags

import (
	"fmt"
	"strings"

	"github.com/oakwood-commons/scafctl/pkg/terminal/output"
	"github.com/spf13/cobra"
)

// AddKvxOutputFlags adds kvx-enabled output flags to a command
// Returns pointers to the flag values for use in command options
func AddKvxOutputFlags(cmd *cobra.Command, outputFormat *string, interactive *bool, expression *string) {
	validFormats := output.BaseOutputFormats()

	cmd.Flags().StringVarP(outputFormat, "output", "o", "table",
		fmt.Sprintf("Output format: %s", strings.Join(validFormats, ", ")))

	cmd.Flags().BoolVarP(interactive, "interactive", "i", false,
		"Launch interactive viewer to explore results (requires terminal)")

	cmd.Flags().StringVarP(expression, "expression", "e", "",
		"CEL expression to filter/transform output data (e.g., '_.items.filter(x, x.enabled)')")
}

// ValidateKvxOutputFormat validates the output format
func ValidateKvxOutputFormat(format string) error {
	validFormats := output.BaseOutputFormats()
	for _, valid := range validFormats {
		if format == valid {
			return nil
		}
	}
	return fmt.Errorf("invalid output format: %s (valid: %s)", format, strings.Join(validFormats, ", "))
}
```

### Phase 2 Deliverables

- [x] `OutputOptions` struct with `Write()` method in `pkg/terminal/kvx/output.go`
- [x] Auto-fallback to JSON when piping table output (non-TTY detection)
- [x] Expression filter works for all output formats
- [x] Shared flags helper for consistent `-o`, `-i`, `-e` flags in `pkg/cmd/flags/output.go`
- [x] Refactored `run solution` to use new shared output infrastructure
- [x] Unit tests for output handler (`pkg/terminal/kvx/output_test.go`, `pkg/cmd/flags/output_test.go`)
- [x] OutputFormat type and helpers in `pkg/terminal/output/formats.go`

---

## Phase 3: Full Rollout

### Objective
Add kvx output support to all commands that output structured data.

### Commands to Update

| Command | Priority | Notes |
|---------|----------|-------|
| `run solution` | Done (Phase 1) | POC command |
| `render workflow` | High | Complex action graphs benefit most |
| `get solution` | Medium | Solution structure exploration |
| `get resolver refs` | Medium | Dependency information |
| `version` | Low | Simple data, but good for consistency |

### Migration Pattern for Each Command

For each command, apply this pattern:

1. **Import packages**:
```go
import (
	cmdflags "github.com/oakwood-commons/scafctl/pkg/cmd/flags"
	"github.com/oakwood-commons/scafctl/pkg/terminal/output"
)
```

2. **Add fields to options struct**:
```go
type CommandOptions struct {
	// ... existing fields ...
	Output      string
	Interactive bool
	Expression  string
}
```

3. **Add flags in command setup**:
```go
cmdflags.AddKvxOutputFlags(cmd, &options.Output, &options.Interactive, &options.Expression)
```

4. **Replace output writing**:
```go
// Before
return output.WriteOutput(o.IOStreams, o.Output, data, nil)

// After
return (&output.KvxOutputOptions{
	IOStreams:   o.IOStreams,
	Format:      output.OutputFormat(o.Output),
	Interactive: o.Interactive,
	Expression:  o.Expression,
	NoColor:     o.CliParams.NoColor,
	AppName:     "scafctl <command>",
	HelpTitle:   "Command Output",
	HelpLines:   []string{"Explore the output data"},
}).Write(data)
```

5. **Update help text** with new flags and examples

### Example: `render workflow` Migration

```go
// In CommandWorkflow setup
cmdflags.AddKvxOutputFlags(cCmd, &options.Output, &options.Interactive, &options.Expression)

// In writeOutput method
func (o *WorkflowOptions) writeOutput(graph *action.ActionGraph) error {
	return (&output.KvxOutputOptions{
		IOStreams:   o.IOStreams,
		Format:      output.OutputFormat(o.Output),
		Interactive: o.Interactive,
		Expression:  o.Expression,
		NoColor:     o.CliParams.NoColor,
		AppName:     "scafctl render workflow",
		HelpTitle:   "Action Graph Viewer",
		HelpLines: []string{
			"Explore the rendered action graph",
			"",
			"Navigate execution phases and actions",
			"Use expressions to filter specific actions",
		},
	}).Write(graph)
}
```

### Phase 3 Deliverables

- [ ] `render workflow` updated with kvx output
- [ ] `get solution` updated with kvx output
- [ ] `get resolver refs` updated with kvx output
- [ ] `version` updated with kvx output (low priority)
- [ ] Consistent `-o`, `-i`, `-e` flags across all commands
- [ ] Updated documentation for all commands

---

## CEL Integration Details

### scafctl CEL Functions in kvx

The interactive TUI will have access to all scafctl CEL functions. Key functions to highlight in help hints:

| Function | Example | Description |
|----------|---------|-------------|
| `base64Encode` | `base64Encode(_.secret)` | Encode value to base64 |
| `base64Decode` | `base64Decode(_.encoded)` | Decode base64 value |
| `jsonEncode` | `jsonEncode(_.config)` | Serialize to JSON string |
| `yamlEncode` | `yamlEncode(_.config)` | Serialize to YAML string |
| `jsonDecode` | `jsonDecode(_.jsonStr)` | Parse JSON string |
| `yamlDecode` | `yamlDecode(_.yamlStr)` | Parse YAML string |
| `regexMatch` | `regexMatch("pattern", _.value)` | Regex matching |
| `urlEncode` | `urlEncode(_.param)` | URL encode string |

### Expression Examples for Users

```bash
# Filter to specific key
scafctl run solution -e '_.database'

# Filter array items
scafctl run solution -e '_.items.filter(x, x.status == "active")'

# Transform data
scafctl run solution -e '_.configs.map(c, c.name)'

# Check conditions
scafctl run solution -e '_.items.exists(x, x.critical)'

# Use scafctl functions
scafctl run solution -e 'base64Decode(_.encodedSecret)'

# Complex queries
scafctl run solution -e '_.services.filter(s, s.replicas > 1).map(s, {"name": s.name, "replicas": s.replicas})'
```

---

## Testing Strategy

### Unit Tests

1. **kvx package tests** (`pkg/terminal/kvx/viewer_test.go`):
   - Test `IsTerminal` with various writers
   - Test option application
   - Test expression evaluation with scafctl CEL functions
   - Mock tui.Run for error cases

2. **Output handler tests** (`pkg/terminal/output/kvx_output_test.go`):
   - Test format selection logic
   - Test auto-fallback to JSON when piping
   - Test expression filter applies to all formats
   - Test error messages for invalid expressions

3. **CEL integration tests** (`pkg/terminal/kvx/cel_test.go`):
   - Test scafctl CEL functions work in expressions
   - Test error handling for invalid expressions
   - Test complex nested expressions

### Integration Tests

```go
func TestSolutionCommandWithKvxOutput(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantFormat string
		wantErr    bool
	}{
		{"default table", []string{"-f", "test.yaml"}, "table", false},
		{"json output", []string{"-f", "test.yaml", "-o", "json"}, "json", false},
		{"expression filter", []string{"-f", "test.yaml", "-e", "_.key"}, "table", false},
		{"invalid expression", []string{"-f", "test.yaml", "-e", "invalid(("}, "", true},
		{"interactive non-tty", []string{"-f", "test.yaml", "-i"}, "", true}, // when piped
	}
	// ... test implementation
}
```

### Manual Testing Checklist

- [ ] Default table output displays correctly
- [ ] `-i` launches interactive TUI
- [ ] `-e` filters data before display
- [ ] `-e` + `-i` filters then allows interactive exploration
- [ ] `-o json` outputs valid JSON
- [ ] `-o yaml` outputs valid YAML
- [ ] Piping table output auto-falls back to JSON
- [ ] scafctl CEL functions work in expressions
- [ ] `--no-color` disables colors in table output
- [ ] Error messages are clear for invalid expressions

---

## Timeline Estimate

| Phase | Effort | Dependencies |
|-------|--------|--------------|
| **Phase 1 (POC)** | 6-8 hours | kvx module path fixed |
| **Phase 2 (Generalize)** | 4-6 hours | Phase 1 complete |
| **Phase 3 (Full Rollout)** | 1-2 hours per command | Phase 2 complete |
| **Testing & Polish** | 3-4 hours | All phases |

**Total: ~3-4 days**

---

## Backward Compatibility

### Breaking Changes

1. **Default output format changes**: Commands will default to `table` instead of `json`
   - **Mitigation**: Scripts using `scafctl ... | jq` will break
   - **Solution**: Document the change; users should use `-o json` explicitly

2. **New flags**: `-i` and `-e` are new
   - **Mitigation**: None needed, additive change

### Migration Guide for Users

```bash
# Old (worked because default was json)
scafctl run solution -f ./sol.yaml | jq .database

# New (explicit json format required for piping)
scafctl run solution -f ./sol.yaml -o json | jq .database

# Better (use built-in expression)
scafctl run solution -f ./sol.yaml -e '_.database' -o json
```

---

## Open Items

> **Note**: Per our collaboration agreement with the kvx developer, items that require changes to kvx will be raised as issues rather than worked around in scafctl. See the [Collaboration with kvx Developer](#collaboration-with-kvx-developer) section for tracking.

1. **Terminal width detection**: Should we detect terminal width for optimal table rendering?
   - Recommendation: Yes, kvx handles this automatically

2. **Large output warning**: Should we warn if output exceeds a certain size before rendering?
   - Recommendation: Add `--warn-size` flag or config option
   - _If kvx needs enhancement to support this, raise as issue_

3. **Config file support**: Should kvx theme/settings be configurable via scafctl config?
   - Recommendation: Future enhancement, not in initial implementation

4. **Custom CEL environment integration**: Ensure kvx's expression provider API cleanly supports external CEL environments
   - _Raise any friction points encountered during implementation_

---

## Next Steps

1. ✅ Plan approved by stakeholder
2. ✅ kvx module path fix (`github.com/oakwood-commons/kvx`) - Resolved
3. ✅ Charmbracelet dependency conflict - Resolved by kvx developer
4. ✅ **Phase 1 POC Complete**: `scafctl run solution` now has kvx integration
   - Default output is now table view (was JSON)
   - `-i/--interactive` flag launches TUI
   - `-e/--expression` flag filters output with CEL
   - `-o json` and `-o yaml` continue to work for piping
   - Auto-fallback to JSON when output is piped
5. ✅ POC reviewed, feedback positive
6. ✅ **Phase 2 Generalization Complete**: Shared output infrastructure created
   - `pkg/terminal/kvx/output.go` - `OutputOptions` struct with `Write()` method
   - `pkg/terminal/output/formats.go` - `OutputFormat` type and helpers
   - `pkg/cmd/flags/output.go` - `AddKvxOutputFlags()` and `ToKvxOutputOptions()` helpers
   - `scafctl run solution` refactored to use shared infrastructure
   - Full test coverage for new packages
7. 🔜 Roll out to remaining commands (Phase 3)
   - `render workflow` - High priority
   - `get solution` - Medium priority
   - `get resolver refs` - Medium priority
   - `version` - Low priority
