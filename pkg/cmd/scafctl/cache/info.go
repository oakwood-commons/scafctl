// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package cache

import (
	"context"
	"os"
	"path/filepath"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/scafctl/pkg/cmd/flags"
	"github.com/oakwood-commons/scafctl/pkg/paths"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
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

// Info represents information about a cache directory.
type Info struct {
	Name        string `json:"name" yaml:"name"`
	Path        string `json:"path" yaml:"path"`
	Size        int64  `json:"size" yaml:"size"`
	SizeHuman   string `json:"sizeHuman" yaml:"sizeHuman"`
	FileCount   int64  `json:"fileCount" yaml:"fileCount"`
	Description string `json:"description" yaml:"description"`
}

// InfoOutput represents the info command output.
type InfoOutput struct {
	Caches     []Info `json:"caches" yaml:"caches"`
	TotalSize  int64  `json:"totalSize" yaml:"totalSize"`
	TotalHuman string `json:"totalHuman" yaml:"totalHuman"`
	TotalFiles int64  `json:"totalFiles" yaml:"totalFiles"`
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
		Long: heredoc.Doc(`
			Display information about scafctl cache usage.

			Shows the size and file count for each cache directory.

			Examples:
			  # Show cache information
			  scafctl cache info

			  # Show cache info as JSON
			  scafctl cache info -o json
		`),
		RunE: func(cmd *cobra.Command, _ []string) error {
			kvxOpts := flags.ToKvxOutputOptions(&options.KvxOutputFlags, kvx.WithIOStreams(ioStreams))
			return runInfo(cmd.Context(), options, kvxOpts)
		},
	}

	flags.AddKvxOutputFlagsToStruct(cmd, &options.KvxOutputFlags)

	return cmd
}

func runInfo(ctx context.Context, _ *InfoOptions, outputOpts *kvx.OutputOptions) error {
	w := writer.MustFromContext(ctx)

	// Collect cache info
	caches := []Info{
		getCacheInfo("HTTP Cache", paths.HTTPCacheDir(), "HTTP response cache"),
	}

	// Calculate totals
	var totalSize int64
	var totalFiles int64
	for _, cache := range caches {
		totalSize += cache.Size
		totalFiles += cache.FileCount
	}

	output := InfoOutput{
		Caches:     caches,
		TotalSize:  totalSize,
		TotalHuman: formatBytes(totalSize),
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

// getCacheInfo returns information about a cache directory.
func getCacheInfo(name, dir, description string) Info {
	info := Info{
		Name:        name,
		Path:        dir,
		Description: description,
	}

	// Check if directory exists
	stat, err := os.Stat(dir)
	if os.IsNotExist(err) || !stat.IsDir() {
		info.SizeHuman = "0 B"
		return info
	}

	// Calculate size and file count
	_ = filepath.Walk(dir, func(_ string, fileInfo os.FileInfo, walkErr error) error {
		if walkErr != nil || fileInfo.IsDir() {
			return nil //nolint:nilerr // Intentionally ignoring walk errors
		}
		info.Size += fileInfo.Size()
		info.FileCount++
		return nil
	})

	info.SizeHuman = formatBytes(info.Size)
	return info
}
