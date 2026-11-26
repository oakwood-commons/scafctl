// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package run

import (
	"fmt"

	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution/source"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/spf13/cobra"
)

// CommandRun creates the 'run' command that executes solutions and other runnable artifacts.
func CommandRun(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
	cCmd := &cobra.Command{
		Use:     "run",
		Aliases: []string{"r"},
		Short:   fmt.Sprintf("Runs %s solutions, resolvers, actions, and providers", path),
		Long: `Run executes solutions, resolvers, actions, or individual providers.

Resolvers within the same dependency phase execute concurrently for optimal performance.
Actions are executed in dependency phases after resolvers complete.

SUBCOMMANDS:
  solution  Run a solution (resolvers + actions)
  action    Run specific actions from a solution by name
  resolver  Run resolvers only (for debugging/inspection)
  provider  Run a single provider directly (for testing/debugging)`,
		SilenceUsage: true,
		Args: func(c *cobra.Command, args []string) error {
			if len(args) == 0 {
				return nil
			}
			bin := settings.CliBinaryName
			if cliParams != nil && cliParams.BinaryName != "" {
				bin = cliParams.BinaryName
			}
			refArg := args[0]
			if cls, err := source.Classify(args[0]); err == nil && !cls.Kind.IsReference() {
				refArg = "-f " + args[0]
			}
			return fmt.Errorf("unknown command %q for %q; specify what to run: \"%s run solution %s\" or \"%s run action %s\"",
				args[0], c.CommandPath(), bin, refArg, bin, refArg)
		},
		RunE: func(c *cobra.Command, _ []string) error {
			return c.Help()
		},
	}

	runPath := fmt.Sprintf("%s/%s", path, cCmd.Use)
	cCmd.AddCommand(CommandSolution(cliParams, ioStreams, runPath))
	cCmd.AddCommand(CommandResolver(cliParams, ioStreams, runPath))
	cCmd.AddCommand(CommandAction(cliParams, ioStreams, runPath))
	cCmd.AddCommand(CommandProvider(cliParams, ioStreams, runPath))

	return cCmd
}
