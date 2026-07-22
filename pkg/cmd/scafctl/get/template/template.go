// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package template

import (
	"fmt"

	"github.com/oakwood-commons/scafctl/pkg/cmd/cmdutil"
	"github.com/oakwood-commons/scafctl/pkg/cmd/scafctl/get/gotmplfunction"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/spf13/cobra"
)

// CommandTemplate creates the 'get template' command group. A bare invocation
// shows help; its 'functions' child is the canonical form of the former
// 'get go-template-functions'.
func CommandTemplate(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
	cCmd := cmdutil.MakeHelpOnlyGroup(&cobra.Command{
		Use:          "template",
		Short:        "Go template resources",
		SilenceUsage: true,
	})

	cmdPath := fmt.Sprintf("%s/%s", path, cCmd.Use)
	cCmd.AddCommand(gotmplfunction.CommandFunctions(cliParams, ioStreams, cmdPath))

	return cCmd
}
