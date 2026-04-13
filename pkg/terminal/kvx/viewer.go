// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package kvx provides integration with the kvx data viewer library for scafctl.
// It offers both non-interactive table output and interactive TUI modes for exploring
// structured data like resolver results and action graphs.
package kvx

import (
	"context"
	"fmt"
	"io"
	"os"

	tea "charm.land/bubbletea/v2"
	"github.com/oakwood-commons/kvx/pkg/core"
	"github.com/oakwood-commons/kvx/pkg/tui"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
)

// applyWhereFilter applies a per-item CEL boolean filter to data.
// Returns the original data unchanged if where is empty.
func applyWhereFilter(where string, data any) (any, error) {
	if where == "" {
		return data, nil
	}
	engine, err := core.New()
	if err != nil {
		return nil, fmt.Errorf("failed to create CEL engine for where filter: %w", err)
	}
	filtered, err := engine.EvaluateWhere(where, data)
	if err != nil {
		return nil, fmt.Errorf("where filter failed: %w", err)
	}
	return filtered, nil
}

// ViewerOptions configures the kvx viewer
type ViewerOptions struct {
	// Ctx is the context for CEL expression evaluation.
	// This enables context-dependent features like debug.out when Writer is in context.
	// If nil, context.Background() is used.
	Ctx context.Context `json:"-" yaml:"-"`

	// AppName is the application name shown in TUI (default: "scafctl")
	AppName string `json:"appName,omitempty" yaml:"appName,omitempty" doc:"Application name shown in TUI title" example:"scafctl run solution"`

	// Width is the terminal width (0 = auto-detect)
	Width int `json:"width,omitempty" yaml:"width,omitempty" doc:"Terminal width for rendering (0 = auto-detect)" example:"120" maximum:"500"`

	// Height is the terminal height (0 = auto-detect)
	Height int `json:"height,omitempty" yaml:"height,omitempty" doc:"Terminal height for rendering (0 = auto-detect)" example:"40" maximum:"200"`

	// NoColor disables colors in output
	NoColor bool `json:"noColor,omitempty" yaml:"noColor,omitempty" doc:"Disable colored output"`

	// In is the input reader (default: os.Stdin)
	In io.Reader `json:"-" yaml:"-"`

	// Out is the output writer (default: os.Stdout)
	Out io.Writer `json:"-" yaml:"-"`

	// Writer is the scafctl CLI writer for debug output (used by debug.out CEL function)
	Writer *writer.Writer `json:"-" yaml:"-"`

	// Expression is a CEL expression to filter/transform data before display
	Expression string `json:"expression,omitempty" yaml:"expression,omitempty" doc:"CEL expression to filter/transform output data" example:"_.database" maxLength:"4096"`

	// Where is a per-item CEL boolean filter applied to list data
	Where string `json:"where,omitempty" yaml:"where,omitempty" doc:"Per-item CEL filter for list data" example:"_.enabled" maxLength:"4096"`

	// Interactive enables the interactive TUI mode
	Interactive bool `json:"interactive,omitempty" yaml:"interactive,omitempty" doc:"Launch interactive TUI for data exploration"`

	// HelpTitle is a custom help title for interactive mode
	HelpTitle string `json:"helpTitle,omitempty" yaml:"helpTitle,omitempty" doc:"Custom help title for TUI" example:"Resolver Results Viewer" maxLength:"100"`

	// HelpLines are custom help text lines for interactive mode
	HelpLines []string `json:"helpLines,omitempty" yaml:"helpLines,omitempty" doc:"Custom help text lines for TUI" maxItems:"20"`

	// Theme is the color theme for interactive mode (dark, warm, cool, midnight)
	Theme string `json:"theme,omitempty" yaml:"theme,omitempty" doc:"Color theme for TUI" example:"dark" maxLength:"20"`

	// Layout controls the non-interactive rendering layout.
	// Valid values: "auto" (default - kvx decides), "table" (force bordered table), "list" (force list view).
	Layout string `json:"layout,omitempty" yaml:"layout,omitempty" doc:"Rendering layout for non-interactive display" example:"auto" maxLength:"10"`

	// InitialExpr is an initial expression to evaluate when launching TUI
	InitialExpr string `json:"initialExpr,omitempty" yaml:"initialExpr,omitempty" doc:"Initial CEL expression to evaluate in TUI" maxLength:"4096"`

	// ColumnOrder specifies the preferred display order of columns for table rendering.
	// Fields not listed are appended in their natural order.
	ColumnOrder []string `json:"columnOrder,omitempty" yaml:"columnOrder,omitempty" doc:"Preferred column display order for table rendering"`

	// ColumnHints provides per-column display customizations (header rename, max width, alignment, visibility).
	// Use SchemaJSON for a declarative alternative derived from a JSON Schema.
	ColumnHints map[string]tui.ColumnHint `json:"-" yaml:"-" doc:"Per-column display hints"`

	// SchemaJSON is a JSON Schema document used to derive column display hints.
	// Parsed via tui.ParseSchema; any ColumnHints set directly take precedence
	// over schema-derived hints on a per-key basis. Supports title (header rename),
	// maxLength (max width), deprecated (hidden), type integer/number (right-align),
	// and required (priority boost).
	SchemaJSON []byte `json:"-" yaml:"-" doc:"JSON Schema for deriving column display hints"`

	// DisplaySchemaJSON is a JSON Schema document with x-kvx-* vendor extensions
	// that control the interactive TUI's card-list and detail view rendering.
	// Parsed via tui.ParseSchemaWithDisplay for use in interactive mode.
	// When set, the TUI renders arrays as a scrollable card list with sectioned
	// detail views instead of the default KEY/VALUE table.
	DisplaySchemaJSON []byte `json:"-" yaml:"-" doc:"JSON Schema with x-kvx-* extensions for rich TUI rendering"`
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

// WithAppName sets the application name shown in TUI
func WithAppName(name string) Option {
	return func(o *ViewerOptions) { o.AppName = name }
}

// WithDimensions sets the terminal dimensions
func WithDimensions(width, height int) Option {
	return func(o *ViewerOptions) {
		o.Width = width
		o.Height = height
	}
}

// WithNoColor disables colors in output
func WithNoColor(noColor bool) Option {
	return func(o *ViewerOptions) { o.NoColor = noColor }
}

// WithIO sets custom input/output streams
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

// WithTheme sets the color theme for TUI
func WithTheme(theme string) Option {
	return func(o *ViewerOptions) { o.Theme = theme }
}

// WithLayout sets the rendering layout for non-interactive display.
// Valid values: "auto" (default), "table", "list", "tree", "mermaid".
func WithLayout(layout string) Option {
	return func(o *ViewerOptions) { o.Layout = layout }
}

// WithWhere sets a per-item CEL boolean filter for list data.
func WithWhere(where string) Option {
	return func(o *ViewerOptions) { o.Where = where }
}

// WithInitialExpr sets an initial expression for TUI
func WithInitialExpr(expr string) Option {
	return func(o *ViewerOptions) { o.InitialExpr = expr }
}

// WithColumnOrder sets the preferred column order for table rendering.
func WithColumnOrder(order []string) Option {
	return func(o *ViewerOptions) { o.ColumnOrder = order }
}

// WithColumnHints sets per-column display hints (header rename, max width, alignment, visibility).
// For a declarative alternative, use WithSchemaJSON.
func WithColumnHints(hints map[string]tui.ColumnHint) Option {
	return func(o *ViewerOptions) { o.ColumnHints = hints }
}

// WithSchemaJSON sets a JSON Schema document used to derive column display hints.
// The schema is parsed via tui.ParseSchema. Any ColumnHints set directly take
// precedence over schema-derived hints on a per-key basis.
func WithSchemaJSON(schema []byte) Option {
	return func(o *ViewerOptions) { o.SchemaJSON = schema }
}

// WithDisplaySchemaJSON sets a JSON Schema document with x-kvx-* vendor extensions
// that control the interactive TUI's card-list and detail view rendering.
// The schema is parsed via tui.ParseSchemaWithDisplay. Column hints derived from
// the schema are merged with any programmatic ColumnHints (programmatic take precedence).
func WithDisplaySchemaJSON(schema []byte) Option {
	return func(o *ViewerOptions) { o.DisplaySchemaJSON = schema }
}

// WithContext sets the context for CEL expression evaluation.
// This enables context-dependent features like debug.out when Writer is in context.
func WithContext(ctx context.Context) Option {
	return func(o *ViewerOptions) { o.Ctx = ctx }
}

// View displays data using kvx (table or interactive based on options).
// If Expression is set, it filters/transforms the data before display.
// If Interactive is true, launches the TUI; otherwise renders a static table.
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

	// Apply per-item Where filter before Expression (consistent with writeStructured).
	root, err = applyWhereFilter(options.Where, root)
	if err != nil {
		return err
	}

	// Apply CEL expression filter if provided
	if options.Expression != "" {
		ctx := options.Ctx
		if ctx == nil {
			ctx = context.Background()
		}
		root, err = EvaluateWithScafctlCEL(ctx, options.Expression, root)
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

	// Non-interactive: render based on layout
	switch options.Layout {
	case "list":
		return renderList(root, options)
	case "tree":
		return renderTree(root, options)
	case "mermaid":
		return renderMermaid(root, options)
	default:
		// "auto", "table", and empty all use table rendering.
		// The upstream tui.RenderTable with ColumnarMode "auto" (default)
		// already handles smart layout detection for arrays vs objects.
		return renderTable(root, options)
	}
}

// renderTable outputs data as a bordered table (non-interactive).
func renderTable(root any, options *ViewerOptions) error {
	hints := resolveColumnHints(options.SchemaJSON, options.ColumnHints)
	output := tui.RenderTable(root, tui.TableOptions{
		AppName:     options.AppName,
		Path:        "_",
		Bordered:    true,
		Width:       options.Width,
		NoColor:     options.NoColor,
		ColumnOrder: options.ColumnOrder,
		ColumnHints: hints,
	})
	fmt.Fprint(options.Out, output)
	return nil
}

// resolveColumnHints merges schema-derived hints with programmatic hints.
// Programmatic hints take precedence over schema-derived ones on a per-key basis.
func resolveColumnHints(schemaJSON []byte, programmatic map[string]tui.ColumnHint) map[string]tui.ColumnHint {
	if len(schemaJSON) == 0 && len(programmatic) == 0 {
		return nil
	}

	var merged map[string]tui.ColumnHint

	if len(schemaJSON) > 0 {
		parsed, err := tui.ParseSchema(schemaJSON)
		if err == nil {
			merged = parsed
		}
	}

	if len(programmatic) > 0 {
		if merged == nil {
			return programmatic
		}
		for k, v := range programmatic {
			merged[k] = v
		}
	}

	return merged
}

// renderList outputs data as a key-value list (non-interactive).
func renderList(root any, options *ViewerOptions) error {
	output := tui.RenderList(root, options.NoColor)
	fmt.Fprint(options.Out, output)
	return nil
}

// renderTree outputs data as an ASCII tree structure.
func renderTree(root any, options *ViewerOptions) error {
	output := tui.Render(root, tui.FormatTree, tui.TableOptions{
		NoColor: options.NoColor,
	})
	fmt.Fprint(options.Out, output)
	return nil
}

// renderMermaid outputs data as a Mermaid flowchart diagram.
func renderMermaid(root any, options *ViewerOptions) error {
	output := tui.Render(root, tui.FormatMermaid, tui.TableOptions{
		NoColor: options.NoColor,
	})
	fmt.Fprint(options.Out, output)
	return nil
}

// runInteractive launches the kvx TUI
func runInteractive(root any, options *ViewerOptions) error {
	// Set up scafctl's CEL provider for the TUI
	if err := SetupScafctlCELProvider(options.Writer); err != nil {
		return fmt.Errorf("failed to setup CEL provider: %w", err)
	}

	cfg, err := buildTUIConfig(options)
	if err != nil {
		return err
	}

	teaOpts := make([]tea.ProgramOption, 0)
	if options.In != nil && options.Out != nil {
		teaOpts = append(teaOpts, tui.WithIO(options.In, options.Out)...)
	}

	return tui.Run(root, cfg, teaOpts...)
}

// IsTerminal checks if the writer is a terminal (TTY).
// Returns true if the writer supports interactive output.
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

// RenderTable renders data as a non-interactive table string.
// This is useful when you need the table output as a string rather than writing to a stream.
func RenderTable(data any, opts tui.TableOptions) (string, error) {
	root, err := core.LoadObject(data)
	if err != nil {
		return "", fmt.Errorf("failed to load data: %w", err)
	}

	return tui.RenderTable(root, opts), nil
}

// RenderList renders data as a non-interactive list string.
// This is useful when you need the list output as a string rather than writing to a stream.
func RenderList(data any, noColor bool) (string, error) {
	root, err := core.LoadObject(data)
	if err != nil {
		return "", fmt.Errorf("failed to load data: %w", err)
	}

	return tui.RenderList(root, noColor), nil
}

// Snapshot renders a non-interactive snapshot of the TUI and returns it as a string.
// This produces the same visual output as interactive mode but without blocking for input,
// making it suitable for tests and non-TTY environments.
func Snapshot(data any, opts ...Option) (string, error) {
	options := DefaultViewerOptions()
	for _, opt := range opts {
		opt(options)
	}

	root, err := core.LoadObject(data)
	if err != nil {
		return "", fmt.Errorf("failed to load data: %w", err)
	}

	// Apply per-item Where filter before Expression (consistent with writeStructured).
	root, err = applyWhereFilter(options.Where, root)
	if err != nil {
		return "", err
	}

	// Apply CEL expression filter if provided
	if options.Expression != "" {
		ctx := options.Ctx
		if ctx == nil {
			ctx = context.Background()
		}
		root, err = EvaluateWithScafctlCEL(ctx, options.Expression, root)
		if err != nil {
			return "", fmt.Errorf("expression evaluation failed: %w", err)
		}
	}

	cfg, cfgErr := buildTUIConfig(options)
	if cfgErr != nil {
		return "", fmt.Errorf("failed to build TUI config: %w", cfgErr)
	}
	cfg.HideFooter = true
	return tui.RenderSnapshot(root, cfg), nil
}

// buildTUIConfig creates a tui.Config from ViewerOptions.
// This is shared between runInteractive and Snapshot to avoid duplication.
func buildTUIConfig(options *ViewerOptions) (tui.Config, error) {
	cfg := tui.DefaultConfig()
	cfg.AppName = options.AppName
	cfg.NoColor = options.NoColor
	cfg.Width = options.Width
	cfg.Height = options.Height

	if options.Theme != "" {
		cfg.ThemeName = options.Theme
	}
	if options.HelpTitle != "" {
		cfg.HelpAboutTitle = options.HelpTitle
	}
	if len(options.HelpLines) > 0 {
		cfg.HelpAboutLines = options.HelpLines
	}
	if options.InitialExpr != "" {
		cfg.InitialExpr = options.InitialExpr
	}

	// Apply display schema (x-kvx-* extensions) for rich card-list and detail views.
	// Also merge any derived column hints with programmatic hints.
	if len(options.DisplaySchemaJSON) > 0 {
		schemaHints, displaySchema, err := tui.ParseSchemaWithDisplay(options.DisplaySchemaJSON)
		if err != nil {
			return tui.Config{}, fmt.Errorf("failed to parse display schema: %w", err)
		}
		if displaySchema != nil {
			cfg.DisplaySchema = displaySchema
		}
		// Merge schema-derived hints: programmatic hints take precedence
		if len(schemaHints) > 0 {
			merged := make(map[string]tui.ColumnHint, len(schemaHints))
			for k, v := range schemaHints {
				merged[k] = v
			}
			for k, v := range options.ColumnHints {
				merged[k] = v
			}
			options.ColumnHints = merged
		}
	}

	return cfg, nil
}
