// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package kvx

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"sort"

	toml "github.com/pelletier/go-toml/v2"

	"github.com/oakwood-commons/kvx/pkg/tui"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"gopkg.in/yaml.v3"
)

// OutputFormat represents supported output formats for command output.
type OutputFormat string

const (
	// OutputFormatAuto lets kvx choose the best visual format (table or list)
	// based on the data shape. This is the default.
	OutputFormatAuto OutputFormat = "auto"

	// OutputFormatTable uses kvx bordered table view
	OutputFormatTable OutputFormat = "table"

	// OutputFormatList uses kvx list view for key-value display
	OutputFormatList OutputFormat = "list"

	// OutputFormatJSON outputs as JSON (for piping/scripting)
	OutputFormatJSON OutputFormat = "json"

	// OutputFormatYAML outputs as YAML (for piping/scripting)
	OutputFormatYAML OutputFormat = "yaml"

	// OutputFormatQuiet suppresses all output (exit code only)
	OutputFormatQuiet OutputFormat = "quiet"

	// OutputFormatTest generates a functional test definition from the command output.
	// The command is executed normally and the result is used to derive CEL assertions,
	// write a snapshot golden file to testdata/, and emit test YAML to stdout.
	OutputFormatTest OutputFormat = "test"

	// OutputFormatTree renders data as an ASCII tree structure
	OutputFormatTree OutputFormat = "tree"

	// OutputFormatMermaid renders data as a Mermaid flowchart diagram
	OutputFormatMermaid OutputFormat = "mermaid"

	// OutputFormatCSV outputs as comma-separated values (for spreadsheets/scripting)
	OutputFormatCSV OutputFormat = "csv"

	// OutputFormatTOML outputs as TOML (for config files/scripting)
	OutputFormatTOML OutputFormat = "toml"
)

// String returns the string representation of the output format.
func (f OutputFormat) String() string {
	return string(f)
}

// BaseOutputFormats returns the common output formats supported by data-outputting commands.
// This list is used for flag validation and help text generation.
func BaseOutputFormats() []string {
	return []string{
		string(OutputFormatAuto),
		string(OutputFormatTable),
		string(OutputFormatList),
		string(OutputFormatTree),
		string(OutputFormatMermaid),
		string(OutputFormatJSON),
		string(OutputFormatYAML),
		string(OutputFormatCSV),
		string(OutputFormatTOML),
		string(OutputFormatQuiet),
		string(OutputFormatTest),
	}
}

// IsStructuredFormat returns true if the format produces machine-readable or
// text-only output (json, yaml, mermaid) rather than interactive TUI or
// bordered table output. Mermaid is included because it is a piping-friendly
// plain-text format even though Write() routes it through the kvx renderer.
func IsStructuredFormat(format OutputFormat) bool {
	return format == OutputFormatJSON || format == OutputFormatYAML || format == OutputFormatMermaid || format == OutputFormatCSV || format == OutputFormatTOML
}

// IsKvxFormat returns true if the format uses kvx visual output (auto, table, list, or tree).
// These formats render human-readable output to the terminal.
func IsKvxFormat(format OutputFormat) bool {
	return format == OutputFormatAuto || format == OutputFormatTable || format == OutputFormatList || format == OutputFormatTree || format == ""
}

// IsAutoFormat returns true if the format uses automatic layout selection.
func IsAutoFormat(format OutputFormat) bool {
	return format == OutputFormatAuto || format == ""
}

// IsListFormat returns true if the format uses kvx list output.
func IsListFormat(format OutputFormat) bool {
	return format == OutputFormatList
}

// IsQuietFormat returns true if the format suppresses output.
func IsQuietFormat(format OutputFormat) bool {
	return format == OutputFormatQuiet
}

// ParseOutputFormat parses a string into an OutputFormat.
// It returns the format and whether it was recognized.
func ParseOutputFormat(s string) (OutputFormat, bool) {
	switch s {
	case "auto", "":
		return OutputFormatAuto, true
	case "table":
		return OutputFormatTable, true
	case "list":
		return OutputFormatList, true
	case "json":
		return OutputFormatJSON, true
	case "yaml":
		return OutputFormatYAML, true
	case "quiet":
		return OutputFormatQuiet, true
	case "test":
		return OutputFormatTest, true
	case "tree":
		return OutputFormatTree, true
	case "mermaid":
		return OutputFormatMermaid, true
	case "csv":
		return OutputFormatCSV, true
	case "toml":
		return OutputFormatTOML, true
	default:
		return "", false
	}
}

