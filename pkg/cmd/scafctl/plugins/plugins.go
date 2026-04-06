// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package plugins provides commands for managing scafctl plugins.
package plugins

import (
	"fmt"
	"strings"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/spf13/cobra"
)

// CommandPlugins creates the plugins command group.
func CommandPlugins(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
	cmd := &cobra.Command{
		Use:          "plugins",
		Short:        fmt.Sprintf("Manage %s plugins", path),
		SilenceUsage: true,
		Long: strings.ReplaceAll(heredoc.Doc(`
			Manage scafctl plugins.

			Plugins extend scafctl with additional providers and auth handlers
			distributed as binary artifacts through OCI catalogs.

			Commands:
			  install  - Pre-fetch plugin binaries declared in a solution
			  list     - List cached plugin binaries
		`), settings.CliBinaryName, cliParams.BinaryName),
	}

	cmd.AddCommand(CommandInstall(cliParams, ioStreams, path))
	cmd.AddCommand(CommandList(cliParams, ioStreams, path))

	return cmd
}
