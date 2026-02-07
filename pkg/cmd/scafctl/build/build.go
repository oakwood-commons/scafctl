// Package build provides the build command for packaging artifacts into the local catalog.
package build

import (
	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/spf13/cobra"
)

// CommandBuild creates the build command group.
func CommandBuild(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
	cmd := &cobra.Command{
		Use:          "build",
		Aliases:      []string{"b", "package"},
		Short:        "Build and package artifacts into the local catalog",
		SilenceUsage: true,
		Long: heredoc.Doc(`
			Build and package artifacts into the local catalog.

			The build command packages solutions and plugins as OCI artifacts
			in the local catalog for versioned storage and later execution.

			The local catalog is stored at:
			  - Linux: ~/.local/share/scafctl/catalog/
			  - macOS: ~/Library/Application Support/scafctl/catalog/
			  - Windows: %LOCALAPPDATA%\scafctl\catalog\
		`),
	}

	cmd.AddCommand(CommandBuildSolution(cliParams, ioStreams, path))

	return cmd
}