// OutputOptions configures kvx-enabled output behavior for commands.
// It provides a unified way to handle table, interactive, JSON, and YAML output
// with CEL expression filtering support.
type OutputOptions struct {
	// Ctx is the context for CEL expression evaluation.
	// This enables context-dependent features like debug.out when Writer is in context.
	// If nil, context.Background() is used.
	Ctx context.Context `json:"-" yaml:"-"`

	// IOStreams provides input/output streams for the command
	IOStreams *terminal.IOStreams `json:"-" yaml:"-"`

	// Format specifies the output format (table, json, yaml, quiet)
	Format OutputFormat `json:"format,omitempty" yaml:"format,omitempty" doc:"Output format" example:"table" maxLength:"10"`

	// Interactive launches the kvx TUI for data exploration
	Interactive bool `json:"interactive,omitempty" yaml:"interactive,omitempty" doc:"Launch interactive TUI mode"`

	// Expression is a CEL expression to filter/transform output data
	Expression string `json:"expression,omitempty" yaml:"expression,omitempty" doc:"CEL expression to filter output" example:"_.database" maxLength:"4096"`

	// Where is a per-item CEL boolean filter applied to list data before rendering.
	// Each item is tested individually; only items where the expression evaluates to true are kept.
	Where string `json:"where,omitempty" yaml:"where,omitempty" doc:"Per-item CEL filter for list data" example:"_.enabled" maxLength:"4096"`

	// NoColor disables colored output
	NoColor bool `json:"noColor,omitempty" yaml:"noColor,omitempty" doc:"Disable colored output"`

	// AppName is shown in the TUI title
	AppName string `json:"appName,omitempty" yaml:"appName,omitempty" doc:"Application name for TUI title" example:"scafctl run solution" maxLength:"100"`

	// HelpTitle is the help section title in interactive mode
	HelpTitle string `json:"helpTitle,omitempty" yaml:"helpTitle,omitempty" doc:"Help section title for TUI" example:"Resolver Results" maxLength:"100"`

	// HelpLines are help text lines shown in interactive mode
	HelpLines []string `json:"helpLines,omitempty" yaml:"helpLines,omitempty" doc:"Help text lines for TUI" maxItems:"20"`

	// Theme is the color theme for interactive mode (dark, warm, cool, midnight)
	Theme string `json:"theme,omitempty" yaml:"theme,omitempty" doc:"Color theme for TUI" example:"dark" maxLength:"20"`

	// PrettyPrint enables indented JSON output
	PrettyPrint bool `json:"prettyPrint,omitempty" yaml:"prettyPrint,omitempty" doc:"Enable indented JSON output"`

	// ColumnOrder specifies the preferred display order of columns for table rendering.
	// Fields not listed are appended in their natural order.
	ColumnOrder []string `json:"columnOrder,omitempty" yaml:"columnOrder,omitempty" doc:"Preferred column display order for table rendering"`

	// ColumnHints provides per-column display customizations (header rename, max width, alignment, visibility).
	// Use SchemaJSON for a declarative alternative derived from a JSON Schema.
	ColumnHints map[string]tui.ColumnHint `json:"-" yaml:"-" doc:"Per-column display hints"`

	// SchemaJSON is a JSON Schema document used to derive column display hints.
	// Parsed via tui.ParseSchema; any ColumnHints set directly take precedence
	// over schema-derived hints on a per-key basis.
	SchemaJSON []byte `json:"-" yaml:"-" doc:"JSON Schema for deriving column display hints"`

	// DisplaySchemaJSON is a JSON Schema document with x-kvx-* vendor extensions
	// that control the interactive TUI's card-list and detail view rendering.
	// Parsed via tui.ParseSchemaWithDisplay for use in interactive mode.
	// When set, the TUI renders arrays as a scrollable card list with sectioned
	// detail views instead of the default KEY/VALUE table.
	DisplaySchemaJSON []byte `json:"-" yaml:"-" doc:"JSON Schema with x-kvx-* extensions for rich TUI rendering"`
}

// NewOutputOptions creates a new OutputOptions with default settings.
func NewOutputOptions(ioStreams *terminal.IOStreams) *OutputOptions {
	return &OutputOptions{
		IOStreams:   ioStreams,
		Format:      OutputFormatAuto,
		PrettyPrint: true,
	}
}

