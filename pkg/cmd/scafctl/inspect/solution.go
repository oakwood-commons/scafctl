// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package inspect

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/scafctl/pkg/cmd/flags"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/get"
	"github.com/oakwood-commons/scafctl/pkg/solution/inspect"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/kvx"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// SolutionOptions holds options for the inspect solution command.
type SolutionOptions struct {
	IOStreams  *terminal.IOStreams
	CliParams  *settings.Run
	BinaryName string
	File       string

	// Usage switches to the user-facing "how do I run this" projection.
	Usage bool

	// kvx output integration
	flags.KvxOutputFlags
}

// CommandInspectSolution creates the 'inspect solution' subcommand.
func CommandInspectSolution(cliParams *settings.Run, ioStreams *terminal.IOStreams, binaryName string) *cobra.Command {
	opts := &SolutionOptions{}

	cmd := &cobra.Command{
		Use:     "solution [name[@version]]",
		Aliases: []string{"sol"},
		Short:   "Inspect a solution's structure (or --usage for how to run it)",
		Long: heredoc.Doc(`
			Inspect a specific solution's structure and metadata with full kvx
			output support.

			By default this shows the developer view: metadata, resolvers, actions,
			file dependencies, and the run command, in any kvx output format
			(table, JSON, YAML, tree, mermaid, interactive).

			Add --usage for the user-facing view: a synopsis, the parameters a
			solution takes (with types, defaults, and discovered allowed values),
			and the exact command to run each action. Use this to learn how to
			consume a solution you did not write.

			Solutions can be loaded from:
			  - Catalog name or remote registry ref: positional argument
			  - URL: positional argument or -f/--file
			  - Local file: -f/--file flag
			  - Auto-discovery: if no source is specified, searches for solution.yaml
		`),
		Example: heredoc.Docf(`
			# How do I run this solution? (usage view)
			$ %[1]s inspect solution -f ./my-solution.yaml --usage

			# Usage view as structured data
			$ %[1]s inspect solution -f ./my-solution.yaml --usage -o json

			# Inspect a solution's structure from a file (table view)
			$ %[1]s inspect solution -f ./my-solution.yaml

			# Inspect from catalog with JSON output
			$ %[1]s inspect solution my-app -o json

			# Interactive TUI for exploring solution structure
			$ %[1]s inspect solution -f ./my-solution.yaml -i

			# Filter resolvers with a where clause
			$ %[1]s inspect solution -f ./my-solution.yaml -o json -e '_.resolvers' -w '_.conditional'

			# Tree view for hierarchical overview
			$ %[1]s inspect solution -f ./my-solution.yaml -o tree
		`, binaryName),
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cliParams.EntryPointSettings.Path = filepath.Join(cliParams.EntryPointSettings.Path, cmd.Name())
			ctx := settings.IntoContext(cmd.Context(), cliParams)

			opts.IOStreams = ioStreams
			opts.CliParams = cliParams
			opts.AppName = cliParams.BinaryName
			opts.BinaryName = binaryName

			w := writer.FromContext(ctx)

			if len(args) > 0 {
				if err := get.ValidatePositionalRef(args[0], opts.File, binaryName+" inspect solution"); err != nil {
					w.Errorf("%v", err)
					return exitcode.WithCode(err, exitcode.InvalidInput)
				}
				opts.File = args[0]
			}

			return opts.Run(ctx)
		},
		SilenceUsage: true,
	}

	cmd.Flags().StringVarP(&opts.File, "file", "f", "", "Path to the solution file (local file, URL, or '-' for stdin)")
	cmd.Flags().BoolVar(&opts.Usage, "usage", false, "Show the user-facing usage view (synopsis, parameters, and how to run each action)")
	flags.AddKvxOutputFlagsToStruct(cmd, &opts.KvxOutputFlags)

	return cmd
}

// Run executes the inspect solution command.
func (o *SolutionOptions) Run(ctx context.Context) error {
	w := writer.FromContext(ctx)

	sol, err := inspect.LoadSolution(ctx, o.File)
	if err != nil {
		// Surface the error to the user before returning the coded error;
		// otherwise a non-GeneralError exit code exits silently.
		if w != nil {
			w.Errorf("%v", err)
		}
		return err
	}

	appName := o.BinaryName + " inspect solution"
	kvxOpts := flags.ToKvxOutputOptions(&o.KvxOutputFlags,
		kvx.WithOutputContext(ctx),
		kvx.WithOutputNoColor(o.CliParams.NoColor),
		kvx.WithOutputAppName(appName),
		kvx.WithIOStreams(o.IOStreams),
	)

	// User-facing usage projection.
	if o.Usage {
		usage, uErr := inspect.BuildUsage(ctx, sol, o.File, o.BinaryName)
		if uErr != nil {
			if w != nil {
				w.Errorf("%v", uErr)
			}
			return exitcode.WithCode(uErr, exitcode.InvalidInput)
		}
		// Default (auto) non-interactive output uses a human-friendly sectioned
		// renderer; explicit -o/-i uses the kvx pipeline for structured output.
		if (o.Output == "" || o.Output == "auto") && !o.Interactive {
			return o.renderUsageText(ctx, usage)
		}
		return kvxOpts.Write(usage)
	}

	// Default: developer structure view.
	exp := inspect.BuildSolutionExplanation(sol)
	result := buildInspectResult(exp, sol, o.File, o.BinaryName)
	return kvxOpts.Write(result)
}

