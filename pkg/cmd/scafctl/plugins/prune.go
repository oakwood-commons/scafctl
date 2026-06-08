// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package plugins

import (
	"context"
	"fmt"
	"strings"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/kvx/pkg/tui"
	"github.com/oakwood-commons/scafctl/pkg/cmd/flags"
	"github.com/oakwood-commons/scafctl/pkg/plugin"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/kvx"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// PruneOptions holds options for the prune command.
type PruneOptions struct {
	CliParams *settings.Run
	IOStreams *terminal.IOStreams
	CacheDir  string
	Keep      int
	All       bool
	Force     bool
	DryRun    bool
	flags.KvxOutputFlags
}

// pruneResultItem is the display-friendly representation of a pruned entry.
type pruneResultItem struct {
	Name      string `json:"name" yaml:"name"`
	Version   string `json:"version" yaml:"version"`
	Platform  string `json:"platform" yaml:"platform"`
	Size      int64  `json:"size" yaml:"size"`
	SizeHuman string `json:"sizeHuman" yaml:"sizeHuman"`
}

// pruneColumnHints controls table column display for prune results.
var pruneColumnHints = map[string]tui.ColumnHint{
	"name":      {MaxWidth: 25, Priority: 10},
	"version":   {MaxWidth: 15, Priority: 9},
	"platform":  {MaxWidth: 15, Priority: 8},
	"sizeHuman": {MaxWidth: 10, Priority: 6, DisplayName: "SIZE"},
	"size":      {Hidden: true},
}

// CommandPrune creates the prune subcommand.
func CommandPrune(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
	opts := &PruneOptions{
		CliParams:      cliParams,
		IOStreams:      ioStreams,
		KvxOutputFlags: flags.KvxOutputFlags{AppName: cliParams.BinaryName},
	}

	cmd := &cobra.Command{
		Use:          "prune [plugin-name ...]",
		Short:        "Remove old cached plugin versions",
		SilenceUsage: true,
		Long: strings.ReplaceAll(heredoc.Doc(`
			Remove old cached plugin versions, keeping only the most recent
			version(s) per plugin.

			By default, keeps the latest version of each plugin and removes
			all older versions. Use --keep to retain more versions.

			Examples:
			  # Remove old versions, keep latest per plugin
			  scafctl plugins prune

			  # Prune specific plugins only
			  scafctl plugins prune exec github

			  # Keep 2 most recent versions
			  scafctl plugins prune --keep 2

			  # Preview what would be removed
			  scafctl plugins prune --dry-run

			  # Remove everything (empty the cache)
			  scafctl plugins prune --all --force
		`), settings.CliBinaryName, cliParams.BinaryName),
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			w := writer.FromContext(cmd.Context())
			if w == nil {
				w = writer.New(ioStreams, cliParams)
			}
			ctx := writer.WithWriter(cmd.Context(), w)

			kvxOpts := flags.ToKvxOutputOptions(&opts.KvxOutputFlags,
				kvx.WithIOStreams(ioStreams),
				kvx.WithOutputColumnHints(pruneColumnHints),
				kvx.WithOutputColumnOrder([]string{"name", "version", "platform", "sizeHuman"}),
			)

			return runPrune(ctx, opts, args, kvxOpts)
		},
	}

	cmd.Flags().IntVar(&opts.Keep, "keep", 1, "Number of versions to retain per plugin")
	cmd.Flags().BoolVar(&opts.All, "all", false, "Remove ALL cached plugins (requires --force)")
	cmd.Flags().BoolVar(&opts.Force, "force", false, "Skip confirmation for destructive operations")
	cmd.Flags().BoolVar(&opts.DryRun, "dry-run", false, "Preview without deleting")
	cmd.Flags().StringVar(&opts.CacheDir, "cache-dir", "", fmt.Sprintf("Plugin cache directory (default: $XDG_CACHE_HOME/%s/plugins/)", path))
	flags.AddKvxOutputFlagsToStruct(cmd, &opts.KvxOutputFlags)

	return cmd
}

func runPrune(ctx context.Context, opts *PruneOptions, args []string, kvxOpts *kvx.OutputOptions) error {
	w := writer.FromContext(ctx)
	if w == nil {
		return fmt.Errorf("writer not initialized in context")
	}

	cacheDir := opts.CacheDir
	if cacheDir == "" {
		binaryName := opts.CliParams.BinaryName
		if binaryName == "" {
			binaryName = settings.CliBinaryName
		}
		cacheDir = settings.PluginCacheDirFor(binaryName)
	}

	cache := plugin.NewCache(cacheDir)

	pruneOpts := plugin.PruneOptions{
		Keep:  opts.Keep,
		Names: args,
		All:   opts.All,
		Force: opts.Force,
	}

	summary, err := cache.Prune(pruneOpts, opts.DryRun)
	if err != nil {
		w.Errorf("%v", err)
		return err
	}

	if len(summary.Removed) == 0 && len(summary.Skipped) == 0 {
		w.PlainStderrf("Nothing to prune — cache is already clean.")
		return kvxOpts.Write([]pruneResultItem{})
	}

	if len(summary.Removed) == 0 && len(summary.Skipped) > 0 {
		for _, s := range summary.Skipped {
			if s.Version != "" {
				w.WarnStderrf("Skipped %s@%s (%s): %s", s.Name, s.Version, s.Platform, s.Reason)
			} else {
				w.WarnStderrf("Skipped %s: %s", s.Name, s.Reason)
			}
		}
		w.PlainStderrf("Nothing pruned — all targets are locked or in use.")
		return kvxOpts.Write([]pruneResultItem{})
	}

	// Build display items.
	items := make([]pruneResultItem, 0, len(summary.Removed))
	for _, r := range summary.Removed {
		items = append(items, pruneResultItem{
			Name:      r.Name,
			Version:   r.Version,
			Platform:  r.Platform,
			Size:      r.Size,
			SizeHuman: formatBytes(r.Size),
		})
	}

	if opts.DryRun {
		w.PlainStderrf("Dry run: would remove %d version(s), freeing %s:", len(summary.Removed), formatBytes(summary.TotalFreed))
	}

	if err := kvxOpts.Write(items); err != nil {
		return fmt.Errorf("rendering output: %w", err)
	}

	if !opts.DryRun {
		w.PlainStderrf("Pruned %d version(s), freed %s.", len(summary.Removed), formatBytes(summary.TotalFreed))
	}

	for _, s := range summary.Skipped {
		if s.Version != "" {
			w.WarnStderrf("Skipped %s@%s (%s): %s", s.Name, s.Version, s.Platform, s.Reason)
		} else {
			w.WarnStderrf("Skipped %s: %s", s.Name, s.Reason)
		}
	}

	return nil
}
