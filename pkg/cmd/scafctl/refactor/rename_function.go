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

// CommandRenameFunction creates the `refactor rename function` command.
func CommandRenameFunction(cliParams *settings.Run, _ *terminal.IOStreams, path string) *cobra.Command {
	return newRenameCommand(cliParams, path, renameCommandConfig{
		use:   "function <old-name> <new-name>",
		short: "Rename an author-defined function and update every invocation",
		long: heredoc.Doc(`
			Rename a spec.functions definition and rewrite every invocation of it
			-- '{{ name ... }}' calls across all templates, including inside other
			function bodies -- plus the definition itself, preserving comments,
			key order, and formatting.

			Only author-defined functions are renamed; built-in and extension
			functions (printf, upper, ...) that share the new name are unaffected.
			The rename refuses to run when any invocation cannot be located
			byte-exact, so it never performs a partial rewrite that would silently
			break the solution.
		`),
		example: heredoc.Doc(`
			# Rename function 'greet' to 'salute' in the discovered solution
			scafctl refactor rename function greet salute

			# Target a specific file and preview the change without writing
			scafctl refactor rename function greet salute -f ./solution.yaml --dry-run
		`),
		kind:   "function",
		rename: pkgrefactor.RenameFunction,
	})
}
