// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package get

import (
	"fmt"

	"github.com/oakwood-commons/scafctl/pkg/cmd/scafctl/get/authhandler"
	"github.com/oakwood-commons/scafctl/pkg/cmd/scafctl/get/celfunction"
	"github.com/oakwood-commons/scafctl/pkg/cmd/scafctl/get/commands"
	"github.com/oakwood-commons/scafctl/pkg/cmd/scafctl/get/gotmplfunction"
	"github.com/oakwood-commons/scafctl/pkg/cmd/scafctl/get/provider"
	"github.com/oakwood-commons/scafctl/pkg/cmd/scafctl/get/resolver"
	"github.com/oakwood-commons/scafctl/pkg/cmd/scafctl/get/solution"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/spf13/cobra"
)

func CommandGet(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
	cCmd := &cobra.Command{
		Use:          "get",
		Aliases:      []string{"g"},
		Short:        fmt.Sprintf("Gets %s things", path),
		SilenceUsage: true,
	}
	cCmd.AddCommand(authhandler.CommandAuthHandler(cliParams, ioStreams, fmt.Sprintf("%s/%s", path, cCmd.Use)))
	cCmd.AddCommand(provider.CommandProvider(cliParams, ioStreams, fmt.Sprintf("%s/%s", path, cCmd.Use)))
	cCmd.AddCommand(solution.CommandSolution(cliParams, ioStreams, fmt.Sprintf("%s/%s", path, cCmd.Use)))
	cCmd.AddCommand(resolver.CommandResolver(cliParams, ioStreams, fmt.Sprintf("%s/%s", path, cCmd.Use)))
	cCmd.AddCommand(celfunction.CommandCelFunction(cliParams, ioStreams, fmt.Sprintf("%s/%s", path, cCmd.Use)))
	cCmd.AddCommand(gotmplfunction.CommandGotmplFunction(cliParams, ioStreams, fmt.Sprintf("%s/%s", path, cCmd.Use)))
	cCmd.AddCommand(commands.CommandCommands(cliParams, ioStreams, path))
	return cCmd
}
