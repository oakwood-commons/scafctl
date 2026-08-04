// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package refactor

import (
	"github.com/MakeNowJust/heredoc/v2"
	pkgrefactor "github.com/oakwood-commons/scafctl/pkg/refactor"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/spf13/cobra"
)

// CommandRenameAction creates the `refactor rename action` command.
func CommandRenameAction(cliParams *settings.Run, _ *terminal.IOStreams, path string) *cobra.Command {
	return newRenameCommand(cliParams, path, renameCommandConfig{
		use:   "action <old-name> <new-name>",
		short: "Rename a workflow action and update every reference to it",
		long: heredoc.Doc(`
			Rename a workflow action and rewrite every reference to it --
			dependsOn entries, CEL '__actions.name' uses, explicit template
			'.__actions.name' uses, and the definition itself -- preserving
			comments, key order, and formatting.

			The rename refuses to run when any reference to the target action
			cannot be located byte-exact, so it never performs a partial rewrite
			that would silently break the solution. Note: an action's 'alias' is
			a separate name and is not changed by renaming the action.
		`),
		example: heredoc.Doc(`
			# Rename action 'deploy' to 'release' in the discovered solution
			scafctl refactor rename action deploy release

			# Target a specific file and preview the change without writing
			scafctl refactor rename action deploy release -f ./solution.yaml --dry-run
		`),
		kind:   "action",
		rename: pkgrefactor.RenameAction,
	})
}
