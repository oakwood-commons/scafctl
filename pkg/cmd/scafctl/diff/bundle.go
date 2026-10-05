// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package diff

import (
	"context"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/cmd/flags"
	"github.com/oakwood-commons/scafctl/pkg/diffreport"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution/bundler"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// BundleDiffOptions holds options for the diff bundle command.
type BundleDiffOptions struct {
	RefA         string
	RefB         string
	FilesOnly    bool
	SolutionOnly bool
	Ignore       []string
	CliParams    *settings.Run
	IOStreams    *terminal.IOStreams

	flags.KvxOutputFlags
}

// CommandDiffBundle creates the `diff bundle` subcommand.
func CommandDiffBundle(cliParams *settings.Run, ioStreams *terminal.IOStreams, binaryName string) *cobra.Command {
	opts := &BundleDiffOptions{
		CliParams: cliParams,
		IOStreams: ioStreams,
	}
	opts.AppName = cliParams.BinaryName

	cmd := &cobra.Command{
		Use:          subBundle + " <ref-a> <ref-b>",
		Short:        "Show changes between two bundled solution versions",
		SilenceUsage: true,
		Long: heredoc.Docf(`
			Show what changed between two versions of a bundled artifact,
			enabling informed upgrade decisions and change auditing.

			Compares:
			  - Solution YAML (resolvers added/removed/modified, actions changed)
			  - Bundled files (files added/removed/modified)
			  - Vendored dependencies (added/removed/upgraded)
			  - Plugin dependencies (version or defaults changed)

			Examples:
			  # Compare two versions
			  %[1]s diff bundle my-solution@1.0.0 my-solution@2.0.0

			  # Show only file changes
			  %[1]s diff bundle my-solution@1.0.0 my-solution@2.0.0 --files-only

			  # Show only solution structure changes
			  %[1]s diff bundle my-solution@1.0.0 my-solution@2.0.0 --solution-only

			  # JSON output for scripting
			  %[1]s diff bundle my-solution@1.0.0 my-solution@2.0.0 -o json
		`, binaryName),
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.RefA = args[0]
			opts.RefB = args[1]
			return runBundleDiff(cmd.Context(), opts)
		},
	}

	cmd.Flags().BoolVar(&opts.FilesOnly, "files-only", false, "Show only file changes, skip solution YAML diff")
	cmd.Flags().BoolVar(&opts.SolutionOnly, "solution-only", false, "Show only solution YAML diff, skip file changes")
	cmd.Flags().StringSliceVar(&opts.Ignore, "ignore", nil, "Glob patterns to exclude from diff (repeatable)")
	flags.AddKvxOutputFlagsToStruct(cmd, &opts.KvxOutputFlags)

	return cmd
}

// bundleDiffResult aliases the domain diff result for brevity.
type bundleDiffResult = bundler.DiffResult

func runBundleDiff(ctx context.Context, opts *BundleDiffOptions) error {
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

	result := &bundleDiffResult{
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
	appName := opts.CliParams.BinaryName + " diff bundle"
	return writeDiffOutput(ctx, w, opts.IOStreams, &opts.KvxOutputFlags, appName, result, reportFromBundleDiff(result))
}

// reportFromBundleDiff maps a bundler.DiffResult into the shared diff report.
func reportFromBundleDiff(result *bundler.DiffResult) *diffreport.Report {
	r := diffreport.New("bundle", result.RefA, result.RefB)

	if result.Solution != nil {
		addBundleDiffSets(r, "resolvers", result.Solution.Resolvers)
		addBundleDiffSets(r, "actions", result.Solution.Actions)
	}
	if result.Files != nil {
		for _, f := range result.Files.Added {
			r.Add(diffreport.Entry{Group: "files", Path: f.Path, Kind: diffreport.ChangeAdded, Detail: bundler.FormatSize(f.Size)})
		}
		for _, f := range result.Files.Modified {
			r.Add(diffreport.Entry{Group: "files", Path: f.Path, Kind: diffreport.ChangeModified})
		}
		for _, f := range result.Files.Removed {
			r.Add(diffreport.Entry{Group: "files", Path: f.Path, Kind: diffreport.ChangeRemoved})
		}
	}
	if result.Vendored != nil {
		for _, v := range result.Vendored.Added {
			r.Add(diffreport.Entry{Group: "vendored", Path: v.Name, Kind: diffreport.ChangeAdded, Detail: v.Version})
		}
		for _, v := range result.Vendored.Upgraded {
			r.Add(diffreport.Entry{Group: "vendored", Path: v.Name, Kind: diffreport.ChangeModified, Before: v.From, After: v.To, Detail: v.From + " -> " + v.To})
		}
		for _, v := range result.Vendored.Removed {
			r.Add(diffreport.Entry{Group: "vendored", Path: v.Name, Kind: diffreport.ChangeRemoved, Detail: v.Version})
		}
	}
	if result.Plugins != nil {
		for _, p := range result.Plugins.Added {
			r.Add(diffreport.Entry{Group: "plugins", Path: p.Name, Kind: diffreport.ChangeAdded, Detail: p.VersionTo})
		}
		for _, p := range result.Plugins.Modified {
			r.Add(diffreport.Entry{Group: "plugins", Path: p.Name, Kind: diffreport.ChangeModified, Before: p.VersionFrom, After: p.VersionTo, Detail: p.VersionFrom + " -> " + p.VersionTo})
		}
		for _, p := range result.Plugins.Removed {
			r.Add(diffreport.Entry{Group: "plugins", Path: p.Name, Kind: diffreport.ChangeRemoved, Detail: p.VersionFrom})
		}
	}
	return r
}

func addBundleDiffSets(r *diffreport.Report, group string, ds bundler.DiffSets) {
	for _, name := range ds.Added {
		r.Add(diffreport.Entry{Group: group, Path: name, Kind: diffreport.ChangeAdded})
	}
	// ds.Modified holds names present on both sides, not confirmed structural
	// changes (see bundler.ComputeDiffSetsFromBool), so they are omitted to
	// avoid reporting every shared resolver/action as modified.
	for _, name := range ds.Removed {
		r.Add(diffreport.Entry{Group: group, Path: name, Kind: diffreport.ChangeRemoved})
	}
}
