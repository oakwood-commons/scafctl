// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package eval

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/gotmpl"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// TemplateOptions holds options for the eval template command.
type TemplateOptions struct {
	IOStreams    *terminal.IOStreams
	CliParams    *settings.Run
	Output       string
	Template     string
	TemplateFile string
	Vars         []string
	Data         string
	File         string
	ShowRefs     bool
}

// TemplateResult holds the result of evaluating a Go template.
type TemplateResult struct {
	Output     string                     `json:"output" yaml:"output" doc:"The rendered template output"`
	Template   string                     `json:"template" yaml:"template" doc:"The template that was evaluated"`
	References []gotmpl.TemplateReference `json:"references,omitempty" yaml:"references,omitempty" doc:"Referenced template variables"`
}

// CommandTemplate creates the 'eval template' command.
func CommandTemplate(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
	opts := &TemplateOptions{}

	cCmd := &cobra.Command{
		Use:     "template",
		Aliases: []string{"tmpl", "t"},
		Short:   "Evaluate a Go template",
		Long: heredoc.Doc(`
			Evaluate a Go template with optional data context.

			Provide a template inline via --template or from a file via --template-file.
			Data can come from --var flags, inline JSON via --data, or a file via --file.

			Use --show-refs to also list template variable references found in the template.

			Examples:
			  # Simple variable rendering
			  scafctl eval template -t '{{ .name | upper }}' -v name=hello

			  # Template from file with data
			  scafctl eval template --template-file deploy.tmpl -d '{"env": "prod"}'

			  # Show referenced resolver fields
			  scafctl eval template -t '{{ ._.config.host }}' -f resolvers.json --show-refs

			  # Output as JSON
			  scafctl eval template -t '{{ .name }}' -v name=world -o json
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

	cCmd.Flags().StringVarP(&opts.Template, "template", "t", "", "Go template string (inline)")
	cCmd.Flags().StringVar(&opts.TemplateFile, "template-file", "", "Go template file path")
	cCmd.Flags().StringArrayVarP(&opts.Vars, "var", "v", nil, "Variable as key=value (repeatable)")
	cCmd.Flags().StringVar(&opts.Data, "data", "", "Inline JSON data context")
	cCmd.Flags().StringVar(&opts.File, "file", "", "JSON/YAML file for data context")
	cCmd.Flags().BoolVar(&opts.ShowRefs, "show-refs", false, "Also output referenced template variables")
	cCmd.Flags().StringVarP(&opts.Output, "output", "o", "auto", "Output format: auto, json, yaml")

	cCmd.MarkFlagsMutuallyExclusive("template", "template-file")

	return cCmd
}

// Run executes the eval template command.
func (o *TemplateOptions) Run(ctx context.Context) error {
	w := writer.FromContext(ctx)
	if w == nil {
		return fmt.Errorf("writer not initialized in context")
	}

	// Get template content
	tmplContent, err := o.getTemplateContent()
	if err != nil {
		w.Errorf("failed to get template content: %v", err)
		return exitcode.WithCode(err, exitcode.InvalidInput)
	}

	// Build data context
	rootData, err := celexp.BuildDataContext(o.Data, o.File)
	if err != nil {
		w.Errorf("failed to build data context: %v", err)
		return exitcode.WithCode(err, exitcode.InvalidInput)
	}

	// Parse --var flags
	vars, err := celexp.ParseVars(o.Vars)
	if err != nil {
		w.Errorf("failed to parse variables: %v", err)
		return exitcode.WithCode(err, exitcode.InvalidInput)
	}

	// Merge root data and vars into a single data map
	data := mergeData(rootData, vars)

	// Execute template
	result, err := gotmpl.Execute(ctx, gotmpl.TemplateOptions{
		Content: tmplContent,
		Name:    "eval",
		Data:    data,
	})
	if err != nil {
		w.Errorf("template execution failed: %v", err)
		return exitcode.WithCode(err, exitcode.GeneralError)
	}

	tmplResult := &TemplateResult{
		Output:   result.Output,
		Template: tmplContent,
	}

	// Optionally extract references
	if o.ShowRefs {
		refs, refErr := gotmpl.GetGoTemplateReferences(tmplContent, "{{", "}}")
		if refErr == nil {
			tmplResult.References = refs
		}
	}

	// Handle structured output formats
	if o.Output == "json" || o.Output == "yaml" {
		return writeStructured(o.IOStreams, tmplResult, o.Output)
	}

	// Plain output - just the rendered template
	w.Plainf("%s\n", result.Output)

	if o.ShowRefs && len(tmplResult.References) > 0 {
		w.Plain("")
		w.Infof("Referenced variables:")
		for _, ref := range tmplResult.References {
			w.Plainf("  %s (at position %s)\n", ref.Path, ref.Position)
		}
	}

	return nil
}

// getTemplateContent returns the template string from flags.
func (o *TemplateOptions) getTemplateContent() (string, error) {
	if o.Template == "" && o.TemplateFile == "" {
		return "", fmt.Errorf("one of --template or --template-file is required")
	}

	if o.Template != "" {
		return o.Template, nil
	}

	data, err := os.ReadFile(o.TemplateFile)
	if err != nil {
		return "", fmt.Errorf("reading template file %s: %w", o.TemplateFile, err)
	}

	return string(data), nil
}

// mergeData combines root data and vars into a single map suitable for template execution.
func mergeData(rootData any, vars map[string]any) map[string]any {
	result := make(map[string]any)

	// If rootData is a map, merge it in
	if m, ok := rootData.(map[string]any); ok {
		for k, v := range m {
			result[k] = v
		}
	} else if rootData != nil {
		result["_"] = rootData
	}

	// Overlay vars
	for k, v := range vars {
		result[k] = v
	}

	return result
}
