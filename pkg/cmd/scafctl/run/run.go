// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

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
		Short:   fmt.Sprintf("Runs %s solutions, resolvers, and providers", settings.CliBinaryName),
		Long: `Run executes solutions, resolvers, or individual providers.

Resolvers within the same dependency phase execute concurrently for optimal performance.
Actions are executed in dependency phases after resolvers complete.

SUBCOMMANDS:
  solution  Run a solution (resolvers + actions)
  resolver  Run resolvers only (for debugging/inspection)
  provider  Run a single provider directly (for testing/debugging)`,
		SilenceUsage: true,
	}

	runPath := fmt.Sprintf("%s/%s", path, cCmd.Use)
	cCmd.AddCommand(CommandSolution(cliParams, ioStreams, runPath))
	cCmd.AddCommand(CommandResolver(cliParams, ioStreams, runPath))
	cCmd.AddCommand(CommandProvider(cliParams, ioStreams, runPath))

	return cCmd
}
