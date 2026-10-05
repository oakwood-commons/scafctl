// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package test

import (
	"context"
	"errors"
	"fmt"

	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/solution/load"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
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
