// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package lint provides the lint command for validating solutions.
// Business logic has been extracted to pkg/lint for reuse across
// CLI, MCP, and future API layers.
package lint

import (
	"context"
	_ "embed"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/kvx/pkg/tui"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/cmd/flags"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	pkglint "github.com/oakwood-commons/scafctl/pkg/lint"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/get"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/kvx"
	"github.com/oakwood-commons/scafctl/pkg/terminal/output"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

//go:embed lint_schema.json
var lintSchemaJSON []byte

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
	BinaryName     string
	File           string
	Severity       string
	KvxOutputFlags flags.KvxOutputFlags
	CliParams      *settings.Run
	IOStreams      *terminal.IOStreams
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
		Short:   "Report authoring warnings and best practices (the advisory subset of 'validate')",
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
			    - undefined-optional-reference  Optional CEL ref (_.?name) targets an undefined resolver

			SOME OUTPUT FORMATS:
			  table   Human-readable bordered table
			  json    JSON output for tooling integration
			  yaml    YAML output
			  quiet   Exit code only (0=clean, 1=issues found)

			  Use -o to select a format, -i for interactive exploration,
			  -e for CEL expression filtering, -w for per-finding filters.
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
	cmd.Flags().StringVar(&options.Severity, "severity", "info", "Minimum severity to report: error, warning, info")
	flags.AddKvxOutputFlagsToStruct(cmd, &options.KvxOutputFlags)

	lintPath := fmt.Sprintf("%s/%s", path, cmd.Use)
	cmd.AddCommand(CommandRules(cliParams, ioStreams, lintPath))
	cmd.AddCommand(CommandRule(cliParams, ioStreams, lintPath))
	cmd.AddCommand(CommandExplainRuleDeprecated(cliParams, ioStreams, lintPath))

	return cmd
}

// findingsColumnHints returns column display hints for the findings table.
// Fixed columns (severity, rule, location) use static width caps; the
// message column is marked Flex so it absorbs remaining terminal width.
func findingsColumnHints() map[string]tui.ColumnHint {
	return map[string]tui.ColumnHint{
		"severity":   {MaxWidth: 8, DisplayName: "Severity"},
		"ruleName":   {MaxWidth: maxRuleWidth, DisplayName: "Rule"},
		"location":   {MaxWidth: maxLocationWidth, DisplayName: "Location"},
		"message":    {DisplayName: "Message", Flex: true},
		"category":   {Hidden: true},
		"suggestion": {Hidden: true},
		"sourceFile": {Hidden: true},
		"line":       {Hidden: true},
		"column":     {Hidden: true},
	}
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

	// Unified resolution chain: -f > positional arg > auto-discover
	resolvedPath, resolveErr := get.Resolve(ctx, getter, opts.File, "", get.ResolveOptions{
		Risk: get.DiscoveryRiskLow,
	})
	if resolveErr != nil {
		writeError(opts, fmt.Sprintf("failed to load solution: %v", resolveErr))
		return exitcode.WithCode(resolveErr, exitcode.FileNotFound)
	}
	// Update opts.File so downstream linting reports the correct path.
	opts.File = resolvedPath

	// Emit verbose discovery information
	if w := writer.FromContext(ctx); w != nil && w.VerboseEnabled() {
		switch opts.File {
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

	if opts.KvxOutputFlags.Output == "quiet" {
		if result.ErrorCount > 0 {
			return exitcode.WithCode(fmt.Errorf("found %d errors", result.ErrorCount), exitcode.ValidationFailed)
		}
		return nil
	}

	// Render via the shared renderer so 'scafctl lint', 'validate solution',
	// and 'validate resolver's lint gate all produce identical finding output.
	// runLint applies severity filtering (above) before rendering; the gate
	// callers render unfiltered on purpose.
	if renderErr := RenderResult(ctx, result, opts.BinaryName+" lint", opts.IOStreams, opts.KvxOutputFlags, opts.CliParams.NoColor); renderErr != nil {
		writeError(opts, fmt.Sprintf("failed to write output: %v", renderErr))
		return renderErr
	}

	if result.ErrorCount > 0 {
		return exitcode.WithCode(fmt.Errorf("found %d errors", result.ErrorCount), exitcode.ValidationFailed)
	}

	return nil
}

// RenderResult renders a lint result using the same output path as the 'lint'
// command, so callers (e.g. 'validate solution') produce identical findings
// output. It writes findings to the provided IO streams via kvx and returns an
// error only if writing fails -- it does NOT make a pass/fail decision, leaving
// exit-code policy to the caller. The appName is used for kvx display context
// (e.g. "scafctl validate solution").
//
// When the output format is a human-readable (non-structured) table and there
// are no findings, a success message is emitted and nil is returned.
//
// The 'quiet' format renders NOTHING to stdout (its contract is exit-code
// only); all callers get this behavior consistently through this function.
func RenderResult(
	ctx context.Context,
	result *Result,
	appName string,
	ioStreams *terminal.IOStreams,
	outputFlags flags.KvxOutputFlags,
	noColor bool,
) error {
	if result == nil {
		return nil
	}

	// Quiet is exit-code only: emit no output regardless of findings. The
	// caller is responsible for translating findings into an exit code.
	if outputFlags.Output == "quiet" {
		return nil
	}

	kvxOpts := flags.ToKvxOutputOptions(&outputFlags,
		kvx.WithIOStreams(ioStreams),
		kvx.WithOutputContext(ctx),
		kvx.WithOutputNoColor(noColor),
		kvx.WithOutputAppName(appName),
		kvx.WithOutputDisplaySchemaJSON(lintSchemaJSON),
		kvx.WithOutputColumnHints(findingsColumnHints()),
		kvx.WithOutputColumnOrder([]string{"severity", "ruleName", "location", "message"}),
	)

	var outputData any = result
	if !kvx.IsStructuredFormat(kvxOpts.Format) && outputFlags.Expression == "" {
		if len(result.Findings) == 0 {
			if w := writer.FromContext(ctx); w != nil {
				w.Success("No lint issues found.")
			}
			return nil
		}
		if outputFlags.Interactive {
			outputData = result.Findings
		} else {
			outputData = projectFindings(result.Findings)
		}
	}

	if err := kvxOpts.Write(outputData); err != nil {
		return exitcode.WithCode(err, exitcode.GeneralError)
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
			"location": f.Location,
			"message":  f.Message,
			"ruleName": f.RuleName,
		}
	}
	return rows
}

// Column width limits for table rendering. Values are chosen so the four
// visible columns (severity + rule + location + message + separators)
// fit comfortably in an 80-column terminal. The message column fills
// remaining space and is the first to be truncated on narrow terminals.
const (
	maxRuleWidth     = 20
	maxLocationWidth = 20
)

func getRegistry(ctx context.Context) *provider.Registry {
	reg, err := builtin.DefaultRegistry(ctx)
	if err != nil {
		reg = provider.GetGlobalRegistry()
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
