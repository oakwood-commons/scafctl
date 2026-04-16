// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package plugins

import (
	"context"
	"fmt"
	"strings"

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
	BinaryName string
	CliParams  *settings.Run
	IOStreams  *terminal.IOStreams
	CacheDir   string
	flags.KvxOutputFlags
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

			Shows the name, version, platform, size, and path for each
			cached binary.

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
			kvxOpts := flags.ToKvxOutputOptions(&opts.KvxOutputFlags, kvx.WithIOStreams(ioStreams))

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

	cache := plugin.NewCache(opts.CacheDir)
	cached, err := cache.List()
	if err != nil {
		w.Errorf("failed to list cached plugins: %v", err)
		return err
	}

	if len(cached) == 0 {
		w.Infof("No plugins cached. Use '%s plugins install' to fetch plugins.", opts.BinaryName)
		return nil
	}

	return kvxOpts.Write(cached)
}
