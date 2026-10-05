// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"fmt"
	"strings"

	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// minArgsForKindTypo is the smallest argument count that can be a mistyped
// `<kind> <name>` pair. A single argument is a reference and is handled by the
// root command's own validation.
const minArgsForKindTypo = 2

// checkUnknownKind detects a mistyped kind subcommand on a root catalog command
// (pull, push, delete). Cobra only dispatches registered subcommands, so an
// unknown kind arrives as a positional argument; with a name after it, the
// root command's argument-count error would not mention the valid kinds.
//
// It returns nil unless there are at least two arguments and the first is not a
// registered kind or alias.
func checkUnknownKind(verb, binary string, args []string) error {
	if len(args) < minArgsForKindTypo || isCatalogKind(args[0]) {
		return nil
	}

	err := fmt.Errorf("%w %q; valid kinds: %s; use '%s catalog %s <kind> <name>'",
		ErrUnknownKind, args[0], strings.Join(catalogKindNames(), ", "), binary, verb)
	return exitcode.WithCode(err, exitcode.InvalidInput)
}

// reportUnknownKind runs checkUnknownKind and, on failure, prints the error
// through the command's writer (see reportCmdError).
func reportUnknownKind(cmd *cobra.Command, verb, binary string, args []string) error {
	err := checkUnknownKind(verb, binary, args)
	if err == nil {
		return nil
	}
	reportCmdError(cmd, err)
	return err
}

// reportCmdError prints err through the command's writer. The process entry
// point does not print errors that carry an exit code, so the command must.
func reportCmdError(cmd *cobra.Command, err error) {
	if ctx := cmd.Context(); ctx != nil {
		if w := writer.FromContext(ctx); w != nil {
			w.Errorf("%v", err)
		}
	}
}

// rootArgs wraps a root command's positional-argument validator so a mistyped
// kind is not rejected by the generic argument-count error. Arguments that look
// like a mistyped kind skip the wrapped validator; the command's RunE then
// reports them via reportUnknownKind. Reporting cannot happen here because the
// writer is attached to the context in PersistentPreRun, which runs after Args.
func rootArgs(_, _ string, next cobra.PositionalArgs) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) >= minArgsForKindTypo && !isCatalogKind(args[0]) {
			return nil
		}
		return next(cmd, args)
	}
}