// OutputOption is a functional option for configuring OutputOptions.
type OutputOption func(*OutputOptions)

// WithOutputFormat sets the output format.
func WithOutputFormat(format OutputFormat) OutputOption {
	return func(o *OutputOptions) { o.Format = format }
}

// WithOutputFormatString sets the output format from a string.
func WithOutputFormatString(format string) OutputOption {
	return func(o *OutputOptions) {
		if f, ok := ParseOutputFormat(format); ok {
			o.Format = f
		}
	}
}

// WithOutputInteractive enables interactive TUI mode.
func WithOutputInteractive(interactive bool) OutputOption {
	return func(o *OutputOptions) { o.Interactive = interactive }
}

// WithOutputExpression sets a CEL expression to filter/transform output.
func WithOutputExpression(expr string) OutputOption {
	return func(o *OutputOptions) { o.Expression = expr }
}

// WithOutputNoColor disables colored output.
func WithOutputNoColor(noColor bool) OutputOption {
	return func(o *OutputOptions) { o.NoColor = noColor }
}

// WithOutputAppName sets the application name for TUI title.
func WithOutputAppName(name string) OutputOption {
	return func(o *OutputOptions) { o.AppName = name }
}

// WithOutputHelp sets custom help text for interactive mode.
func WithOutputHelp(title string, lines []string) OutputOption {
	return func(o *OutputOptions) {
		o.HelpTitle = title
		o.HelpLines = lines
	}
}

// WithOutputTheme sets the color theme for TUI.
func WithOutputTheme(theme string) OutputOption {
	return func(o *OutputOptions) { o.Theme = theme }
}

// WithOutputPrettyPrint enables or disables indented JSON output.
func WithOutputPrettyPrint(pretty bool) OutputOption {
	return func(o *OutputOptions) { o.PrettyPrint = pretty }
}

// WithOutputContext sets the context for CEL expression evaluation.
// This enables context-dependent features like debug.out when Writer is in context.
func WithOutputContext(ctx context.Context) OutputOption {
	return func(o *OutputOptions) { o.Ctx = ctx }
}

// WithOutputColumnOrder sets the preferred column order for table rendering.
func WithOutputColumnOrder(order []string) OutputOption {
	return func(o *OutputOptions) { o.ColumnOrder = order }
}

// WithOutputColumnHints sets per-column display hints (header rename, max width, alignment, visibility).
// For a declarative alternative, use WithOutputSchemaJSON.
func WithOutputColumnHints(hints map[string]tui.ColumnHint) OutputOption {
	return func(o *OutputOptions) { o.ColumnHints = hints }
}

// WithOutputSchemaJSON sets a JSON Schema document used to derive column display hints.
// The schema is parsed via tui.ParseSchema. Any ColumnHints set directly take
// precedence over schema-derived hints on a per-key basis.
func WithOutputSchemaJSON(schema []byte) OutputOption {
	return func(o *OutputOptions) { o.SchemaJSON = schema }
}

// WithOutputDisplaySchemaJSON sets a JSON Schema document with x-kvx-* vendor extensions
// that control the interactive TUI's card-list and detail view rendering.
// The schema is parsed via tui.ParseSchemaWithDisplay. Column hints derived from
// the schema are merged with any programmatic ColumnHints (programmatic take precedence).
func WithOutputDisplaySchemaJSON(schema []byte) OutputOption {
	return func(o *OutputOptions) { o.DisplaySchemaJSON = schema }
}

// WithIOStreams sets the IOStreams for output.
func WithIOStreams(ioStreams *terminal.IOStreams) OutputOption {
	return func(o *OutputOptions) { o.IOStreams = ioStreams }
}

// Snapshot renders a non-interactive snapshot of the TUI and returns it as a string.
// This produces the same visual output as interactive mode but without blocking for input,
// making it suitable for golden file tests and non-TTY environments.
func (o *OutputOptions) Snapshot(data any) (string, error) {
	kvxOpts := o.buildViewerOptions()
	return Snapshot(data, kvxOpts...)
}

