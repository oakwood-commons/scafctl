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

	// Interactive enables the interactive TUI mode
	Interactive bool `json:"interactive,omitempty" yaml:"interactive,omitempty" doc:"Launch interactive TUI for data exploration"`

	// HelpTitle is a custom help title for interactive mode
	HelpTitle string `json:"helpTitle,omitempty" yaml:"helpTitle,omitempty" doc:"Custom help title for TUI" example:"Resolver Results Viewer" maxLength:"100"`

	// HelpLines are custom help text lines for interactive mode
	HelpLines []string `json:"helpLines,omitempty" yaml:"helpLines,omitempty" doc:"Custom help text lines for TUI" maxItems:"20"`

	// Theme is the color theme for interactive mode (dark, warm, cool, midnight)
	Theme string `json:"theme,omitempty" yaml:"theme,omitempty" doc:"Color theme for TUI" example:"dark" maxLength:"20"`

	// InitialExpr is an initial expression to evaluate when launching TUI
	InitialExpr string `json:"initialExpr,omitempty" yaml:"initialExpr,omitempty" doc:"Initial CEL expression to evaluate in TUI" maxLength:"4096"`

	// SortKeys enables alphabetical sorting of map keys
	SortKeys bool `json:"sortKeys,omitempty" yaml:"sortKeys,omitempty" doc:"Sort map keys alphabetically in output"`
}

// DefaultViewerOptions returns sensible defaults for scafctl
func DefaultViewerOptions() *ViewerOptions {
	return &ViewerOptions{
		AppName:  "scafctl",
		In:       os.Stdin,
		Out:      os.Stdout,
		SortKeys: true,
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

// WithInitialExpr sets an initial expression for TUI
func WithInitialExpr(expr string) Option {
	return func(o *ViewerOptions) { o.InitialExpr = expr }
}

// WithSortKeys enables alphabetical sorting of map keys
func WithSortKeys(sort bool) Option {
	return func(o *ViewerOptions) { o.SortKeys = sort }
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

	// Apply CEL expression filter if provided
	if options.Expression != "" {
		// Use context from options, or fall back to Background
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

	// Non-interactive: render table
	return renderTable(root, options)
}

// renderTable outputs data as a bordered table (non-interactive).
// For scalar values (string, number, bool), it outputs the value directly instead of a table.
//
// WORKAROUND: This scalar detection logic should ideally be handled in kvx's RenderTable
// function itself. This is documented as a recommendation for the kvx developer.
// See: docs/design/kvx-integration-plan.md (Recommendations Log)
func renderTable(root any, options *ViewerOptions) error {
	// WORKAROUND: Check if the data is a scalar value - output directly without table formatting
	// This should be handled by kvx's RenderTable instead of requiring consumers to implement it.
	if isScalarValue(root) {
		fmt.Fprintln(options.Out, formatScalar(root))
		return nil
	}

	// Configure sort order
	sortOrder := core.SortNone
	if options.SortKeys {
		sortOrder = core.SortAscending
	}

	engine, err := core.New(core.WithSortOrder(sortOrder))
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

// isScalarValue returns true if the value is a simple scalar (string, number, bool, nil)
func isScalarValue(v any) bool {
	if v == nil {
		return true
	}
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

// formatScalar formats a scalar value for direct output
func formatScalar(v any) string {
	if v == nil {
		return "null"
	}
	return fmt.Sprintf("%v", v)
}

// runInteractive launches the kvx TUI
func runInteractive(root any, options *ViewerOptions) error {
	// Set up scafctl's CEL provider for the TUI
	if err := SetupScafctlCELProvider(options.Writer); err != nil {
		return fmt.Errorf("failed to setup CEL provider: %w", err)
	}

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
func RenderTable(data any, noColor bool, keyWidth, valueWidth int) (string, error) {
	root, err := core.LoadObject(data)
	if err != nil {
		return "", fmt.Errorf("failed to load data: %w", err)
	}

	engine, err := core.New(core.WithSortOrder(core.SortAscending))
	if err != nil {
		return "", fmt.Errorf("failed to create engine: %w", err)
	}

	return engine.RenderTable(root, noColor, keyWidth, valueWidth), nil
}
