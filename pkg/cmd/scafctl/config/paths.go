// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"context"
	"fmt"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/scafctl/pkg/cmd/flags"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/paths"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/kvx"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// Supported platforms for --platform flag.
var supportedPlatforms = paths.SupportedPlatforms

// PathInfo is an alias for paths.PathInfo.
//
// Deprecated: Use paths.PathInfo from pkg/paths instead.
type PathInfo = paths.PathInfo

// PathsOptions holds options for the config paths command.
type PathsOptions struct {
	IOStreams      *terminal.IOStreams
	CliParams      *settings.Run
	KvxOutputFlags flags.KvxOutputFlags
	Platform       string
}

// CommandPaths creates the 'config paths' command.
func CommandPaths(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
	opts := &PathsOptions{}

	cCmd := &cobra.Command{
		Use:   "paths",
		Short: "Show XDG-compliant paths used by scafctl",
		Long: heredoc.Doc(`
			Display all paths used by scafctl.

			scafctl follows the XDG Base Directory Specification for storing
			configuration, data, cache, and state files. This command shows
			the resolved paths for the current system.

			Use --platform to see illustrative paths for other operating systems.
			This is useful for documentation or cross-platform reference.

			Environment variables can override default paths:
			  - XDG_CONFIG_HOME: Configuration files
			  - XDG_DATA_HOME: User data (secrets, catalogs)
			  - XDG_CACHE_HOME: Cache files
			  - XDG_STATE_HOME: State files (logs, history)
			  - SCAFCTL_SECRETS_DIR: Override secrets location specifically

			Examples:
			  # Show all paths for current system
			  scafctl config paths

			  # Show paths for Linux
			  scafctl config paths --platform linux

			  # Show paths for Windows
			  scafctl config paths --platform windows

			  # Output as JSON
			  scafctl config paths -o json

			  # Output as YAML
			  scafctl config paths -o yaml
		`),
		Args: cobra.NoArgs,
		RunE: func(cCmd *cobra.Command, _ []string) error {
			cliParams.EntryPointSettings.Path = filepath.Join(path, cCmd.Use)
			ctx := settings.IntoContext(context.Background(), cliParams)

			if lgr := logger.FromContext(cCmd.Context()); lgr != nil {
				ctx = logger.WithLogger(ctx, lgr)
			}

			w := writer.FromContext(cCmd.Context())
			if w == nil {
				w = writer.New(ioStreams, cliParams)
			}
			ctx = writer.WithWriter(ctx, w)

			opts.IOStreams = ioStreams
			opts.CliParams = cliParams

			return opts.Run(ctx)
		},
		SilenceUsage: true,
	}

	flags.AddKvxOutputFlagsToStruct(cCmd, &opts.KvxOutputFlags)
	cCmd.Flags().StringVar(&opts.Platform, "platform", "", "Show illustrative paths for a specific platform (linux, darwin/macos, windows)")

	return cCmd
}

// Run executes the config paths command.
func (o *PathsOptions) Run(ctx context.Context) error {
	w := writer.MustFromContext(ctx)

	// Determine if we're showing paths for a different platform
	targetPlatform := o.Platform
	isIllustrative := false

	if targetPlatform != "" {
		// Normalize platform name
		targetPlatform = strings.ToLower(targetPlatform)
		if targetPlatform == "macos" {
			targetPlatform = "darwin"
		}

		// Validate platform
		if !slices.Contains(supportedPlatforms, targetPlatform) {
			err := fmt.Errorf("unsupported platform %q; supported platforms: linux, darwin (or macos), windows", o.Platform)
			w.Errorf("%v", err)
			return exitcode.WithCode(err, exitcode.InvalidInput)
		}

		// Check if it's different from current platform
		if targetPlatform != runtime.GOOS {
			isIllustrative = true
		}
	} else {
		targetPlatform = runtime.GOOS
	}

	var pathInfos []PathInfo

	if isIllustrative {
		// Generate illustrative paths for the target platform
		pathInfos = getIllustrativePaths(targetPlatform)
	} else {
		// Get real paths for current platform
		pathInfos = o.getRealPaths()
	}

	// Handle structured output formats
	outputOpts := flags.ToKvxOutputOptions(&o.KvxOutputFlags, kvx.WithIOStreams(o.IOStreams))
	if kvx.IsStructuredFormat(outputOpts.Format) {
		return outputOpts.Write(pathInfos)
	}

	// Table output
	w.Infof("scafctl Paths")
	w.Plain("")

	if isIllustrative {
		w.Plainf("Platform: %s (illustrative)\n", targetPlatform)
		w.Warningf("These are illustrative paths for reference only. They may not reflect actual paths on a real %s system.\n", targetPlatform)
	} else {
		w.Plainf("Platform: %s/%s\n", runtime.GOOS, runtime.GOARCH)
	}
	w.Plain("")

	// Find max name length for alignment
	maxNameLen := 0
	for _, p := range pathInfos {
		if len(p.Name) > maxNameLen {
			maxNameLen = len(p.Name)
		}
	}

	for _, p := range pathInfos {
		w.Plainf("%-*s  %s\n", maxNameLen, p.Name+":", p.Path)
	}

	w.Plain("")
	if !isIllustrative {
		w.Plainf("Override paths with XDG environment variables or SCAFCTL_SECRETS_DIR.\n")
	}

	return nil
}

// getRealPaths returns the actual paths for the current platform.
func (o *PathsOptions) getRealPaths() []PathInfo {
	return paths.AllPaths()
}

// getIllustrativePaths returns illustrative default paths for a given platform.
//
// Deprecated: Use paths.IllustrativePaths from pkg/paths instead.
func getIllustrativePaths(platform string) []PathInfo {
	return paths.IllustrativePaths(platform)
}