// Write outputs data in the configured format with kvx support.
// It handles automatic fallback to JSON when output is piped,
// CEL expression filtering, and interactive TUI mode.
func (o *OutputOptions) Write(data any) error {
	// Quiet mode: no output
	if o.Format == OutputFormatQuiet {
		return nil
	}

	// Test generation is handled at the command level before reaching kvx.
	// If it reaches here, the command does not implement test output support.
	if o.Format == OutputFormatTest {
		return fmt.Errorf("output format %q is not supported by this command; supported formats: auto, table, list, json, yaml, quiet", OutputFormatTest)
	}

	// Determine if we should use kvx visual output
	useKvx := IsKvxFormat(o.Format) || o.Interactive

	if useKvx {
		return o.writeKvx(data)
	}

	// Mermaid produces plain text via the kvx renderer (not JSON/YAML serialization),
	// so route it to writeKvx even though IsStructuredFormat reports true.
	if o.Format == OutputFormatMermaid {
		return o.writeKvx(data)
	}

	// For structured formats (json/yaml), apply expression filter if provided
	return o.writeStructured(data)
}

// writeKvx handles table and interactive output using kvx.
func (o *OutputOptions) writeKvx(data any) error {
	// Tree and mermaid are text formats that should render regardless of TTY.
	// Only table/list/auto/interactive need a terminal.
	isTTYRequired := o.Format != OutputFormatTree && o.Format != OutputFormatMermaid

	// Check terminal requirement for table/interactive output
	if isTTYRequired && !IsTerminal(o.IOStreams.Out) {
		// Auto-fallback to JSON when piped (unless interactive was explicitly requested)
		if o.Interactive {
			return fmt.Errorf("interactive mode requires a terminal; use -o json or -o yaml for piped output")
		}
		// Silently fall back to JSON for non-interactive piped output.
		// Apply Where then Expression (same order as writeStructured).
		outputData := data
		var filterErr error
		outputData, filterErr = applyWhereFilter(o.Where, outputData)
		if filterErr != nil {
			return filterErr
		}
		if o.Expression != "" {
			ctx := o.Ctx
			if ctx == nil {
				ctx = context.Background()
			}
			var err error
			outputData, err = EvaluateExpression(ctx, o.Expression, outputData)
			if err != nil {
				return fmt.Errorf("expression evaluation failed: %w", err)
			}
		}
		// Print scalar values as plain text instead of JSON wrapping.
		if isScalarValue(outputData) {
			fmt.Fprintln(o.IOStreams.Out, outputData)
			return nil
		}
		return o.writeJSON(outputData)
	}

	// Build kvx options
	kvxOpts := o.buildViewerOptions()
	kvxOpts = append(kvxOpts,
		WithIO(o.IOStreams.In, o.IOStreams.Out),
		WithInteractive(o.Interactive),
	)

	// Pre-evaluate expression for scalar detection: if Where + Expression
	// resolve to a scalar value (string, number, bool), print it directly
	// instead of wrapping it in a table view.
	if !o.Interactive && (o.Expression != "" || o.Where != "") {
		preData := data
		var preErr error
		preData, preErr = applyWhereFilter(o.Where, preData)
		if preErr != nil {
			return preErr
		}
		if o.Expression != "" {
			ctx := o.Ctx
			if ctx == nil {
				ctx = context.Background()
			}
			preData, preErr = EvaluateExpression(ctx, o.Expression, preData)
			if preErr != nil {
				return fmt.Errorf("expression evaluation failed: %w", preErr)
			}
		}
		if isScalarValue(preData) {
			fmt.Fprintln(o.IOStreams.Out, preData)
			return nil
		}
		// Not a scalar — pass pre-evaluated data to viewer without re-evaluating.
		// Remove expression/where from options to avoid double evaluation.
		kvxOpts = removeExpressionOptions(kvxOpts)
		data = preData
	}

	// Pass layout based on output format
	switch o.Format {
	case OutputFormatList:
		kvxOpts = append(kvxOpts, WithLayout("list"))
	case OutputFormatTable:
		kvxOpts = append(kvxOpts, WithLayout("table"))
	case OutputFormatTree:
		kvxOpts = append(kvxOpts, WithLayout("tree"))
	case OutputFormatMermaid:
		kvxOpts = append(kvxOpts, WithLayout("mermaid"))
	case OutputFormatAuto, OutputFormatJSON, OutputFormatYAML, OutputFormatCSV, OutputFormatTOML, OutputFormatQuiet, OutputFormatTest:
		// Auto and empty use default layout (auto).
		// Structured formats (JSON/YAML/CSV/TOML), Quiet, and Test are handled
		// upstream and should not reach here, but are listed for exhaustiveness.
	}

	return View(data, kvxOpts...)
}

