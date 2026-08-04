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

// CommandRenameCall creates the `refactor rename call` command.
func CommandRenameCall(cliParams *settings.Run, _ *terminal.IOStreams, path string) *cobra.Command {
	return newRenameCommand(cliParams, path, renameCommandConfig{
		use:   "call <old-name> <new-name>",
		short: "Rename a reusable call and update every reference to it",
		long: heredoc.Doc(`
			Rename a spec.calls definition and rewrite every reference to it --
			the 'call:' entries in resolver with/transform/validate steps and in
			workflow actions, plus the definition itself -- preserving comments,
			key order, and formatting.

			Calls are only referenced structurally (via the 'call:' field), never
			from CEL or templates. The rename refuses to run when any reference to
			the target call cannot be located byte-exact, so it never performs a
			partial rewrite that would silently break the solution.
		`),
		example: heredoc.Doc(`
			# Rename call 'fetch' to 'download' in the discovered solution
			scafctl refactor rename call fetch download

			# Target a specific file and preview the change without writing
			scafctl refactor rename call fetch download -f ./solution.yaml --dry-run
		`),
		kind:   "call",
		rename: pkgrefactor.RenameCall,
	})
}
