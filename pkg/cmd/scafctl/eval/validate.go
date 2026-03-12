// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package eval

import (
	"context"
	"fmt"
	"path/filepath"
	"text/template"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/google/cel-go/cel"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/gotmpl"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// ValidateOptions holds options for the eval validate command.
type ValidateOptions struct {
	IOStreams  *terminal.IOStreams
	CliParams  *settings.Run
	Output     string
	Expression string
	Type       string
}

// ValidateResult holds the result of validating an expression.
type ValidateResult struct {
	Expression string   `json:"expression" yaml:"expression" doc:"The expression that was validated" maxLength:"4096"`
	Type       string   `json:"type" yaml:"type" doc:"The expression type (cel or go-template)" maxLength:"20"`
	Valid      bool     `json:"valid" yaml:"valid" doc:"Whether the expression is syntactically valid"`
	Error      string   `json:"error,omitempty" yaml:"error,omitempty" doc:"Error message if invalid" maxLength:"4096"`
	References []string `json:"references,omitempty" yaml:"references,omitempty" doc:"Referenced variables found in the expression" maxItems:"100"`
}

// CommandValidate creates the 'eval validate' command.
func CommandValidate(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
	opts := &ValidateOptions{}

	cCmd := &cobra.Command{
		Use:   "validate",
		Short: "Validate a CEL expression or Go template for syntax errors",
		Long: heredoc.Doc(`
			Validate the syntax of a CEL expression or Go template without evaluating it.

			This is useful for checking expressions before embedding them in solutions
			or for CI/CD pipelines that validate solution files.

			Examples:
			  # Validate a CEL expression
			  scafctl eval validate --expression 'size(name) > 3' --type cel

			  # Validate a Go template
			  scafctl eval validate --expression '{{ .name }}' --type go-template

			  # Output as JSON
			  scafctl eval validate --expression '{{ .name' --type go-template -o json
		`),
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

	cCmd.Flags().StringVar(&opts.Expression, "expression", "", "Expression to validate (required)")
	cCmd.Flags().StringVar(&opts.Type, "type", "", "Expression type: cel or go-template (required)")
	cCmd.Flags().StringVarP(&opts.Output, "output", "o", "auto", "Output format: auto, json, yaml")

	_ = cCmd.MarkFlagRequired("expression")
	_ = cCmd.MarkFlagRequired("type")

	return cCmd
}

// Run executes the eval validate command.
func (o *ValidateOptions) Run(ctx context.Context) error {
	w := writer.FromContext(ctx)
	if w == nil {
		return fmt.Errorf("writer not initialized in context")
	}

	var result *ValidateResult

	switch o.Type {
	case "cel":
		result = validateCEL(o.Expression)
	case "go-template", "gotemplate", "template":
		result = validateGoTemplate(o.Expression)
	default:
		err := fmt.Errorf("unsupported type %q; use 'cel' or 'go-template'", o.Type)
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.InvalidInput)
	}

	// Handle structured output formats
	if o.Output == "json" || o.Output == "yaml" {
		return writeStructured(o.IOStreams, result, o.Output)
	}

	// Table output
	if result.Valid {
		w.Successf("Expression is valid (%s)\n", result.Type)
		if len(result.References) > 0 {
			w.Infof("Referenced variables:")
			for _, ref := range result.References {
				w.Plainf("  %s\n", ref)
			}
		}
	} else {
		w.Errorf("Expression is invalid (%s): %s\n", result.Type, result.Error)
		return exitcode.WithCode(fmt.Errorf("invalid expression"), exitcode.InvalidInput)
	}

	return nil
}

// validateCEL validates a CEL expression for syntax errors.
func validateCEL(expr string) *ValidateResult {
	result := &ValidateResult{
		Expression: expr,
		Type:       "cel",
		Valid:      true,
	}

	env, err := cel.NewEnv()
	if err != nil {
		result.Valid = false
		result.Error = fmt.Sprintf("failed to create CEL environment: %v", err)
		return result
	}

	_, issues := env.Parse(expr)
	if issues != nil && issues.Err() != nil {
		result.Valid = false
		result.Error = issues.Err().Error()
		return result
	}

	return result
}

// validateGoTemplate validates a Go template for syntax errors and extracts references.
func validateGoTemplate(expr string) *ValidateResult {
	result := &ValidateResult{
		Expression: expr,
		Type:       "go-template",
		Valid:      true,
	}

	_, err := template.New("validate").Parse(expr)
	if err != nil {
		result.Valid = false
		result.Error = err.Error()
		return result
	}

	// Extract references
	refs, refErr := gotmpl.GetGoTemplateReferences(expr, "{{", "}}")
	if refErr == nil {
		for _, ref := range refs {
			result.References = append(result.References, ref.Path)
		}
	}

	return result
}
