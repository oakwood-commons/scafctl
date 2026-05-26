// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"fmt"
	"strings"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/spf13/cobra"
)

// CommandProfile creates the 'auth profile' command group.
func CommandProfile(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "profile",
		Short: "Manage auth profiles",
		Long: strings.ReplaceAll(heredoc.Doc(`
			Manage authentication profiles for scafctl.

			Profiles allow you to maintain multiple credentials for the same
			auth handler (e.g. work and personal GitHub accounts).

			Use 'scafctl auth profile delete <handler> <profile>' to remove a profile.
		`), settings.CliBinaryName, cliParams.BinaryName),
		SilenceUsage: true,
	}

	cmdPath := fmt.Sprintf("%s/%s", path, cmd.Use)
	cmd.AddCommand(CommandProfileDelete(cliParams, ioStreams, cmdPath))

	return cmd
}
