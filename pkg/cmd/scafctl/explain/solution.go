// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package explain

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution/get"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// SolutionOptions holds configuration for the explain solution command
type SolutionOptions struct {
	IOStreams *terminal.IOStreams
	CliParams *settings.Run
	File      string
}

// CommandSolution creates the 'explain solution' subcommand
func CommandSolution(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
	options := &SolutionOptions{}

	cCmd := &cobra.Command{
		Use:     "solution [name[@version]]",
		Aliases: []string{"solutions", "sol", "s"},
		Short:   "Explain a solution's metadata and structure",
		Long: `Show detailed documentation for a solution including its metadata,
resolvers, actions, and overall structure.

The output includes:
  - Solution name, version, and description
  - List of resolvers with their providers and dependencies
  - List of actions with their types and dependencies
  - Required parameters summary
  - Catalog and visibility information

Solutions can be loaded from:
  - Catalog name or remote registry ref: Use as positional argument (e.g., "my-app")
  - URL: Use as positional argument or with -f/--file (e.g., "https://example.com/my-solution.yaml")
  - Local file: Use -f/--file flag (e.g., -f ./my-solution.yaml)
  - Auto-discovery: If no source is specified, searches for solution.yaml

NOTE: Positional arguments accept catalog names, remote registry refs, and URLs.
Local file paths must use -f/--file.

Examples:
  # Explain a solution from a file
  scafctl explain solution -f ./my-solution.yaml

  # Explain a solution from catalog
  scafctl explain solution my-app

  # Explain using default file discovery
  scafctl explain solution`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cCmd *cobra.Command, args []string) error {
			cliParams.EntryPointSettings.Path = filepath.Join(path, cCmd.Name())
			ctx := settings.IntoContext(cCmd.Context(), cliParams)

			options.IOStreams = ioStreams
			options.CliParams = cliParams

			w := writer.New(ioStreams, cliParams)

			if len(args) > 0 {
				if err := get.ValidatePositionalRef(args[0], options.File, cliParams.BinaryName+" explain solution"); err != nil {
					w.Errorf("%v", err)
					return exitcode.WithCode(err, exitcode.InvalidInput)
				}
				options.File = args[0]
			}

			return options.Run(ctx)
		},
		SilenceUsage: true,
	}

	cCmd.Flags().StringVarP(&options.File, "file", "f", "", "Path to the solution file (local file, URL, or '-' for stdin)")

	return cCmd
}

// Run executes the explain solution command
func (o *SolutionOptions) Run(ctx context.Context) error {
	w := writer.New(o.IOStreams, o.CliParams)

	sol, err := LoadSolution(ctx, o.File)
	if err != nil {
		w.Errorf("%v", err)
		return err
	}

	exp := BuildSolutionExplanation(sol)
	o.printSolutionExplanation(w, exp)
	return nil
}

// printSolutionExplanation prints a human-readable explanation of a solution
func (o *SolutionOptions) printSolutionExplanation(w *writer.Writer, exp *SolutionExplanation) {
	// Header
	displayName := exp.DisplayName
	if displayName == "" {
		displayName = exp.Name
	}
	w.Infof("%s (%s@%s)", displayName, exp.Name, exp.Version)
	w.Plainln("")

	// Description
	if exp.Description != "" {
		w.Plainln(exp.Description)
		w.Plainln("")
	}

	// Basic metadata
	w.Infof("Metadata")
	w.Plainlnf("  Name:       %s", exp.Name)
	w.Plainlnf("  Version:    %s", exp.Version)
	if exp.Category != "" {
		w.Plainlnf("  Category:   %s", exp.Category)
	}
	w.Plainln("")

	// Catalog info
	if exp.Catalog != nil {
		w.Infof("Catalog")
		if exp.Catalog.Visibility != "" {
			w.Plainlnf("  Visibility: %s", exp.Catalog.Visibility)
		}
		if exp.Catalog.Beta {
			w.Plainln("  Status:     Beta")
		}
		if exp.Catalog.Disabled {
			w.Warningf("⚠️  This solution is DISABLED")
		}
		w.Plainln("")
	}

	// Resolvers
	if len(exp.Resolvers) > 0 {
		w.Infof("Resolvers (%d)", len(exp.Resolvers))
		for _, r := range exp.Resolvers {
			if len(r.Providers) > 0 {
				w.Plainlnf("  %s (%s)", r.Name, strings.Join(r.Providers, ", "))
			} else {
				w.Plainlnf("  %s", r.Name)
			}
			if len(r.DependsOn) > 0 {
				w.Plainlnf("      depends on: %s", strings.Join(r.DependsOn, ", "))
			}
			if r.Conditional {
				w.Plainln("      conditional: yes")
			}
			if len(r.Phases) > 0 {
				w.Plainlnf("      phases: %s", strings.Join(r.Phases, " → "))
			}
		}
		w.Plainln("")
	}

	// Actions
	actionCount := len(exp.Actions) + len(exp.Finally)
	if actionCount > 0 {
		w.Infof("Actions (%d)", actionCount)
		for _, act := range exp.Actions {
			o.printActionInfo(w, &act, "  ")
		}
		if len(exp.Finally) > 0 {
			w.Plainln("  finally:")
			for _, act := range exp.Finally {
				o.printActionInfo(w, &act, "    ")
			}
		}
		w.Plainln("")
	}

	// Tags
	if len(exp.Tags) > 0 {
		w.Infof("Tags")
		w.Plainln(strings.Join(exp.Tags, ", "))
		w.Plainln("")
	}

	// Links
	if len(exp.Links) > 0 {
		w.Infof("Links")
		for _, link := range exp.Links {
			w.Plainlnf("  • %s: %s", link.Name, link.URL)
		}
		w.Plainln("")
	}

	// Maintainers
	if len(exp.Maintainers) > 0 {
		w.Infof("Maintainers")
		for _, m := range exp.Maintainers {
			if m.Email != "" {
				w.Plainlnf("  • %s <%s>", m.Name, m.Email)
			} else {
				w.Plainlnf("  • %s", m.Name)
			}
		}
		w.Plainln("")
	}

	// Source path
	if exp.Path != "" {
		w.Infof("Source")
		w.Plainln(exp.Path)
	}
}

// printActionInfo prints a summary of a single action from structured data
func (o *SolutionOptions) printActionInfo(w *writer.Writer, act *ActionInfo, indent string) {
	w.Plainlnf("%s%s (%s)", indent, act.Name, act.Provider)

	if len(act.DependsOn) > 0 {
		w.Plainlnf("%s    depends on: %s", indent, strings.Join(act.DependsOn, ", "))
	}
	if act.Conditional {
		w.Plainlnf("%s    conditional: yes", indent)
	}
	if act.HasRetry {
		w.Plainlnf("%s    retry: enabled", indent)
	}
	if act.HasForEach {
		w.Plainlnf("%s    forEach: enabled", indent)
	}
}
