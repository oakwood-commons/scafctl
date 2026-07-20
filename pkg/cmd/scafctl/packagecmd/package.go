// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package packagecmd provides the "package" command for packaging artifacts into the local catalog.
package packagecmd

import (
	"strings"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/scafctl/pkg/cmd/cmdutil"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/spf13/cobra"
)

// CommandPackage creates the package command group.
func CommandPackage(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
	cmd := cmdutil.MakeHelpOnlyGroup(&cobra.Command{
		Use:          "package",
		Aliases:      []string{"build", "b"},
		Short:        "Package artifacts into the local catalog",
		SilenceUsage: true,
		Long: strings.ReplaceAll(heredoc.Doc(`
			Package solutions and plugins as OCI artifacts into the local catalog.

			The package command stores solutions and plugins as OCI artifacts
			in the local catalog for versioned storage and later execution.

			"build" is a backward-compatible alias for "package".

			The local catalog is stored at:
			  - Linux: ~/.local/share/scafctl/catalog/
			  - macOS: ~/.local/share/scafctl/catalog/
			  - Windows: %LOCALAPPDATA%\scafctl\catalog\
		`), settings.CliBinaryName, cliParams.BinaryName),
	})

	cmd.AddCommand(CommandPackageSolution(cliParams, ioStreams, path))
	cmd.AddCommand(CommandPackagePlugin(cliParams, ioStreams, path))

	return cmd
}
