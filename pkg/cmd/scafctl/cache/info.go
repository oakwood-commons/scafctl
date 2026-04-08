// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package cache

import (
	"context"
	"fmt"
	"strings"

	"github.com/MakeNowJust/heredoc/v2"
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

// InfoOptions holds options for the info command.
type InfoOptions struct {
	CliParams *settings.Run
	IOStreams *terminal.IOStreams
	flags.KvxOutputFlags
}

// CommandInfo creates the info command.
func CommandInfo(cliParams *settings.Run, ioStreams *terminal.IOStreams, _ string) *cobra.Command {
	options := &InfoOptions{
		CliParams: cliParams,
		IOStreams: ioStreams,
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
		`), settings.CliBinaryName, cliParams.BinaryName),
		RunE: func(cmd *cobra.Command, _ []string) error {
			kvxOpts := flags.ToKvxOutputOptions(&options.KvxOutputFlags, kvx.WithIOStreams(ioStreams))
			return runInfo(cmd.Context(), options, kvxOpts)
		},
	}

	flags.AddKvxOutputFlagsToStruct(cmd, &options.KvxOutputFlags)

	return cmd
}

func runInfo(ctx context.Context, _ *InfoOptions, outputOpts *kvx.OutputOptions) error {
	w := writer.FromContext(ctx)
	if w == nil {
		return fmt.Errorf("writer not initialized in context")
	}

	// Collect cache info
	caches := []cachelib.Info{
		cachelib.GetCacheInfo("HTTP Cache", paths.HTTPCacheDir(), "HTTP response cache"),
		cachelib.GetCacheInfo("Build Cache", paths.BuildCacheDir(), "Incremental build fingerprints"),
		cachelib.GetCacheInfo("Artifact Cache", paths.ArtifactCacheDir(), "Downloaded catalog artifacts (TTL-based)"),
	}

	// Calculate totals
	var totalSize int64
	var totalFiles int64
	for _, cache := range caches {
		totalSize += cache.Size
		totalFiles += cache.FileCount
	}

	output := cachelib.InfoOutput{
		Caches:     caches,
		TotalSize:  totalSize,
		TotalHuman: format.Bytes(totalSize),
		TotalFiles: totalFiles,
	}

	// For structured output, use kvx
	if outputOpts.Format == kvx.OutputFormatJSON || outputOpts.Format == kvx.OutputFormatYAML {
		return outputOpts.Write(output)
	}

	// For table/default output, print human-readable message
	w.Infof("Cache Information")
	w.Plain("")

	// Find max name length for alignment
	maxNameLen := 0
	for _, c := range caches {
		if len(c.Name) > maxNameLen {
			maxNameLen = len(c.Name)
		}
	}

	for _, cache := range caches {
		w.Plainf("%-*s  %s (%d files)\n", maxNameLen, cache.Name+":", cache.SizeHuman, cache.FileCount)
		w.Plainf("%-*s  %s\n", maxNameLen, "", cache.Path)
		w.Plain("")
	}

	w.Plainf("Total: %s (%d files)\n", output.TotalHuman, output.TotalFiles)

	return nil
}
