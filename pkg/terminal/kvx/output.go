// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package kvx

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

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
		string(OutputFormatJSON),
		string(OutputFormatYAML),
		string(OutputFormatQuiet),
		string(OutputFormatTest),
	}
}

// IsStructuredFormat returns true if the format is meant for piping (json/yaml).
// These formats should not use interactive or table output.
func IsStructuredFormat(format OutputFormat) bool {
	return format == OutputFormatJSON || format == OutputFormatYAML
}

// IsKvxFormat returns true if the format uses kvx visual output (auto, table, or list).
// These formats render human-readable output to the terminal.
func IsKvxFormat(format OutputFormat) bool {
	return format == OutputFormatAuto || format == OutputFormatTable || format == OutputFormatList || format == ""
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

// WithIOStreams sets the IOStreams for output.
func WithIOStreams(ioStreams *terminal.IOStreams) OutputOption {
	return func(o *OutputOptions) { o.IOStreams = ioStreams }
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

	// For structured formats (json/yaml), apply expression filter if provided
	return o.writeStructured(data)
}

// writeKvx handles table and interactive output using kvx.
func (o *OutputOptions) writeKvx(data any) error {
	// Check terminal requirement for table/interactive output
	if !IsTerminal(o.IOStreams.Out) {
		// Auto-fallback to JSON when piped (unless interactive was explicitly requested)
		if o.Interactive {
			return fmt.Errorf("interactive mode requires a terminal; use -o json or -o yaml for piped output")
		}
		// Silently fall back to JSON for non-interactive piped output.
		// Apply CEL expression filter if provided (same as structured output path).
		outputData := data
		if o.Expression != "" {
			ctx := o.Ctx
			if ctx == nil {
				ctx = context.Background()
			}
			var err error
			outputData, err = EvaluateExpression(ctx, o.Expression, data)
			if err != nil {
				return fmt.Errorf("expression evaluation failed: %w", err)
			}
		}
		return o.writeJSON(outputData)
	}

	// Build kvx options
	kvxOpts := []Option{
		WithNoColor(o.NoColor),
		WithIO(o.IOStreams.In, o.IOStreams.Out),
		WithInteractive(o.Interactive),
	}

	// Pass layout based on output format
	switch o.Format {
	case OutputFormatList:
		kvxOpts = append(kvxOpts, WithLayout("list"))
	case OutputFormatTable:
		kvxOpts = append(kvxOpts, WithLayout("table"))
	case OutputFormatAuto, OutputFormatJSON, OutputFormatYAML, OutputFormatQuiet, OutputFormatTest:
		// Auto and empty use default layout (auto).
		// JSON/YAML/Quiet/Test are handled upstream and should not reach here,
		// but are listed for exhaustiveness.
	}

	// Pass context for CEL expression evaluation (enables debug.out, etc.)
	if o.Ctx != nil {
		kvxOpts = append(kvxOpts, WithContext(o.Ctx))
	}

	if o.Expression != "" {
		kvxOpts = append(kvxOpts, WithExpression(o.Expression))
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

	return View(data, kvxOpts...)
}

// writeStructured handles JSON/YAML output with optional expression filtering.
func (o *OutputOptions) writeStructured(data any) error {
	// Apply expression filter if provided
	outputData := data
	if o.Expression != "" {
		// Use context from options, or fall back to Background
		ctx := o.Ctx
		if ctx == nil {
			ctx = context.Background()
		}
		var err error
		outputData, err = EvaluateExpression(ctx, o.Expression, data)
		if err != nil {
			return fmt.Errorf("expression evaluation failed: %w", err)
		}
	}

	switch o.Format {
	case OutputFormatJSON:
		return o.writeJSON(outputData)
	case OutputFormatYAML:
		return o.writeYAML(outputData)
	case OutputFormatTable, OutputFormatAuto, OutputFormatList, OutputFormatQuiet, OutputFormatTest:
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
