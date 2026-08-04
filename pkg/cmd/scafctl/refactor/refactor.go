// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package refactor provides the `refactor` command group: source-preserving
// refactorings for solution files (rename and friends). All business logic
// lives in pkg/refactor; these commands are thin wiring.
package refactor

import (
	"fmt"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/scafctl/pkg/cmd/cmdutil"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/spf13/cobra"
)

// CommandRefactor creates the `refactor` parent command.
func CommandRefactor(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
	cCmd := cmdutil.MakeHelpOnlyGroup(&cobra.Command{
		Use:   "refactor",
		Short: "Source-preserving edits to solutions (rename, ...)",
		Long: heredoc.Doc(`
			Apply structural edits to a solution while preserving comments,
			key order, and formatting.

			Edits replace only the exact bytes of each affected token, so the
			rest of the file is left byte-for-byte unchanged. Refactorings refuse
			to run when they cannot locate every affected reference, rather than
			performing a partial (and potentially breaking) rewrite.

			  refactor rename resolver <old> <new>   Rename a resolver everywhere
		`),
		SilenceUsage: true,
	})

	cCmd.AddCommand(CommandRename(cliParams, ioStreams, fmt.Sprintf("%s/%s", path, cCmd.Use)))
	return cCmd
}

// CommandRename creates the `refactor rename` parent command.
func CommandRename(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
	cCmd := cmdutil.MakeHelpOnlyGroup(&cobra.Command{
		Use:   "rename",
		Short: "Rename a symbol and update every reference to it",
		Long: heredoc.Doc(`
			Rename a symbol and rewrite every reference to it in place.

			  refactor rename resolver <old> <new>   Rename a resolver
		`),
		SilenceUsage: true,
	})

	cCmd.AddCommand(CommandRenameResolver(cliParams, ioStreams, fmt.Sprintf("%s/%s", path, cCmd.Use)))
	return cCmd
}
