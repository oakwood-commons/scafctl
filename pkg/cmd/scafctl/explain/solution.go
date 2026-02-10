// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package explain

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/oakwood-commons/scafctl/pkg/action"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/resolver"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/get"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// SolutionOptions holds configuration for the explain solution command
type SolutionOptions struct {
	IOStreams *terminal.IOStreams
	CliParams *settings.Run
	Path      string
}

// CommandSolution creates the 'explain solution' subcommand
func CommandSolution(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
	options := &SolutionOptions{}

	cCmd := &cobra.Command{
		Use:     "solution [path]",
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

Examples:
  # Explain a solution from a file
  scafctl explain solution ./my-solution.yaml

  # Explain using default file discovery
  scafctl explain solution

  # Explain a solution from a URL
  scafctl explain solution https://example.com/solution.yaml`,
		RunE: func(cCmd *cobra.Command, args []string) error {
			cliParams.EntryPointSettings.Path = filepath.Join(path, cCmd.Use)
			ctx := settings.IntoContext(context.Background(), cliParams)

			options.IOStreams = ioStreams
			options.CliParams = cliParams

			if len(args) > 0 {
				options.Path = args[0]
			}

			return options.Run(ctx)
		},
		SilenceUsage: true,
	}

	cCmd.Flags().StringVarP(&options.Path, "path", "p", "", "Path to the solution file (local file or URL)")

	return cCmd
}

// Run executes the explain solution command
func (o *SolutionOptions) Run(ctx context.Context) error {
	w := writer.New(o.IOStreams, o.CliParams)
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

	// Load the solution
	getter := get.NewGetter(getterOpts...)
	sol, err := getter.Get(ctx, o.Path)
	if err != nil {
		err = fmt.Errorf("failed to load solution: %w", err)
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.FileNotFound)
	}

	o.printSolutionExplanation(w, sol)
	return nil
}

// printSolutionExplanation prints a human-readable explanation of a solution
func (o *SolutionOptions) printSolutionExplanation(w *writer.Writer, sol *solution.Solution) {
	// Header
	displayName := sol.Metadata.DisplayName
	if displayName == "" {
		displayName = sol.Metadata.Name
	}
	versionStr := "unknown"
	if sol.Metadata.Version != nil {
		versionStr = sol.Metadata.Version.String()
	}
	w.Infof("%s (%s@%s)", displayName, sol.Metadata.Name, versionStr)
	w.Plainln("")

	// Description
	if sol.Metadata.Description != "" {
		w.Plainln(sol.Metadata.Description)
		w.Plainln("")
	}

	// Basic metadata
	w.Infof("Metadata")
	w.Plainlnf("  Name:       %s", sol.Metadata.Name)
	w.Plainlnf("  Version:    %s", versionStr)
	if sol.Metadata.Category != "" {
		w.Plainlnf("  Category:   %s", sol.Metadata.Category)
	}
	w.Plainln("")

	// Catalog info
	if sol.Catalog.Visibility != "" || sol.Catalog.Beta || sol.Catalog.Disabled {
		w.Infof("Catalog")
		if sol.Catalog.Visibility != "" {
			w.Plainlnf("  Visibility: %s", sol.Catalog.Visibility)
		}
		if sol.Catalog.Beta {
			w.Plainln("  Status:     Beta")
		}
		if sol.Catalog.Disabled {
			w.Warningf("⚠️  This solution is DISABLED")
		}
		w.Plainln("")
	}

	// Resolvers
	if sol.Spec.HasResolvers() {
		w.Infof("Resolvers (%d)", len(sol.Spec.Resolvers))
		o.printResolvers(w, sol)
		w.Plainln("")
	}

	// Actions
	if sol.Spec.HasActions() {
		actionCount := 0
		if sol.Spec.Workflow != nil {
			actionCount = len(sol.Spec.Workflow.Actions) + len(sol.Spec.Workflow.Finally)
		}
		w.Infof("Actions (%d)", actionCount)
		o.printActions(w, sol)
		w.Plainln("")
	}

	// Tags
	if len(sol.Metadata.Tags) > 0 {
		w.Infof("Tags")
		w.Plainln(strings.Join(sol.Metadata.Tags, ", "))
		w.Plainln("")
	}

	// Links
	if len(sol.Metadata.Links) > 0 {
		w.Infof("Links")
		for _, link := range sol.Metadata.Links {
			w.Plainlnf("  • %s: %s", link.Name, link.URL)
		}
		w.Plainln("")
	}

	// Maintainers
	if len(sol.Metadata.Maintainers) > 0 {
		w.Infof("Maintainers")
		for _, m := range sol.Metadata.Maintainers {
			if m.Email != "" {
				w.Plainlnf("  • %s <%s>", m.Name, m.Email)
			} else {
				w.Plainlnf("  • %s", m.Name)
			}
		}
		w.Plainln("")
	}

	// Source path
	if sol.GetPath() != "" {
		w.Infof("Source")
		w.Plainln(sol.GetPath())
	}
}

