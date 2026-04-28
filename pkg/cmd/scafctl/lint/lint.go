// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package lint provides the lint command for validating solutions.
// Business logic has been extracted to pkg/lint for reuse across
// CLI, MCP, and future API layers.
package lint

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/term"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/kvx/pkg/tui"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/cmd/flags"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	pkglint "github.com/oakwood-commons/scafctl/pkg/lint"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/solutionprovider"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/get"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/kvx"
	"github.com/oakwood-commons/scafctl/pkg/terminal/output"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// Type aliases re-exporting from pkg/lint for backward compatibility.
// Callers that import this package continue to work without modification.
type (
	SeverityLevel = pkglint.SeverityLevel
	Finding       = pkglint.Finding
	Result        = pkglint.Result
)

// Severity constant re-exports.
const (
	SeverityError   = pkglint.SeverityError
	SeverityWarning = pkglint.SeverityWarning
	SeverityInfo    = pkglint.SeverityInfo
)

// Solution delegates to pkg/lint.Solution.
func Solution(sol *solution.Solution, filePath string, registry *provider.Registry) *Result {
	return pkglint.Solution(sol, filePath, registry)
}

// FilterBySeverity delegates to pkg/lint.FilterBySeverity.
func FilterBySeverity(result *Result, minSeverity string) *Result {
	return pkglint.FilterBySeverity(result, minSeverity)
}

// Options holds command flags and settings.
type Options struct {
	BinaryName string
	File       string
	Output     string
	Severity   string
	Expression string
	CliParams  *settings.Run
	IOStreams  *terminal.IOStreams
}

