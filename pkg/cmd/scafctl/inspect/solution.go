// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package inspect

import (
	"context"
	"fmt"
	"path/filepath"

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

	// kvx output integration
	flags.KvxOutputFlags
}

// CommandInspectSolution creates the 'inspect solution' subcommand.
func CommandInspectSolution(cliParams *settings.Run, ioStreams *terminal.IOStreams, binaryName string) *cobra.Command {
	opts := &SolutionOptions{}

	cmd := &cobra.Command{
		Use:     "solution [name[@version]]",
		Aliases: []string{"sol"},
		Short:   "Inspect solution structure with kvx output",
		Long: heredoc.Doc(`
			Inspect a solution's structure and metadata with full kvx output support.

			This provides a structured view of solution metadata, resolvers, actions,
			parameters, file dependencies, and the run command. Unlike 'explain solution'
			which uses fixed text output, 'inspect solution' supports all kvx output
			formats including table, JSON, YAML, tree, mermaid, and interactive mode.

			Solutions can be loaded from:
			  - Catalog name or remote registry ref: positional argument
			  - URL: positional argument or -f/--file
			  - Local file: -f/--file flag
			  - Auto-discovery: if no source is specified, searches for solution.yaml
		`),
		Example: heredoc.Docf(`
			# Inspect a solution from a file (table view)
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
	flags.AddKvxOutputFlagsToStruct(cmd, &opts.KvxOutputFlags)

	return cmd
}

// Run executes the inspect solution command.
func (o *SolutionOptions) Run(ctx context.Context) error {
	sol, err := inspect.LoadSolution(ctx, o.File)
	if err != nil {
		return fmt.Errorf("loading solution: %w", err)
	}

	exp := inspect.BuildSolutionExplanation(sol)

	// Build the full inspection result including run command info
	result := buildInspectResult(exp, sol, o.File, o.BinaryName)

	kvxOpts := flags.ToKvxOutputOptions(&o.KvxOutputFlags,
		kvx.WithOutputContext(ctx),
		kvx.WithOutputNoColor(o.CliParams.NoColor),
		kvx.WithOutputAppName(o.BinaryName+" inspect solution"),
		kvx.WithIOStreams(o.IOStreams),
	)

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
