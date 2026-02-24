// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
	yaml "gopkg.in/yaml.v3"
)

// CELOptions holds options for the eval cel command.
type CELOptions struct {
	IOStreams  *terminal.IOStreams
	CliParams  *settings.Run
	Output     string
	Expression string
	Vars       []string
	Data       string
	File       string
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
		RunE: func(cmd *cobra.Command, _ []string) error {
			cliParams.EntryPointSettings.Path = filepath.Join(path, cmd.Use)
			ctx := settings.IntoContext(context.Background(), cliParams)

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
	cCmd.Flags().StringVarP(&opts.Output, "output", "o", "auto", "Output format: auto, json, yaml")

	_ = cCmd.MarkFlagRequired("expression")

	return cCmd
}

// Run executes the eval cel command.
func (o *CELOptions) Run(ctx context.Context) error {
	w := writer.MustFromContext(ctx)

	// Build root data from --data or --file
	rootData, err := buildDataContext(o.Data, o.File)
	if err != nil {
		w.Errorf("failed to build data context: %v", err)
		return exitcode.WithCode(err, exitcode.InvalidInput)
	}

	// Parse --var flags into additional variables
	vars, err := parseVars(o.Vars)
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

	// Handle structured output formats
	if o.Output == "json" || o.Output == "yaml" {
		return writeStructured(o.IOStreams, celResult, o.Output)
	}

	// Table output
	w.Plainf("%v\n", result)

	return nil
}

// buildDataContext creates a data context map from --data or --file flags.
func buildDataContext(data, file string) (any, error) {
	if data != "" && file != "" {
		return nil, fmt.Errorf("cannot use both --data and --file")
	}

	if data == "" && file == "" {
		return nil, nil
	}

	var raw []byte
	if data != "" {
		raw = []byte(data)
	} else {
		var err error
		raw, err = os.ReadFile(file)
		if err != nil {
			return nil, fmt.Errorf("reading file %s: %w", file, err)
		}
	}

	// Try JSON first, then YAML
	var result any
	if err := json.Unmarshal(raw, &result); err != nil {
		if err2 := yaml.Unmarshal(raw, &result); err2 != nil {
			return nil, fmt.Errorf("data is not valid JSON or YAML: %w", err)
		}
	}

	return result, nil
}

// parseVars converts key=value pairs into a map.
func parseVars(vars []string) (map[string]any, error) {
	if len(vars) == 0 {
		return nil, nil
	}

	result := make(map[string]any, len(vars))
	for _, v := range vars {
		parts := strings.SplitN(v, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid variable format %q; expected key=value", v)
		}
		key := strings.TrimSpace(parts[0])
		value := parts[1]

		// Try to parse as JSON for complex values
		var parsed any
		if err := json.Unmarshal([]byte(value), &parsed); err == nil {
			result[key] = parsed
		} else {
			result[key] = value
		}
	}

	return result, nil
}

// writeStructured writes data as JSON or YAML to the output stream.
func writeStructured(ioStreams *terminal.IOStreams, data any, format string) error {
	switch format {
	case "json":
		enc := json.NewEncoder(ioStreams.Out)
		enc.SetIndent("", "  ")
		return enc.Encode(data)
	case "yaml":
		enc := yaml.NewEncoder(ioStreams.Out)
		enc.SetIndent(2)
		err := enc.Encode(data)
		_ = enc.Close()
		return err
	default:
		return fmt.Errorf("unsupported output format %q; use json or yaml", format)
	}
}
