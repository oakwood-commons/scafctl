// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package lint provides the lint command for validating solutions.
// Business logic has been extracted to pkg/lint for reuse across
// CLI, MCP, and future API layers.
package lint

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/MakeNowJust/heredoc/v2"
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
	File      string
	Output    string
	Severity  string
	CliParams *settings.Run
	IOStreams *terminal.IOStreams
}

// CommandLint creates the lint command.
func CommandLint(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
	options := &Options{
		CliParams: cliParams,
		IOStreams: ioStreams,
	}

	cmd := &cobra.Command{
		Use:     "lint",
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
		Example: heredoc.Doc(`
			# Lint a solution file
			scafctl lint -f ./solution.yaml

			# Show only errors (skip warnings and info)
			scafctl lint -f ./solution.yaml --severity error

			# Output as JSON for CI integration
			scafctl lint -f ./solution.yaml -o json
		`),
		RunE: func(cmd *cobra.Command, _ []string) error {
			cliParams.EntryPointSettings.Path = filepath.Join(path, cmd.Use)
			ctx := settings.IntoContext(context.Background(), cliParams)
			lgr := logger.FromContext(cmd.Context())
			ctx = logger.WithLogger(ctx, lgr)

			return runLint(ctx, options)
		},
		SilenceUsage: true,
	}

	cmd.Flags().StringVarP(&options.File, "file", "f", "", "Solution file path (auto-discovered if not provided, use '-' for stdin)")
	cmd.Flags().StringVarP(&options.Output, "output", "o", "table", "Output format: table, json, yaml, quiet")
	cmd.Flags().StringVar(&options.Severity, "severity", "info", "Minimum severity to report: error, warning, info")

	lintPath := fmt.Sprintf("%s/%s", path, cmd.Use)
	cmd.AddCommand(CommandRules(cliParams, ioStreams, lintPath))
	cmd.AddCommand(CommandExplainRule(cliParams, ioStreams, lintPath))

	return cmd
}

func runLint(ctx context.Context, opts *Options) error {
	lgr := logger.FromContext(ctx)

	// Set up getter with catalog resolver for bare name resolution
	var getterOpts []get.Option
	localCatalog, err := catalog.NewLocalCatalog(*lgr)
	if err == nil {
		resolver := catalog.NewSolutionResolver(localCatalog, *lgr)
		getterOpts = append(getterOpts, get.WithCatalogResolver(resolver))
	} else {
		lgr.V(1).Info("catalog not available for solution resolution", "error", err)
	}

	getter := get.NewGetter(getterOpts...)
	sol, err := getter.Get(ctx, opts.File)
	if err != nil {
		writeError(opts, fmt.Sprintf("failed to load solution: %v", err))
		return exitcode.WithCode(err, exitcode.FileNotFound)
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
		"",
		kvx.WithOutputContext(ctx),
		kvx.WithOutputNoColor(opts.CliParams.NoColor),
		kvx.WithOutputAppName("scafctl lint"),
	)
	kvxOpts.IOStreams = opts.IOStreams

	if err := kvxOpts.Write(result); err != nil {
		writeError(opts, fmt.Sprintf("failed to write output: %v", err))
		return exitcode.WithCode(err, exitcode.GeneralError)
	}

	if result.ErrorCount > 0 {
		return exitcode.WithCode(fmt.Errorf("found %d errors", result.ErrorCount), exitcode.ValidationFailed)
	}

	return nil
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