// buildViewerOptions converts OutputOptions fields into viewer Option slice.
// This is shared between writeKvx and Snapshot to avoid duplication.
func (o *OutputOptions) buildViewerOptions() []Option {
	kvxOpts := []Option{
		WithNoColor(o.NoColor),
	}

	// Pass context for CEL expression evaluation (enables debug.out, etc.)
	if o.Ctx != nil {
		kvxOpts = append(kvxOpts, WithContext(o.Ctx))
	}

	if o.Expression != "" {
		kvxOpts = append(kvxOpts, WithExpression(o.Expression))
	}
	if o.Where != "" {
		kvxOpts = append(kvxOpts, WithWhere(o.Where))
	}
	if o.AppName != "" {
		kvxOpts = append(kvxOpts, WithAppName(o.AppName))
	}
	if o.HelpTitle != "" || len(o.HelpLines) > 0 {
		kvxOpts = append(kvxOpts, WithHelp(o.HelpTitle, o.HelpLines))
	}
	if o.Theme != "" {
		kvxOpts = append(kvxOpts, WithTheme(o.Theme))
	}
	if len(o.ColumnOrder) > 0 {
		kvxOpts = append(kvxOpts, WithColumnOrder(o.ColumnOrder))
	}
	if len(o.ColumnHints) > 0 {
		kvxOpts = append(kvxOpts, WithColumnHints(o.ColumnHints))
	}
	if len(o.SchemaJSON) > 0 {
		kvxOpts = append(kvxOpts, WithSchemaJSON(o.SchemaJSON))
	}
	if len(o.DisplaySchemaJSON) > 0 {
		kvxOpts = append(kvxOpts, WithDisplaySchemaJSON(o.DisplaySchemaJSON))
	}

	return kvxOpts
}

// writeStructured handles JSON/YAML output with optional expression and where filtering.
func (o *OutputOptions) writeStructured(data any) error {
	outputData := data

	// Apply per-item Where filter before expression/serialization
	var err error
	outputData, err = applyWhereFilter(o.Where, outputData)
	if err != nil {
		return err
	}

	// Apply expression filter if provided
	if o.Expression != "" {
		// Use context from options, or fall back to Background
		ctx := o.Ctx
		if ctx == nil {
			ctx = context.Background()
		}
		var err error
		outputData, err = EvaluateExpression(ctx, o.Expression, outputData)
		if err != nil {
			return fmt.Errorf("expression evaluation failed: %w", err)
		}
	}

	switch o.Format {
	case OutputFormatJSON:
		return o.writeJSON(outputData)
	case OutputFormatYAML:
		return o.writeYAML(outputData)
	case OutputFormatCSV:
		return o.writeCSV(outputData)
	case OutputFormatTOML:
		return o.writeTOML(outputData)
	case OutputFormatTable, OutputFormatAuto, OutputFormatList, OutputFormatTree, OutputFormatMermaid, OutputFormatQuiet, OutputFormatTest:
		// These formats are handled upstream (writeKvx or command-level test generation),
		// and should not reach writeStructured.
		return fmt.Errorf("unexpected output format in writeStructured: %s", o.Format)
	default:
		return fmt.Errorf("unsupported output format: %s", o.Format)
	}
}

// writeJSON writes data as JSON.
func (o *OutputOptions) writeJSON(data any) error {
	var jsonData []byte
	var err error

	if o.PrettyPrint {
		jsonData, err = json.MarshalIndent(data, "", "  ")
	} else {
		jsonData, err = json.Marshal(data)
	}
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}

	fmt.Fprintln(o.IOStreams.Out, string(jsonData))
	return nil
}

// writeYAML writes data as YAML.
func (o *OutputOptions) writeYAML(data any) error {
	yamlData, err := yaml.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal YAML: %w", err)
	}
	fmt.Fprintln(o.IOStreams.Out, string(yamlData))
	return nil
}

// writeTOML writes data as TOML.
func (o *OutputOptions) writeTOML(data any) error {
	tomlData, err := toml.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal TOML: %w", err)
	}
	fmt.Fprint(o.IOStreams.Out, string(tomlData))
	return nil
}

