package get

import (
	"fmt"

	"github.com/kcloutie/scafctl/pkg/cmd/scafctl/get/solution"
	"github.com/kcloutie/scafctl/pkg/settings"
	"github.com/kcloutie/scafctl/pkg/terminal"
	"github.com/spf13/cobra"
)

func CommandGet(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
	cCmd := &cobra.Command{
		Use:          "get",
		Aliases:      []string{"g"},
		Short:        fmt.Sprintf("Gets %s things", settings.CliBinaryName),
		SilenceUsage: true,
	}
	cCmd.AddCommand(solution.CommandSolution(cliParams, ioStreams, fmt.Sprintf("%s/%s", path, cCmd.Use)))
	return cCmd
}
