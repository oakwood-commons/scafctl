// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package get

import (
	"fmt"

	"github.com/oakwood-commons/scafctl/pkg/cmd/scafctl/get/celfunction"
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
		Short:        fmt.Sprintf("Gets %s things", settings.CliBinaryName),
		SilenceUsage: true,
	}
	cCmd.AddCommand(provider.CommandProvider(cliParams, ioStreams, fmt.Sprintf("%s/%s", path, cCmd.Use)))
	cCmd.AddCommand(solution.CommandSolution(cliParams, ioStreams, fmt.Sprintf("%s/%s", path, cCmd.Use)))
	cCmd.AddCommand(resolver.CommandResolver(cliParams, ioStreams, fmt.Sprintf("%s/%s", path, cCmd.Use)))
	cCmd.AddCommand(celfunction.CommandCelFunction(cliParams, ioStreams, fmt.Sprintf("%s/%s", path, cCmd.Use)))
	return cCmd
}
