// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package cache provides commands for managing the scafctl cache.
package cache

import (
	"fmt"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/spf13/cobra"
)

// CommandCache creates the cache command group.
func CommandCache(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
	cmd := &cobra.Command{
		Use:          "cache",
		Short:        fmt.Sprintf("Manage the %s cache", path),
		SilenceUsage: true,
		Long: heredoc.Doc(`
			Manage the scafctl cache.

			The cache stores HTTP responses to reduce network requests.

			The cache is stored at:
			  - Linux: ~/.cache/scafctl/
			  - macOS: ~/.cache/scafctl/
			  - Windows: %LOCALAPPDATA%\cache\scafctl\
		`),
	}

	cmd.AddCommand(CommandClear(cliParams, ioStreams, path))
	cmd.AddCommand(CommandInfo(cliParams, ioStreams, path))

	return cmd
}
