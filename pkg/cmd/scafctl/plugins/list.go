// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package plugins

import (
	"context"

	"github.com/MakeNowJust/heredoc/v2"
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
	CliParams *settings.Run
	IOStreams *terminal.IOStreams
	CacheDir  string
	flags.KvxOutputFlags
}

// CommandList creates the list subcommand.
func CommandList(cliParams *settings.Run, ioStreams *terminal.IOStreams, _ string) *cobra.Command {
	opts := &ListOptions{
		CliParams: cliParams,
		IOStreams: ioStreams,
	}

	cmd := &cobra.Command{
		Use:          "list",
		Aliases:      []string{"ls"},
		Short:        "List cached plugin binaries",
		SilenceUsage: true,
		Long: heredoc.Doc(`
			List all plugin binaries stored in the local plugin cache.

			Shows the name, version, platform, size, and path for each
			cached binary.

			Examples:
			  # List all cached plugins
			  scafctl plugins list

			  # List in JSON format
			  scafctl plugins list -o json
		`),
		RunE: func(cmd *cobra.Command, _ []string) error {
			w := writer.FromContext(cmd.Context())
			if w == nil {
				w = writer.New(ioStreams, cliParams)
			}
			ctx := writer.WithWriter(cmd.Context(), w)
			kvxOpts := flags.ToKvxOutputOptions(&opts.KvxOutputFlags, kvx.WithIOStreams(ioStreams))

			return runList(ctx, opts, kvxOpts)
		},
	}

	cmd.Flags().StringVar(&opts.CacheDir, "cache-dir", "", "Plugin cache directory (default: $XDG_CACHE_HOME/scafctl/plugins/)")
	flags.AddKvxOutputFlagsToStruct(cmd, &opts.KvxOutputFlags)

	return cmd
}

func runList(ctx context.Context, opts *ListOptions, kvxOpts *kvx.OutputOptions) error {
	w := writer.MustFromContext(ctx)

	cache := plugin.NewCache(opts.CacheDir)
	cached, err := cache.List()
	if err != nil {
		w.Errorf("failed to list cached plugins: %v", err)
		return err
	}

	if len(cached) == 0 {
		w.Infof("No plugins cached. Use 'scafctl plugins install' to fetch plugins.")
		return nil
	}

	return kvxOpts.Write(cached)
}
