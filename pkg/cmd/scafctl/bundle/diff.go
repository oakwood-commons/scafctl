// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package bundle

import (
	"context"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/cmd/flags"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution/bundler"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/kvx"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// DiffOptions holds options for the bundle diff command.
type DiffOptions struct {
	RefA         string
	RefB         string
	FilesOnly    bool
	SolutionOnly bool
	Ignore       []string
	CliParams    *settings.Run
	IOStreams    *terminal.IOStreams

	flags.KvxOutputFlags
}

// CommandDiff creates the bundle diff command.
func CommandDiff(cliParams *settings.Run, ioStreams *terminal.IOStreams, _ string) *cobra.Command {
	opts := &DiffOptions{
		CliParams: cliParams,
		IOStreams: ioStreams,
	}
	opts.AppName = cliParams.BinaryName

	cmd := &cobra.Command{
		Use:          "diff <ref-a> <ref-b>",
		Short:        "Show changes between two bundled solution versions",
		SilenceUsage: true,
		Long: heredoc.Doc(`
			Show what changed between two versions of a bundled artifact,
			enabling informed upgrade decisions and change auditing.

			Compares:
			  - Solution YAML (resolvers added/removed/modified, actions changed)
			  - Bundled files (files added/removed/modified)
			  - Vendored dependencies (added/removed/upgraded)
			  - Plugin dependencies (version or defaults changed)

			Examples:
			  # Compare two versions
			  scafctl bundle diff my-solution@1.0.0 my-solution@2.0.0

			  # Show only file changes
			  scafctl bundle diff my-solution@1.0.0 my-solution@2.0.0 --files-only

			  # Show only solution structure changes
			  scafctl bundle diff my-solution@1.0.0 my-solution@2.0.0 --solution-only

			  # JSON output for scripting
			  scafctl bundle diff my-solution@1.0.0 my-solution@2.0.0 -o json
		`),
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.RefA = args[0]
			opts.RefB = args[1]
			return runDiff(cmd.Context(), opts)
		},
	}

	cmd.Flags().BoolVar(&opts.FilesOnly, "files-only", false, "Show only file changes, skip solution YAML diff")
	cmd.Flags().BoolVar(&opts.SolutionOnly, "solution-only", false, "Show only solution YAML diff, skip file changes")
	cmd.Flags().StringSliceVar(&opts.Ignore, "ignore", nil, "Glob patterns to exclude from diff (repeatable)")
	flags.AddKvxOutputFlagsToStruct(cmd, &opts.KvxOutputFlags)

	return cmd
}

// Type aliases pointing to the domain types in the bundler package.
type (
	DiffResult      = bundler.DiffResult
	SolutionDiff    = bundler.SolutionDiff
	DiffSets        = bundler.DiffSets
	FilesDiff       = bundler.FilesDiff
	FileDiffEntry   = bundler.FileDiffEntry
	VendoredDiff    = bundler.VendoredDiff
	VendoredEntry   = bundler.VendoredEntry
	VendoredUpgrade = bundler.VendoredUpgrade
	PluginsDiff     = bundler.PluginsDiff
	PluginDiffEntry = bundler.PluginDiffEntry
)