// Result is the structured output for inspect solution.
type Result struct {
	Name         string `json:"name" yaml:"name"`
	DisplayName  string `json:"displayName,omitempty" yaml:"displayName,omitempty"`
	Version      string `json:"version" yaml:"version"`
	Description  string `json:"description,omitempty" yaml:"description,omitempty"`
	Category     string `json:"category,omitempty" yaml:"category,omitempty"`
	Path         string `json:"path,omitempty" yaml:"path,omitempty"`
	HasWorkflow  bool   `json:"hasWorkflow" yaml:"hasWorkflow"`
	HasResolvers bool   `json:"hasResolvers" yaml:"hasResolvers"`

	RunCommand string `json:"runCommand,omitempty" yaml:"runCommand,omitempty"`

	Resolvers []inspect.ResolverInfo `json:"resolvers,omitempty" yaml:"resolvers,omitempty"`
	Actions   []inspect.ActionInfo   `json:"actions,omitempty" yaml:"actions,omitempty"`
	Finally   []inspect.ActionInfo   `json:"finally,omitempty" yaml:"finally,omitempty"`

	Parameters []inspect.ParamInfo `json:"parameters,omitempty" yaml:"parameters,omitempty"`

	Tags             []string                     `json:"tags,omitempty" yaml:"tags,omitempty"`
	Links            []inspect.LinkInfo           `json:"links,omitempty" yaml:"links,omitempty"`
	Maintainers      []inspect.MaintainerInfo     `json:"maintainers,omitempty" yaml:"maintainers,omitempty"`
	FileDependencies []inspect.FileDependencyInfo `json:"fileDependencies,omitempty" yaml:"fileDependencies,omitempty"`

	Catalog *inspect.CatalogInfo `json:"catalog,omitempty" yaml:"catalog,omitempty"`
}

func buildInspectResult(exp *inspect.SolutionExplanation, sol *solution.Solution, path, binaryName string) *Result {
	result := &Result{
		Name:             exp.Name,
		DisplayName:      exp.DisplayName,
		Version:          exp.Version,
		Description:      exp.Description,
		Category:         exp.Category,
		Path:             exp.Path,
		HasWorkflow:      sol.Spec.HasWorkflow(),
		HasResolvers:     sol.Spec.HasResolvers(),
		Resolvers:        exp.Resolvers,
		Actions:          exp.Actions,
		Finally:          exp.Finally,
		Tags:             exp.Tags,
		Links:            exp.Links,
		Maintainers:      exp.Maintainers,
		FileDependencies: exp.FileDependencies,
		Catalog:          exp.Catalog,
	}

	// Include run command if the solution is runnable
	if cmdInfo, err := inspect.BuildRunCommand(sol, path, binaryName); err == nil {
		result.RunCommand = cmdInfo.Command
		result.Parameters = cmdInfo.Parameters
	}

	return result
}

// renderUsageText renders the usage view as human-friendly sectioned output.
// Used for the default (auto) non-interactive format; structured formats go
// through the kvx pipeline instead.
func (o *SolutionOptions) renderUsageText(ctx context.Context, u *inspect.UsageInfo) error {
	w := writer.FromContext(ctx)
	if w == nil {
		return nil
	}

	// Header: name (version) + synopsis.
	title := u.Name
	if u.Version != "" {
		title = fmt.Sprintf("%s (%s)", u.Name, u.Version)
	}
	w.Plainlnf("%s", title)
	if u.Synopsis != "" {
		w.Plainlnf("%s", u.Synopsis)
	}
	if u.Run != "" {
		w.Plainln("")
		w.Plainlnf("Run: %s", u.Run)
	}

	// Parameters.
	if len(u.Params) > 0 {
		w.Plainln("")
		w.Plainln("PARAMETERS")
		for _, p := range u.Params {
			line := "  " + p.Name
			if p.Type != "" {
				line += " (" + p.Type + ")"
			}
			if p.Required {
				line += " [required]"
			} else if p.Default != nil {
				line += fmt.Sprintf(" [default: %v]", p.Default)
			}
			w.Plainlnf("%s", line)
			if p.Description != "" {
				w.Plainlnf("      %s", p.Description)
			}
			if len(p.AllowedValues) > 0 {
				w.Plainlnf("      values: %s", joinAny(p.AllowedValues))
			}
		}
	}

	// Actions.
	if len(u.Actions) > 0 {
		w.Plainln("")
		w.Plainln("ACTIONS")
		for _, a := range u.Actions {
			name := "  " + a.Name
			if a.Default {
				name += " (default)"
			}
			w.Plainlnf("%s", name)
			if a.Description != "" {
				w.Plainlnf("      %s", a.Description)
			}
			w.Plainlnf("      %s", a.Command)
		}
	}

	// Curated examples.
	if len(u.Examples) > 0 {
		w.Plainln("")
		w.Plainln("EXAMPLES")
		for _, ex := range u.Examples {
			if ex.Description != "" {
				w.Plainlnf("  # %s", ex.Description)
			}
			w.Plainlnf("  %s", ex.Command)
		}
	}

	return nil
}

// joinAny formats a slice of literal values as a comma-separated string.
func joinAny(vals []any) string {
	parts := make([]string, len(vals))
	for i, v := range vals {
		parts[i] = fmt.Sprintf("%v", v)
	}
	return strings.Join(parts, ", ")
}
