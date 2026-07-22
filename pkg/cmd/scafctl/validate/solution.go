// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package validate

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/oakwood-commons/scafctl/pkg/cmd/flags"
	cmdlint "github.com/oakwood-commons/scafctl/pkg/cmd/scafctl/lint"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution/get"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	pkgvalidate "github.com/oakwood-commons/scafctl/pkg/validate"

	"github.com/spf13/cobra"
)

// solutionOptions holds flags for the 'validate solution' command.
type solutionOptions struct {
	File           string
	Strict         bool
	KvxOutputFlags flags.KvxOutputFlags
}

// CommandValidateSolution creates the 'validate solution' subcommand -- the
// primary validation gate. It loads a solution and runs lint: lint errors (and
// schema violations, which lint reports) fail; lint warnings are surfaced and,
// with --strict, are also fatal. Schema conformance is covered by lint's
// built-in schema check, so it is not double-reported here.
func CommandValidateSolution(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
	opts := &solutionOptions{}

	binaryName := cliParams.BinaryName
	if binaryName == "" {
		binaryName = settings.CliBinaryName
	}

	cCmd := &cobra.Command{
		Use:     "solution [name[@version]]",
		Aliases: []string{"sol"},
		Short:   fmt.Sprintf("Validate that a %s solution is correct and ready (runs lint)", binaryName),
		Long: strings.ReplaceAll(`Validate a solution and fail on any error. This is the primary gate: it loads
the solution and runs lint, which includes a JSON Schema conformance check.

Lint errors (including schema violations) always fail. Lint warnings are
surfaced but do not fail by default; pass --strict to treat warnings as fatal
too. Use it in CI pipelines or pre-commit checks as a single pass/fail gate.

EXIT CODES:
  0  Solution is valid (no errors; warnings allowed unless --strict)
  2  Validation failed (lint errors, or warnings with --strict)
  4  Solution file not found

Examples:
  # Validate the auto-discovered solution
  scafctl validate solution

  # Validate a specific solution file
  scafctl validate solution -f ./my-solution.yaml

  # Treat warnings as fatal
  scafctl validate solution -f ./my-solution.yaml --strict`, settings.CliBinaryName, binaryName),
		Args: cobra.MaximumNArgs(1),
		PreRunE: func(_ *cobra.Command, args []string) error {
			if len(args) > 0 {
				if err := get.ValidatePositionalRef(args[0], opts.File, binaryName+" validate solution"); err != nil {
					return err
				}
				opts.File = args[0]
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			cliParams.EntryPointSettings.Path = filepath.Join(path, cmd.Name())
			ctx := settings.IntoContext(cmd.Context(), cliParams)
			if lgr := logger.FromContext(cmd.Context()); lgr != nil {
				ctx = logger.WithLogger(ctx, lgr)
			}
			w := writer.FromContext(cmd.Context())
			if w == nil {
				w = writer.New(ioStreams, cliParams)
			}
			ctx = writer.WithWriter(ctx, w)

			return runValidateSolution(ctx, opts, cliParams, ioStreams)
		},
		SilenceUsage: true,
	}

	cCmd.Flags().StringVarP(&opts.File, "file", "f", "", "Solution file path (auto-discovered if not provided)")
	cCmd.Flags().BoolVar(&opts.Strict, "strict", false, "Treat lint warnings as fatal (errors and schema violations are always fatal)")
	flags.AddKvxOutputFlagsToStruct(cCmd, &opts.KvxOutputFlags)

	return cCmd
}

// runValidateSolution orchestrates loading + linting via the domain helper,
// renders findings using the lint command's shared renderer, and applies the
// pass/fail policy. The finding FORMAT is identical to 'scafctl lint' (same
// renderer), but validate is intentionally UNFILTERED: as a gate it shows all
// findings regardless of severity, whereas 'scafctl lint' filters by
// --severity (default info). So the two match at default severity but validate
// deliberately never hides findings.
func runValidateSolution(ctx context.Context, opts *solutionOptions, cliParams *settings.Run, ioStreams *terminal.IOStreams) error {
	registry := defaultRegistry(ctx)

	res, err := pkgvalidate.Solution(ctx, opts.File, registry)
	if err != nil {
		if w := writer.FromContext(ctx); w != nil {
			w.Errorf("%v", err)
		}
		return exitcode.WithCode(err, exitcode.FileNotFound)
	}

	appName := cliParams.BinaryName
	if appName == "" {
		appName = settings.CliBinaryName
	}
	if renderErr := cmdlint.RenderResult(ctx, res.Lint, appName+" validate solution", ioStreams, opts.KvxOutputFlags, cliParams.NoColor); renderErr != nil {
		return renderErr
	}

	if res.Passed(opts.Strict) {
		return nil
	}

	// Failure: distinguish error-driven vs strict-warning-driven failures.
	if res.Lint.ErrorCount > 0 {
		return exitcode.WithCode(
			fmt.Errorf("validation failed: %d error(s)", res.Lint.ErrorCount),
			exitcode.ValidationFailed,
		)
	}
	return exitcode.WithCode(
		fmt.Errorf("validation failed: %d warning(s) (strict mode)", res.Lint.WarnCount),
		exitcode.ValidationFailed,
	)
}

// defaultRegistry returns the builtin provider registry, falling back to the
// global registry so provider-aware lint rules resolve correctly.
func defaultRegistry(ctx context.Context) *provider.Registry {
	reg, err := builtin.DefaultRegistry(ctx)
	if err != nil {
		return provider.GetGlobalRegistry()
	}
	return reg
}