// writeCSV writes data as comma-separated values.
// For a slice of maps, column headers are derived from the union of all keys.
// For a single map, it writes a two-row CSV (header + values).
// Scalar values are written as a single cell.
func (o *OutputOptions) writeCSV(data any) error {
	w := csv.NewWriter(o.IOStreams.Out)
	defer w.Flush()

	switch v := data.(type) {
	case []any:
		return o.writeCSVSlice(w, v)
	case []map[string]any:
		items := make([]any, len(v))
		for i, m := range v {
			items[i] = m
		}
		return o.writeCSVSlice(w, items)
	case map[string]any:
		return o.writeCSVSlice(w, []any{v})
	default:
		// Scalar or unsupported type — write as single value
		return w.Write([]string{fmt.Sprintf("%v", data)})
	}
}

// writeCSVSlice writes a slice of items as CSV rows with a header row.
func (o *OutputOptions) writeCSVSlice(w *csv.Writer, items []any) error {
	if len(items) == 0 {
		return nil
	}

	// Collect all keys across all items for consistent columns
	keySet := make(map[string]struct{})
	for _, item := range items {
		if m, ok := item.(map[string]any); ok {
			for k := range m {
				keySet[k] = struct{}{}
			}
		}
	}

	headers := make([]string, 0, len(keySet))
	for k := range keySet {
		headers = append(headers, k)
	}
	sort.Strings(headers)

	// Apply column order if configured
	if len(o.ColumnOrder) > 0 {
		headers = applyColumnOrder(headers, o.ColumnOrder)
	}

	// Write header row
	if err := w.Write(headers); err != nil {
		return fmt.Errorf("failed to write CSV header: %w", err)
	}

	// Write data rows
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			// Non-map item — write as single column
			if err := w.Write([]string{fmt.Sprintf("%v", item)}); err != nil {
				return fmt.Errorf("failed to write CSV row: %w", err)
			}
			continue
		}
		row := make([]string, len(headers))
		for i, h := range headers {
			if v, exists := m[h]; exists {
				row[i] = fmt.Sprintf("%v", v)
			}
		}
		if err := w.Write(row); err != nil {
			return fmt.Errorf("failed to write CSV row: %w", err)
		}
	}

	return nil
}

// applyColumnOrder reorders headers based on the preferred column order.
// Fields in the order list come first; remaining fields are appended in their original order.
func applyColumnOrder(headers, order []string) []string {
	orderSet := make(map[string]struct{}, len(order))
	for _, o := range order {
		orderSet[o] = struct{}{}
	}

	result := make([]string, 0, len(headers))
	// Add ordered columns first (preserving order list sequence)
	for _, o := range order {
		for _, h := range headers {
			if h == o {
				result = append(result, h)
				break
			}
		}
	}
	// Append remaining columns not in order list
	for _, h := range headers {
		if _, ok := orderSet[h]; !ok {
			result = append(result, h)
		}
	}
	return result
}

// WriteTo writes data to a specific writer with the configured format.
// This is useful when you need to write to a different output than the configured IOStreams.
func (o *OutputOptions) WriteTo(w io.Writer, data any) error {
	// Create a copy with the new writer
	ioStreams := &terminal.IOStreams{
		In:     o.IOStreams.In,
		Out:    w,
		ErrOut: o.IOStreams.ErrOut,
	}
	oCopy := *o
	oCopy.IOStreams = ioStreams
	return oCopy.Write(data)
}

// StructToMap converts a struct (or slice of structs) to a map[string]any
// (or []any) via a JSON round-trip. This ensures field names match JSON tags
// and the resulting value is compatible with CEL expression evaluation.
func StructToMap(v any) (any, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal to JSON: %w", err)
	}
	var result any
	if err := json.Unmarshal(b, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal from JSON: %w", err)
	}
	return result, nil
}

// isScalarValue returns true if the value is a scalar type (string, number, bool)
// that should be printed directly rather than rendered in a table.
func isScalarValue(v any) bool {
	switch v.(type) {
	case string, bool,
		int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64,
		float32, float64:
		return true
	default:
		return false
	}
}

// removeExpressionOptions appends overrides that clear expression and where
// options. Since Option is an opaque func, we cannot filter the original slice;
// instead we append empty-value overrides so the viewer skips re-evaluation.
func removeExpressionOptions(opts []Option) []Option {
	return append(opts, WithExpression(""), WithWhere(""))
}
