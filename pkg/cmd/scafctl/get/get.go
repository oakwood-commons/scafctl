// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package get

import (
	"fmt"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/scafctl/pkg/cmd/scafctl/get/celfunction"
	"github.com/oakwood-commons/scafctl/pkg/cmd/scafctl/get/commands"
	getexamples "github.com/oakwood-commons/scafctl/pkg/cmd/scafctl/get/examples"
	"github.com/oakwood-commons/scafctl/pkg/cmd/scafctl/get/gotmplfunction"
	"github.com/oakwood-commons/scafctl/pkg/cmd/scafctl/get/provider"
	"github.com/oakwood-commons/scafctl/pkg/cmd/scafctl/get/solution"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/spf13/cobra"
)

func CommandGet(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
	cCmd := &cobra.Command{
		Use:     "get",
		Aliases: []string{"g"},
		Short:   "List resources, or show one by name",
		Long: heredoc.Doc(`
			List resources of a kind, or show a single one by name.

			With no name, 'get' lists what exists (e.g. 'get solution' lists
			catalog solutions). With a name, it shows that one item's summary
			(e.g. 'get solution my-app').

			Related commands (for a solution):
			  get solution              List/show solutions from the catalog
			  inspect solution          Full structure of a specific solution file
			  inspect solution --usage  How to run a solution (parameters + actions)
			  explain solution          Schema of the Solution kind (what fields are valid)
		`),
		SilenceUsage: true,
	}
	cCmd.AddCommand(provider.CommandProvider(cliParams, ioStreams, fmt.Sprintf("%s/%s", path, cCmd.Use)))
	cCmd.AddCommand(solution.CommandSolution(cliParams, ioStreams, fmt.Sprintf("%s/%s", path, cCmd.Use)))
	cCmd.AddCommand(getexamples.CommandExamples(cliParams, ioStreams, fmt.Sprintf("%s/%s", path, cCmd.Use)))
	cCmd.AddCommand(celfunction.CommandCelFunction(cliParams, ioStreams, fmt.Sprintf("%s/%s", path, cCmd.Use)))
	cCmd.AddCommand(gotmplfunction.CommandGotmplFunction(cliParams, ioStreams, fmt.Sprintf("%s/%s", path, cCmd.Use)))
	cCmd.AddCommand(commands.CommandCommands(cliParams, ioStreams, path))
	return cCmd
}
