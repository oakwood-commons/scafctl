package run

import (
	"fmt"

	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/spf13/cobra"
)

// CommandRun creates the 'run' command that executes solutions and other runnable artifacts.
func CommandRun(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
	cCmd := &cobra.Command{
		Use:     "run",
		Aliases: []string{"r"},
		Short:   fmt.Sprintf("Runs %s solutions", settings.CliBinaryName),
		Long: `Run executes solutions by resolving all defined resolvers in dependency order.

Resolvers within the same dependency phase execute concurrently for optimal performance.
The command outputs the resolved values in JSON format by default.

SUBCOMMANDS:
  solution  Run a solution's resolvers (default)
  workflow  Run a solution's complete workflow (resolvers + actions)`,
		SilenceUsage: true,
	}

	cCmd.AddCommand(CommandSolution(cliParams, ioStreams, fmt.Sprintf("%s/%s", path, cCmd.Use)))
	cCmd.AddCommand(CommandWorkflow(cliParams, ioStreams, fmt.Sprintf("%s/%s", path, cCmd.Use)))

	return cCmd
}
