// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package test

import (
	"context"
	"errors"
	"fmt"

	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution/load"
	"github.com/oakwood-commons/scafctl/pkg/solution/source"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// testsPathHint is appended to a discovery failure by commands that accept
// --tests-path.
const testsPathHint = "use --file (-f) or --tests-path"

// localSolution returns where a test command reads solutions from: testsPath
// when set, otherwise ref made local by load.Local (fetching URL, registry,
// and catalog references). sel nil uses the router from ctx. hint, when set,
// is appended to a "no solution found" error. Errors are written to w and
// carry an exit code. The caller must Close the result.
func localSolution(ctx context.Context, w *writer.Writer, sel load.Selector, testsPath, ref, hint string) (*load.LocalSolution, error) {
	if testsPath != "" {
		return &load.LocalSolution{Path: testsPath}, nil
	}
	if sel == nil {
		sel = load.NewRouterFromContext(ctx, *logger.FromContext(ctx))
	}

	local, err := load.Local(ctx, sel, ref, load.DiscoveryRiskLow)
	if err != nil {
		if hint != "" && errors.Is(err, load.ErrNoSolutionFound) {
			err = fmt.Errorf("%w; %s", err, hint)
		}
		code := exitcode.InvalidInput
		if errors.Is(err, load.ErrFetch) {
			code = exitcode.CatalogError
		}
		w.Errorf("%s", err)
		return nil, exitcode.WithCode(err, code)
	}
	return local, nil
}

// newVersionedPositionalPreRunE builds a PreRunE hook for test subcommands
// (init, list) that require -f/--file to be a local path and reject bare
// unversioned positional catalog names.
func newVersionedPositionalPreRunE(filePtr *string, cliParams *settings.Run, ioStreams *terminal.IOStreams, subcmd string) func(*cobra.Command, []string) error {
	return func(cCmd *cobra.Command, args []string) error {
		fail := func(err error) error {
			var w *writer.Writer
			if cCmd != nil && cCmd.Context() != nil {
				w = writer.FromContext(cCmd.Context())
			}
			if w == nil && ioStreams != nil {
				w = writer.New(ioStreams, cliParams)
			}
			if w != nil {
				w.Errorf("%v", err)
			}
			return exitcode.WithCode(err, exitcode.InvalidInput)
		}
		bin := settings.CliBinaryName
		if cliParams != nil && cliParams.BinaryName != "" {
			bin = cliParams.BinaryName
		}
		cmdUsage := bin + " " + subcmd
		if _, err := source.ValidateFileFlagPositional(*filePtr, cmdUsage); err != nil {
			return fail(err)
		}
		if len(args) > 0 {
			c, err := source.ValidatePositionalRef(args[0], *filePtr, cmdUsage)
			if err != nil {
				return fail(err)
			}
			if c.IsUnversionedName() {
				return fail(fmt.Errorf("bare names are not supported as positional args for %s; use -f to specify a solution file or a versioned catalog ref (name@version)", subcmd))
			}
			*filePtr = args[0]
		}
		return nil
	}
}
