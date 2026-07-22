// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package cel

import (
	"fmt"

	"github.com/oakwood-commons/scafctl/pkg/cmd/cmdutil"
	"github.com/oakwood-commons/scafctl/pkg/cmd/scafctl/get/celfunction"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/spf13/cobra"
)

// CommandCel creates the 'get cel' command group. A bare invocation shows help;
// its 'functions' child is the canonical form of the former 'get cel-functions'.
func CommandCel(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
	cCmd := cmdutil.MakeHelpOnlyGroup(&cobra.Command{
		Use:          "cel",
		Short:        "CEL resources",
		SilenceUsage: true,
	})

	cmdPath := fmt.Sprintf("%s/%s", path, cCmd.Use)
	cCmd.AddCommand(celfunction.CommandFunctions(cliParams, ioStreams, cmdPath))

	return cCmd
}
