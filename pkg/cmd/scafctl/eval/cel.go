// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package eval

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/cmd/flags"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/kvx"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// CELOptions holds options for the eval cel command.
type CELOptions struct {
	IOStreams      *terminal.IOStreams
	CliParams      *settings.Run
	KvxOutputFlags flags.KvxOutputFlags
	Expression     string
	Vars           []string
	Data           string
	File           string
}

// CELResult holds the result of evaluating a CEL expression.
type CELResult struct {
	Expression string `json:"expression" yaml:"expression" doc:"The CEL expression that was evaluated"`
	Result     any    `json:"result" yaml:"result" doc:"The evaluation result"`
	Type       string `json:"type" yaml:"type" doc:"The Go type of the result"`
}

// CommandCEL creates the 'eval cel' command.
func CommandCEL(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
	opts := &CELOptions{}

	cCmd := &cobra.Command{
		Use:     "cel",
		Aliases: []string{"c"},
		Short:   "Evaluate a CEL expression",
		Long: heredoc.Doc(`
			Evaluate a CEL expression with optional data context.

			Provide variables via --var flags (key=value pairs) or structured
			data via --data (inline JSON) or --file (JSON/YAML file). File data
			is available as the root object "_" in the expression.

			Examples:
			  # Simple variable evaluation
			  scafctl eval cel --expression 'size(name) > 3' -v name=hello

			  # With inline JSON data
			  scafctl eval cel --expression 'items.filter(i, i.active)' \
			    -d '{"items": [{"name": "a", "active": true}]}'

			  # With data from a file
			  scafctl eval cel --expression 'has(config.timeout)' --file config.json

			  # Output as JSON
			  scafctl eval cel --expression '1 + 2' -o json
		`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cliParams.EntryPointSettings.Path = filepath.Join(path, cmd.Use)
			ctx := settings.IntoContext(cmd.Context(), cliParams)

			if lgr := logger.FromContext(cmd.Context()); lgr != nil {
				ctx = logger.WithLogger(ctx, lgr)
			}

			w := writer.FromContext(cmd.Context())
			if w == nil {
				w = writer.New(ioStreams, cliParams)
			}
			ctx = writer.WithWriter(ctx, w)

			opts.IOStreams = ioStreams
			opts.CliParams = cliParams

			return opts.Run(ctx)
		},
		SilenceUsage: true,
	}

	cCmd.Flags().StringVar(&opts.Expression, "expression", "", "CEL expression to evaluate (required)")
	cCmd.Flags().StringArrayVarP(&opts.Vars, "var", "v", nil, "Variable as key=value (repeatable)")
	cCmd.Flags().StringVar(&opts.Data, "data", "", "Inline JSON data context")
	cCmd.Flags().StringVar(&opts.File, "file", "", "JSON/YAML file for data context")
	flags.AddKvxOutputFormatFlagToStructWithFormats(cCmd, &opts.KvxOutputFlags, dataOutputFormats)

	_ = cCmd.MarkFlagRequired("expression")

	return cCmd
}

// Run executes the eval cel command.
func (o *CELOptions) Run(ctx context.Context) error {
	w := writer.FromContext(ctx)
	if w == nil {
		return fmt.Errorf("writer not initialized in context")
	}

	// Build root data from --data or --file
	rootData, err := celexp.BuildDataContext(o.Data, o.File)
	if err != nil {
		w.Errorf("failed to build data context: %v", err)
		return exitcode.WithCode(err, exitcode.InvalidInput)
	}

	// Parse --var flags into additional variables
	vars, err := celexp.ParseVars(o.Vars)
	if err != nil {
		w.Errorf("failed to parse variables: %v", err)
		return exitcode.WithCode(err, exitcode.InvalidInput)
	}

	// Evaluate the expression
	result, err := celexp.EvaluateExpression(ctx, o.Expression, rootData, vars)
	if err != nil {
		w.Errorf("CEL evaluation failed: %v", err)
		return exitcode.WithCode(err, exitcode.GeneralError)
	}

	celResult := &CELResult{
		Expression: o.Expression,
		Result:     result,
		Type:       fmt.Sprintf("%T", result),
	}

	kvxOpts := flags.ToKvxOutputOptions(&o.KvxOutputFlags, kvx.WithIOStreams(o.IOStreams))
	// auto keeps the bespoke human output (just the value); json/yaml/text/quiet
	// render the CELResult wrapper through kvx.
	if kvxOpts.Format != kvx.OutputFormatAuto {
		return kvxOpts.Write(celResult)
	}

	w.Plainf("%v\n", result)

	return nil
}