func runDiff(ctx context.Context, opts *DiffOptions) error {
	lgr := logger.FromContext(ctx)
	w := writer.FromContext(ctx)

	localCatalog, err := catalog.NewLocalCatalog(*lgr)
	if err != nil {
		w.Errorf("failed to open catalog: %v", err)
		return exitcode.WithCode(err, exitcode.CatalogError)
	}

	// Fetch both artifacts
	solA, manifestA, err := bundler.FetchAndExtract(ctx, localCatalog, opts.RefA)
	if err != nil {
		w.Errorf("failed to fetch %s: %v", opts.RefA, err)
		return exitcode.WithCode(err, exitcode.CatalogError)
	}
	defer solA.Cleanup()

	solB, manifestB, err := bundler.FetchAndExtract(ctx, localCatalog, opts.RefB)
	if err != nil {
		w.Errorf("failed to fetch %s: %v", opts.RefB, err)
		return exitcode.WithCode(err, exitcode.CatalogError)
	}
	defer solB.Cleanup()

	result := &DiffResult{
		RefA: opts.RefA,
		RefB: opts.RefB,
	}

	// Solution YAML diff
	if !opts.FilesOnly {
		result.Solution = bundler.DiffSolutions(solA.Sol, solB.Sol)
	}

	// Files diff
	if !opts.SolutionOnly {
		result.Files = bundler.DiffFiles(manifestA, manifestB, opts.Ignore)
		result.Vendored = bundler.DiffVendored(manifestA, manifestB)
	}

	// Plugin diff
	if !opts.FilesOnly {
		result.Plugins = bundler.DiffPlugins(manifestA, manifestB)
	}

	// Output
	format := kvx.OutputFormat(opts.Output)
	if kvx.IsStructuredFormat(format) {
		out := flags.ToKvxOutputOptions(&opts.KvxOutputFlags,
			kvx.WithIOStreams(opts.IOStreams),
			kvx.WithOutputContext(ctx),
		)
		return out.Write(result)
	}

	// Text output
	w.Infof("Comparing %s → %s", opts.RefA, opts.RefB)

	if result.Solution != nil {
		printSolutionDiff(w, result.Solution)
	}
	if result.Files != nil {
		printFilesDiff(w, result.Files)
	}
	if result.Vendored != nil {
		printVendoredDiff(w, result.Vendored)
	}
	if result.Plugins != nil {
		printPluginsDiff(w, result.Plugins)
	}

	// Summary
	w.Plain("")
	changes := bundler.CountChanges(result)
	w.Infof("Summary: %s", changes)

	return nil
}

func printSolutionDiff(w *writer.Writer, diff *SolutionDiff) {
	w.Plain("")
	w.Plain("Solution YAML:")
	printDiffSets(w, "  resolvers:", diff.Resolvers)
	printDiffSets(w, "  workflow.actions:", diff.Actions)
}

func printDiffSets(w *writer.Writer, label string, ds DiffSets) {
	if len(ds.Added) == 0 && len(ds.Removed) == 0 {
		return
	}
	w.Plain(label)
	for _, name := range ds.Added {
		w.Successf("    + %s (added)", name)
	}
	for _, name := range ds.Modified {
		w.Infof("    ~ %s (present in both)", name)
	}
	for _, name := range ds.Removed {
		w.Errorf("    - %s (removed)", name)
	}
}

func printFilesDiff(w *writer.Writer, diff *FilesDiff) {
	if diff == nil {
		return
	}
	w.Plain("")
	w.Plain("Bundled files:")
	for _, f := range diff.Added {
		w.Successf("    + %s (added, %s)", f.Path, bundler.FormatSize(f.Size))
	}
	for _, f := range diff.Modified {
		w.Infof("    ~ %s (modified)", f.Path)
	}
	for _, f := range diff.Removed {
		w.Errorf("    - %s (removed)", f.Path)
	}
}

func printVendoredDiff(w *writer.Writer, diff *VendoredDiff) {
	if diff == nil {
		return
	}
	w.Plain("")
	w.Plain("Vendored dependencies:")
	for _, v := range diff.Added {
		w.Successf("    + %s@%s (added)", v.Name, v.Version)
	}
	for _, v := range diff.Upgraded {
		w.Infof("    ~ %s: %s → %s (upgraded)", v.Name, v.From, v.To)
	}
	for _, v := range diff.Removed {
		w.Errorf("    - %s@%s (removed)", v.Name, v.Version)
	}
}

func printPluginsDiff(w *writer.Writer, diff *PluginsDiff) {
	if diff == nil {
		return
	}
	w.Plain("")
	w.Plain("Plugins:")
	for _, p := range diff.Added {
		w.Successf("    + %s %s (added)", p.Name, p.VersionTo)
	}
	for _, p := range diff.Modified {
		w.Infof("    ~ %s: %s → %s (constraint changed)", p.Name, p.VersionFrom, p.VersionTo)
	}
	for _, p := range diff.Removed {
		w.Errorf("    - %s %s (removed)", p.Name, p.VersionFrom)
	}
}
