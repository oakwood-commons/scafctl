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

// ListOptions holds options for the list command.
type ListOptions struct {
	BinaryName string
	CliParams  *settings.Run
	IOStreams  *terminal.IOStreams
	CacheDir   string
	flags.KvxOutputFlags
}

// pluginListItem is the display-friendly representation of a cached plugin.
// The path and size fields are hidden in table view via column hints.
// sizeHuman provides a human-readable size for table display; both size and
// sizeHuman appear in JSON/YAML output.
type pluginListItem struct {
	Name      string `json:"name" yaml:"name"`
	Version   string `json:"version" yaml:"version"`
	Platform  string `json:"platform" yaml:"platform"`
	Size      int64  `json:"size" yaml:"size"`
	SizeHuman string `json:"sizeHuman" yaml:"sizeHuman"`
	Path      string `json:"path" yaml:"path"`
}

// pluginListColumnHints controls table column display for plugins list.
var pluginListColumnHints = map[string]tui.ColumnHint{
	"name":      {MaxWidth: 25, Priority: 10},
	"version":   {MaxWidth: 15, Priority: 9},
	"platform":  {MaxWidth: 15, Priority: 8},
	"sizeHuman": {MaxWidth: 10, Priority: 6, DisplayName: "SIZE"},
	"size":      {Hidden: true},
	"path":      {Hidden: true},
}

// CommandList creates the list subcommand.
func CommandList(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
	opts := &ListOptions{
		CliParams:      cliParams,
		IOStreams:      ioStreams,
		KvxOutputFlags: flags.KvxOutputFlags{AppName: cliParams.BinaryName},
	}

	cmd := &cobra.Command{
		Use:          "list",
		Aliases:      []string{"ls"},
		Short:        "List cached plugin binaries",
		SilenceUsage: true,
		Long: strings.ReplaceAll(heredoc.Doc(`
			List all plugin binaries stored in the local plugin cache.

			Shows the name, version, platform, and size for each cached binary.
			This command shows the remote download cache only (plugins fetched
			from OCI registries). To see locally-built artifacts created with
			'scafctl build plugin', use:
			  scafctl catalog list --kind provider --pre-release --all-versions

			Examples:
			  # List all cached plugins
			  scafctl plugins list

			  # List in JSON format
			  scafctl plugins list -o json
		`), settings.CliBinaryName, cliParams.BinaryName),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			w := writer.FromContext(cmd.Context())
			if w == nil {
				w = writer.New(ioStreams, cliParams)
			}
			ctx := writer.WithWriter(cmd.Context(), w)
			kvxOpts := flags.ToKvxOutputOptions(&opts.KvxOutputFlags,
				kvx.WithIOStreams(ioStreams),
				kvx.WithOutputColumnHints(pluginListColumnHints),
				kvx.WithOutputColumnOrder([]string{"name", "version", "platform", "sizeHuman"}),
			)

			opts.BinaryName = cliParams.BinaryName

			return runList(ctx, opts, kvxOpts)
		},
	}

	cmd.Flags().StringVar(&opts.CacheDir, "cache-dir", "", fmt.Sprintf("Plugin cache directory (default: $XDG_CACHE_HOME/%s/plugins/)", path))
	flags.AddKvxOutputFlagsToStruct(cmd, &opts.KvxOutputFlags)

	return cmd
}

func runList(ctx context.Context, opts *ListOptions, kvxOpts *kvx.OutputOptions) error {
	if opts.BinaryName == "" {
		opts.BinaryName = settings.CliBinaryName
	}

	w := writer.FromContext(ctx)
	if w == nil {
		return fmt.Errorf("writer not initialized in context")
	}

	cacheDir := opts.CacheDir
	if cacheDir == "" {
		cacheDir = settings.PluginCacheDirFor(opts.BinaryName)
	}
	cache := plugin.NewCache(cacheDir)
	cached, err := cache.List()
	if err != nil {
		w.Errorf("failed to list cached plugins: %v", err)
		return err
	}

	if len(cached) == 0 {
		w.PlainStderrf("No plugins cached. Use '%s plugins install' to fetch plugins.", opts.BinaryName)
		w.PlainStderrf("To see locally-built artifacts: %s catalog list --kind provider --pre-release --all-versions", opts.BinaryName)
		return kvxOpts.Write([]pluginListItem{})
	}

	items := make([]pluginListItem, len(cached))
	for i, p := range cached {
		items[i] = pluginListItem{
			Name:      p.Name,
			Version:   p.Version,
			Platform:  p.Platform,
			Size:      p.Size,
			SizeHuman: formatBytes(p.Size),
			Path:      p.Path,
		}
	}

	return kvxOpts.Write(items)
}

// formatBytes formats a byte count as a human-readable string (KB, MB, GB).
func formatBytes(b int64) string {
	const (
		kb = 1024
		mb = kb * 1024
		gb = mb * 1024
	)
	switch {
	case b >= gb:
		return fmt.Sprintf("%.1f GB", float64(b)/float64(gb))
	case b >= mb:
		return fmt.Sprintf("%.1f MB", float64(b)/float64(mb))
	case b >= kb:
		return fmt.Sprintf("%.1f KB", float64(b)/float64(kb))
	default:
		return fmt.Sprintf("%d B", b)
	}
}
