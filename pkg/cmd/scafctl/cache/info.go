// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package cache

import (
	"context"
	"strings"

	_ "embed"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/kvx/pkg/tui"
	cachelib "github.com/oakwood-commons/scafctl/pkg/cache"
	"github.com/oakwood-commons/scafctl/pkg/cmd/flags"
	"github.com/oakwood-commons/scafctl/pkg/paths"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/format"
	"github.com/oakwood-commons/scafctl/pkg/terminal/kvx"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

//go:embed cache_info_schema.json
var cacheInfoSchemaJSON []byte

// InfoOptions holds options for the info command.
type InfoOptions struct {
	CliParams *settings.Run
	IOStreams *terminal.IOStreams
	flags.KvxOutputFlags
}

// CommandInfo creates the info command.
func CommandInfo(cliParams *settings.Run, ioStreams *terminal.IOStreams, _ string) *cobra.Command {
	options := &InfoOptions{
		CliParams:      cliParams,
		IOStreams:      ioStreams,
		KvxOutputFlags: flags.KvxOutputFlags{AppName: cliParams.BinaryName},
	}

	cmd := &cobra.Command{
		Use:          "info",
		Aliases:      []string{"status", "show"},
		Short:        "Show cache information",
		SilenceUsage: true,
		Long: strings.ReplaceAll(heredoc.Doc(`
			Display information about scafctl cache usage.

			Shows the size and file count for each cache directory.

			Examples:
			  # Show cache information
			  scafctl cache info

			  # Show cache info as JSON
			  scafctl cache info -o json

			  # Show cache info as a table
			  scafctl cache info -o table

			  # Browse caches interactively
			  scafctl cache info -i
		`), settings.CliBinaryName, cliParams.BinaryName),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runInfo(cmd.Context(), options)
		},
	}

	flags.AddKvxOutputFlagsToStruct(cmd, &options.KvxOutputFlags)

	return cmd
}

func runInfo(ctx context.Context, opts *InfoOptions) error {
	// Collect cache info
	caches := []cachelib.Info{
		cachelib.GetCacheInfo("HTTP Cache", paths.HTTPCacheDir(), "HTTP response cache"),
		cachelib.GetCacheInfo("Build Cache", paths.BuildCacheDir(), "Incremental build fingerprints"),
		cachelib.GetCacheInfo("Artifact Cache", paths.ArtifactCacheDir(), "Downloaded catalog artifacts (TTL-based)"),
	}

	// Calculate totals
	var totalSize int64
	var totalFiles int64
	for _, c := range caches {
		totalSize += c.Size
		totalFiles += c.FileCount
	}

	kvxOpts := flags.ToKvxOutputOptions(&opts.KvxOutputFlags,
		kvx.WithOutputContext(ctx),
		kvx.WithOutputNoColor(opts.CliParams.NoColor),
		kvx.WithOutputAppName(opts.CliParams.BinaryName+" cache info"),
		kvx.WithOutputDisplaySchemaJSON(cacheInfoSchemaJSON),
		kvx.WithIOStreams(opts.IOStreams),
		kvx.WithOutputColumnOrder([]string{"name", "sizeHuman", "fileCount", "description"}),
		kvx.WithOutputColumnHints(map[string]tui.ColumnHint{
			"name":        {MaxWidth: 20, Priority: 10, DisplayName: "Name"},
			"sizeHuman":   {MaxWidth: 12, Priority: 9, DisplayName: "Size"},
			"fileCount":   {MaxWidth: 10, Priority: 8, DisplayName: "Files", Align: "right"},
			"description": {MaxWidth: 40, Priority: 5, Flex: true, DisplayName: "Description"},
			"path":        {Hidden: true},
			"size":        {Hidden: true},
		}),
	)

	// Structured formats (JSON/YAML/TOML) use the InfoOutput wrapper with totals.
	// Visual formats (table/list/interactive) and CSV use the flat []Info array.
	// Mermaid is routed through writeKvx by kvx.Write() regardless of this branch.
	if kvxOpts.Format == kvx.OutputFormatJSON || kvxOpts.Format == kvx.OutputFormatYAML || kvxOpts.Format == kvx.OutputFormatTOML {
		output := cachelib.InfoOutput{
			Caches:     caches,
			TotalSize:  totalSize,
			TotalHuman: format.Bytes(totalSize),
			TotalFiles: totalFiles,
		}
		return kvxOpts.Write(output)
	}

	// Capture original format before Write() potentially mutates it (non-TTY fallback).
	originalFormat := kvxOpts.Format

	if err := kvxOpts.Write(caches); err != nil {
		return err
	}

	// Print totals summary on stderr for human-readable formats
	if kvx.IsKvxFormat(originalFormat) || originalFormat == kvx.OutputFormatText {
		w := writer.FromContext(ctx)
		if w != nil {
			w.PlainStderrf("Total: %s (%d files)\n", format.Bytes(totalSize), totalFiles)
		}
	}

	return nil
}
