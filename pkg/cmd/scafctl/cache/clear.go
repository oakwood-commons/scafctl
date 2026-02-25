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
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/paths"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/input"
	"github.com/oakwood-commons/scafctl/pkg/terminal/kvx"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// ClearOptions holds options for the clear command.
type ClearOptions struct {
	CliParams *settings.Run
	IOStreams *terminal.IOStreams
	Kind      string
	Name      string
	Force     bool
	flags.KvxOutputFlags
}

// ClearOutput is an alias for cachelib.ClearOutput.
//
// Deprecated: Use cache.ClearOutput from pkg/cache instead.
type ClearOutput = cachelib.ClearOutput

// Kind is an alias for cachelib.Kind.
//
// Deprecated: Use cache.Kind from pkg/cache instead.
type Kind = cachelib.Kind

const (
	// KindAll clears all caches.
	KindAll Kind = cachelib.KindAll
	// KindHTTP clears the HTTP response cache.
	KindHTTP Kind = cachelib.KindHTTP
	// KindBuild clears the build cache (incremental build fingerprints).
	KindBuild Kind = cachelib.KindBuild
)

// ValidKinds lists all valid cache kinds.
var ValidKinds = cachelib.ValidKinds

// CommandClear creates the clear command.
func CommandClear(cliParams *settings.Run, ioStreams *terminal.IOStreams, _ string) *cobra.Command {
	options := &ClearOptions{
		CliParams: cliParams,
		IOStreams: ioStreams,
	}

	cmd := &cobra.Command{
		Use:          "clear",
		Aliases:      []string{"clean", "rm"},
		Short:        "Clear cached content",
		SilenceUsage: true,
		Long: heredoc.Doc(`
			Clear cached content from the scafctl cache.

			By default, clears all cached content. Use --kind to clear a specific
			type of cache, or --name to clear cache entries matching a pattern.

			Cache kinds:
			  all   - Clear all caches (default)
			  http  - Clear HTTP response cache
			  build - Clear build cache (incremental build fingerprints)

			Examples:
			  # Clear all caches
			  scafctl cache clear

			  # Clear only HTTP cache
			  scafctl cache clear --kind http

			  # Clear cache entries matching a pattern
			  scafctl cache clear --name "api.github.com*"

			  # Skip confirmation prompt
			  scafctl cache clear --force

			  # Show what would be cleared (JSON output)
			  scafctl cache clear -o json
		`),
		RunE: func(cmd *cobra.Command, _ []string) error {
			kvxOpts := flags.ToKvxOutputOptions(&options.KvxOutputFlags, kvx.WithIOStreams(ioStreams))
			return runClear(cmd.Context(), options, kvxOpts)
		},
	}

	flags.AddKvxOutputFlagsToStruct(cmd, &options.KvxOutputFlags)
	cmd.Flags().StringVarP(&options.Kind, "kind", "k", "", fmt.Sprintf("Kind of cache to clear (%s)", strings.Join(ValidKinds, ", ")))
	cmd.Flags().StringVarP(&options.Name, "name", "n", "", "Clear cache entries matching this pattern (supports glob wildcards)")
	cmd.Flags().BoolVarP(&options.Force, "force", "f", false, "Skip confirmation prompt")

	return cmd
}

func runClear(ctx context.Context, options *ClearOptions, outputOpts *kvx.OutputOptions) error {
	w := writer.MustFromContext(ctx)

	// Validate kind if provided
	kind := KindAll
	if options.Kind != "" {
		normalizedKind := strings.ToLower(options.Kind)
		switch normalizedKind {
		case string(KindAll):
			kind = KindAll
		case string(KindHTTP):
			kind = KindHTTP
		case string(KindBuild):
			kind = KindBuild
		default:
			err := fmt.Errorf("invalid cache kind %q; valid kinds: %s", options.Kind, strings.Join(ValidKinds, ", "))
			w.Errorf("%v", err)
			return exitcode.WithCode(err, exitcode.InvalidInput)
		}
	}

	// Build description for confirmation
	description := "all cached content"
	if kind != KindAll {
		description = fmt.Sprintf("%s cache", kind)
	}
	if options.Name != "" {
		description += fmt.Sprintf(" matching %q", options.Name)
	}

	// Confirm action (skip in force mode or quiet mode)
	skipConfirmation := options.Force || options.CliParams.IsQuiet
	if !skipConfirmation {
		in := input.MustFromContext(ctx)
		confirmed, err := in.Confirm(input.NewConfirmOptions().
			WithPrompt(fmt.Sprintf("Clear %s?", description)).
			WithDefault(false))
		if err != nil {
			err := fmt.Errorf("failed to read confirmation: %w", err)
			w.Errorf("%v", err)
			return exitcode.WithCode(err, exitcode.GeneralError)
		}
		if !confirmed {
			w.Info("Cache clear cancelled")
			return nil
		}
	}

	// Perform the clear operation
	var totalFiles int64
	var totalBytes int64

	switch kind {
	case KindAll:
		// Clear all cache directories
		files, bytes, err := cachelib.ClearDirectory(paths.CacheDir(), options.Name)
		if err != nil {
			w.Errorf("failed to clear cache: %v", err)
			return exitcode.WithCode(err, exitcode.GeneralError)
		}
		totalFiles += files
		totalBytes += bytes

	case KindHTTP:
		files, bytes, err := cachelib.ClearDirectory(paths.HTTPCacheDir(), options.Name)
		if err != nil {
			w.Errorf("failed to clear HTTP cache: %v", err)
			return exitcode.WithCode(err, exitcode.GeneralError)
		}
		totalFiles += files
		totalBytes += bytes

	case KindBuild:
		files, bytes, err := cachelib.ClearDirectory(paths.BuildCacheDir(), options.Name)
		if err != nil {
			w.Errorf("failed to clear build cache: %v", err)
			return exitcode.WithCode(err, exitcode.GeneralError)
		}
		totalFiles += files
		totalBytes += bytes
	}

	// Format output
	output := ClearOutput{
		RemovedFiles: totalFiles,
		RemovedBytes: totalBytes,
		RemovedHuman: cachelib.FormatBytes(totalBytes),
		Kind:         string(kind),
	}
	if options.Name != "" {
		output.Name = options.Name
	}

	// For structured output, use kvx
	if outputOpts.Format == kvx.OutputFormatJSON || outputOpts.Format == kvx.OutputFormatYAML {
		return outputOpts.Write(output)
	}

	// For table/default output, print human-readable message
	if totalFiles == 0 {
		w.Infof("No cached content found")
	} else {
		w.Successf("Cleared cache")
		w.Infof("  Removed files: %d", totalFiles)
		w.Infof("  Reclaimed: %s", output.RemovedHuman)
	}

	return nil
}