// printResolvers prints information about the solution's resolvers
func (o *SolutionOptions) printResolvers(w *writer.Writer, sol *solution.Solution) {
	// Sort resolver names for consistent output
	names := make([]string, 0, len(sol.Spec.Resolvers))
	for name := range sol.Spec.Resolvers {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		r := sol.Spec.Resolvers[name]
		if r == nil {
			continue
		}

		// Build resolver info - get providers from phases
		providers := o.getResolverProviders(r)
		if len(providers) > 0 {
			w.Plainlnf("  %s (%s)", name, strings.Join(providers, ", "))
		} else {
			w.Plainlnf("  %s", name)
		}

		// Show dependencies if any
		if len(r.DependsOn) > 0 {
			w.Plainlnf("      depends on: %s", strings.Join(r.DependsOn, ", "))
		}

		// Show condition if set
		if r.When != nil {
			w.Plainln("      conditional: yes")
		}

		// Show phases summary
		phases := o.getResolverPhases(r)
		if len(phases) > 0 {
			w.Plainlnf("      phases: %s", strings.Join(phases, " → "))
		}
	}
}

// getResolverProviders extracts all provider names from resolver phases
func (o *SolutionOptions) getResolverProviders(r *resolver.Resolver) []string {
	providers := make(map[string]bool)

	if r.Resolve != nil {
		for _, source := range r.Resolve.With {
			if source.Provider != "" {
				providers[source.Provider] = true
			}
		}
	}

	if r.Transform != nil {
		for _, transform := range r.Transform.With {
			if transform.Provider != "" {
				providers[transform.Provider] = true
			}
		}
	}

	if r.Validate != nil {
		for _, validation := range r.Validate.With {
			if validation.Provider != "" {
				providers[validation.Provider] = true
			}
		}
	}

	// Convert to sorted slice
	result := make([]string, 0, len(providers))
	for p := range providers {
		result = append(result, p)
	}
	sort.Strings(result)
	return result
}

// getResolverPhases returns which phases are configured
func (o *SolutionOptions) getResolverPhases(r *resolver.Resolver) []string {
	var phases []string
	if r.Resolve != nil && len(r.Resolve.With) > 0 {
		phases = append(phases, "resolve")
	}
	if r.Transform != nil && len(r.Transform.With) > 0 {
		phases = append(phases, "transform")
	}
	if r.Validate != nil && len(r.Validate.With) > 0 {
		phases = append(phases, "validate")
	}
	return phases
}

// printActions prints information about the solution's actions
func (o *SolutionOptions) printActions(w *writer.Writer, sol *solution.Solution) {
	if sol.Spec.Workflow == nil {
		return
	}

	// Print regular actions
	if len(sol.Spec.Workflow.Actions) > 0 {
		// Sort action names for consistent output
		names := make([]string, 0, len(sol.Spec.Workflow.Actions))
		for name := range sol.Spec.Workflow.Actions {
			names = append(names, name)
		}
		sort.Strings(names)

		for _, name := range names {
			act := sol.Spec.Workflow.Actions[name]
			act.Name = name // Ensure name is set from map key
			o.printActionSummary(w, act, "  ")
		}
	}

	// Print finally actions
	if len(sol.Spec.Workflow.Finally) > 0 {
		w.Plainln("  finally:")

		// Sort finally action names for consistent output
		names := make([]string, 0, len(sol.Spec.Workflow.Finally))
		for name := range sol.Spec.Workflow.Finally {
			names = append(names, name)
		}
		sort.Strings(names)

		for _, name := range names {
			act := sol.Spec.Workflow.Finally[name]
			act.Name = name // Ensure name is set from map key
			o.printActionSummary(w, act, "    ")
		}
	}
}

// printActionSummary prints a summary of a single action
func (o *SolutionOptions) printActionSummary(w *writer.Writer, act *action.Action, indent string) {
	if act == nil {
		return
	}

	provider := act.Provider
	if provider == "" {
		provider = "unknown"
	}

	w.Plainlnf("%s%s (%s)", indent, act.Name, provider)

	// Show dependencies if any
	if len(act.DependsOn) > 0 {
		w.Plainlnf("%s    depends on: %s", indent, strings.Join(act.DependsOn, ", "))
	}

	// Show condition if set
	if act.When != nil {
		w.Plainlnf("%s    conditional: yes", indent)
	}

	// Show retry if configured
	if act.Retry != nil {
		w.Plainlnf("%s    retry: enabled", indent)
	}

	// Show forEach if configured
	if act.ForEach != nil {
		w.Plainlnf("%s    forEach: enabled", indent)
	}
}