// CommandLint creates the lint command.
func CommandLint(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
	options := &Options{
		CliParams: cliParams,
		IOStreams: ioStreams,
	}

	cmd := &cobra.Command{
		Use:     "lint [name[@version]]",
		Aliases: []string{"l", "check"},
		Short:   "Lint a solution file for issues and best practices",
		Long: heredoc.Doc(`
			Analyze a solution file for potential issues, anti-patterns, and best practices.

			LINT RULES:
			  Errors (will cause execution failures):
			    - unused-resolver          Resolver defined but never referenced
			    - invalid-dependency       Action depends on non-existent action
			    - missing-provider         Referenced provider not registered
			    - invalid-expression       Invalid CEL expression syntax
			    - invalid-template         Invalid Go template syntax
			    - unbundled-test-file      Test file not covered by bundle.include
			    - invalid-test-name        Test name does not match naming pattern
			    - schema-violation         Solution YAML violates the JSON Schema
			    - unknown-provider-input   Input key not declared in provider schema
			    - invalid-provider-input-type  Literal input value violates provider schema type

			  Warnings (may cause problems):
			    - empty-workflow       Workflow defined but no actions
			    - finally-with-foreach forEach not allowed in finally actions
			    - unused-template      Test template not referenced by any extends

			  Info (suggestions):
			    - missing-description  Action/resolver lacks description
			    - long-timeout        Timeout exceeds recommended maximum
			    - unused-finally      Finally actions with no regular actions

			OUTPUT FORMATS:
			  table   Human-readable table (default)
			  json    JSON output for tooling integration
			  yaml    YAML output
			  quiet   Exit code only (0=clean, 1=issues found)
		`),
		Example: strings.ReplaceAll(heredoc.Doc(`
			# Lint a solution file
			scafctl lint -f ./solution.yaml

			# Show only errors (skip warnings and info)
			scafctl lint -f ./solution.yaml --severity error

			# Output as JSON for CI integration
			scafctl lint -f ./solution.yaml -o json
		`), settings.CliBinaryName, cliParams.BinaryName),
		Args: cobra.MaximumNArgs(1),
		PreRunE: func(_ *cobra.Command, args []string) error {
			if len(args) > 0 {
				if err := get.ValidatePositionalRef(args[0], options.File, cliParams.BinaryName+" lint"); err != nil {
					return err
				}
				options.File = args[0]
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			cliParams.EntryPointSettings.Path = filepath.Join(path, cmd.Name())
			ctx := settings.IntoContext(cmd.Context(), cliParams)
			lgr := logger.FromContext(cmd.Context())
			ctx = logger.WithLogger(ctx, lgr)

			options.BinaryName = cliParams.BinaryName

			return runLint(ctx, options)
		},
		SilenceUsage: true,
	}

	cmd.Flags().StringVarP(&options.File, "file", "f", "", "Solution file path (auto-discovered if not provided, use '-' for stdin)")
	cmd.Flags().StringVarP(&options.Output, "output", "o", "table", "Output format: table, json, yaml, quiet")
	cmd.Flags().StringVarP(&options.Expression, "expression", "e", "", "CEL expression to filter/transform output data (e.g., '_.findings')")
	cmd.Flags().StringVar(&options.Severity, "severity", "info", "Minimum severity to report: error, warning, info")

	lintPath := fmt.Sprintf("%s/%s", path, cmd.Use)
	cmd.AddCommand(CommandRules(cliParams, ioStreams, lintPath))
	cmd.AddCommand(CommandExplainRule(cliParams, ioStreams, lintPath))

	return cmd
}

// findingsColumnHints builds column hints with a message width that fills the
// available terminal width. Fixed columns (severity, location, rule) use static
// caps; the message column expands to consume the remaining space.
func findingsColumnHints(w io.Writer) map[string]tui.ColumnHint {
	msgWidth := maxMessageWidth
	if tw := termWidth(w); tw > 0 {
		// Subtract fixed columns and overhead. The overhead covers borders,
		// the row-number column, and inter-column gaps.
		const fixedOverhead = 6
		const minMsgWidth = 16
		remaining := tw - 8 - maxLocationWidth - maxRuleWidth - fixedOverhead
		if remaining < minMsgWidth {
			remaining = minMsgWidth
		}
		if remaining != msgWidth {
			msgWidth = remaining
		}
	}
	return map[string]tui.ColumnHint{
		"severity": {MaxWidth: 8, DisplayName: "Severity"},
		"location": {MaxWidth: maxLocationWidth, DisplayName: "Location"},
		"message":  {MaxWidth: msgWidth, DisplayName: "Message"},
		"ruleName": {MaxWidth: maxRuleWidth, DisplayName: "Rule"},
	}
}

// termWidth returns the terminal width for the given writer, or 0 if unknown.
func termWidth(w io.Writer) int {
	if f, ok := w.(*os.File); ok {
		fd := f.Fd()
		if width, _, err := term.GetSize(int(fd)); err == nil && width > 0 { //nolint:gosec // fd is a valid file descriptor, not user input
			return width
		}
	}
	return 0
}

func runLint(ctx context.Context, opts *Options) error {
	if opts.BinaryName == "" {
		opts.BinaryName = settings.CliBinaryName
	}

	lgr := logger.FromContext(ctx)

	// Set up getter with catalog resolver for bare name resolution
	var getterOpts []get.Option
	localCatalog, err := catalog.NewLocalCatalog(*lgr)
	if err == nil {
		resolver := catalog.NewSolutionResolver(localCatalog, *lgr,
			catalog.WithResolverRemoteCatalogs(catalog.RemoteCatalogsFromContext(ctx, *lgr)),
		)
		getterOpts = append(getterOpts, get.WithCatalogResolver(resolver))
	} else {
		lgr.V(1).Info("catalog not available for solution resolution", "error", err)
	}

	getter := get.NewGetterFromContext(ctx, getterOpts...)

	// Emit verbose discovery information
	if w := writer.FromContext(ctx); w != nil && w.VerboseEnabled() {
		switch opts.File {
		case "":
			binaryName := settings.CliBinaryName
			if opts.CliParams != nil && opts.CliParams.BinaryName != "" {
				binaryName = opts.CliParams.BinaryName
			}
			w.Verbosef("Auto-discovering solution (binary=%s)", binaryName)
			w.Verbosef("  Search folders: %v", settings.SolutionFoldersFor(binaryName))
			w.Verbosef("  Search filenames: %v", settings.SolutionFileNamesFor(binaryName))
		case "-":
			w.Verbose("Loading solution from stdin")
		default:
			w.Verbosef("Loading solution from: %s", opts.File)
		}
	}

	sol, err := getter.Get(ctx, opts.File)
	if err != nil {
		writeError(opts, fmt.Sprintf("failed to load solution: %v", err))
		return exitcode.WithCode(err, exitcode.FileNotFound)
	}

	if w := writer.FromContext(ctx); w != nil && w.VerboseEnabled() {
		w.Verbosef("Solution loaded: %s (version=%s)", sol.Metadata.Name, sol.Metadata.Version)
	}

	lgr.V(1).Info("linting solution", "file", opts.File, "name", sol.Metadata.Name)

	registry := getRegistry(ctx)
	result := pkglint.Solution(sol, opts.File, registry)
	result = pkglint.FilterBySeverity(result, opts.Severity)

	if opts.Output == "quiet" {
		if result.ErrorCount > 0 {
			return exitcode.WithCode(fmt.Errorf("found %d errors", result.ErrorCount), exitcode.ValidationFailed)
		}
		return nil
	}

	kvxOpts := flags.NewKvxOutputOptionsFromFlags(
		opts.Output,
		false,
		opts.Expression,
		kvx.WithOutputContext(ctx),
		kvx.WithOutputNoColor(opts.CliParams.NoColor),
		kvx.WithOutputAppName(opts.BinaryName+" lint"),
		kvx.WithOutputColumnHints(findingsColumnHints(opts.IOStreams.Out)),
		kvx.WithOutputColumnOrder([]string{"severity", "location", "message", "ruleName"}),
	)
	kvxOpts.IOStreams = opts.IOStreams

	// For table output, project findings to the four visible columns so
	// the columnar renderer sees exactly 4 fields (not the full 9-field
	// struct) and stays in table mode at narrower terminal widths.
	// For structured formats (json/yaml), emit the full result with all
	// fields and summary counts.
	var outputData any = result
	if !kvx.IsStructuredFormat(kvxOpts.Format) && opts.Expression == "" {
		if len(result.Findings) == 0 {
			w := writer.FromContext(ctx)
			w.Success("No lint issues found.")
			return nil
		}
		outputData = projectFindings(result.Findings)
	}

	if err := kvxOpts.Write(outputData); err != nil {
		writeError(opts, fmt.Sprintf("failed to write output: %v", err))
		return exitcode.WithCode(err, exitcode.GeneralError)
	}

	if result.ErrorCount > 0 {
		return exitcode.WithCode(fmt.Errorf("found %d errors", result.ErrorCount), exitcode.ValidationFailed)
	}

	return nil
}

// projectFindings converts findings to maps with only the four table-visible
// columns. This keeps the column count low so kvx renders a columnar table
// instead of falling back to list view at narrow terminal widths.
// Returns []any so kvx View() recognises the data as a homogeneous array.
func projectFindings(findings []*pkglint.Finding) []any {
	rows := make([]any, len(findings))
	for i, f := range findings {
		rows[i] = map[string]any{
			"severity": string(f.Severity),
			"location": truncate(f.Location, maxLocationWidth),
			"message":  f.Message,
			"ruleName": truncate(f.RuleName, maxRuleWidth),
		}
	}
	return rows
}

// Column width limits for table rendering. Values are chosen so the four
// visible columns (severity≤7 + location + message + rule + separators)
// fit comfortably in an 80-column terminal.
const (
	maxLocationWidth = 15
	maxMessageWidth  = 32
	maxRuleWidth     = 12
)

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

func getRegistry(ctx context.Context) *provider.Registry {
	reg, err := builtin.DefaultRegistry(ctx)
	if err != nil {
		reg = provider.GetGlobalRegistry()
	}
	// The solution provider is not part of DefaultRegistry because it has a
	// circular dependency on the registry itself and requires a loader. For
	// lint purposes we only need the provider to be *registered* (so the
	// missing-provider rule doesn't false-positive); it will never be executed.
	if !reg.Has(solutionprovider.ProviderName) {
		solProvider := solutionprovider.New(
			solutionprovider.WithRegistry(reg),
		)
		_ = reg.Register(solProvider)
	}
	return reg
}

func writeError(opts *Options, msg string) {
	output.NewWriteMessageOptions(
		opts.IOStreams,
		output.MessageTypeError,
		opts.CliParams.NoColor,
		opts.CliParams.ExitOnError,
	).WriteMessage(msg)
}
