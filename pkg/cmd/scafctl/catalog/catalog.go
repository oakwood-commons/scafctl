// Package catalog provides commands for inspecting and managing the local catalog.
package catalog

import (
	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/spf13/cobra"
)

// CommandCatalog creates the catalog command group.
func CommandCatalog(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "catalog",
		Aliases: []string{"cat"},
		Short:   "Manage the local artifact catalog",
		Long: heredoc.Doc(`
			Manage the local artifact catalog.

			The catalog stores solutions and plugins as versioned OCI artifacts
			for later execution with 'scafctl run'.

			The local catalog is stored at:
			  - Linux: ~/.local/share/scafctl/catalog/
			  - macOS: ~/Library/Application Support/scafctl/catalog/
			  - Windows: %LOCALAPPDATA%\scafctl\catalog\
		`),
	}

	cmd.AddCommand(CommandList(cliParams, ioStreams, path))
	cmd.AddCommand(CommandInspect(cliParams, ioStreams, path))
	cmd.AddCommand(CommandDelete(cliParams, ioStreams, path))
	cmd.AddCommand(CommandPrune(cliParams, ioStreams, path))
	cmd.AddCommand(CommandSave(cliParams, ioStreams, path))
	cmd.AddCommand(CommandLoad(cliParams, ioStreams, path))

	return cmd
}
