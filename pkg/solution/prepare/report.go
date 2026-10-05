// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package prepare

import (
	"context"
	"errors"

	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution/source"
	"github.com/oakwood-commons/scafctl/pkg/state"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
)

// ErrorExitCode maps a solution selection, load, or preparation failure to
// its exit code. A malformed reference (or a --catalog/--version that does
// not apply to it) and a solution that still uses removed state configuration
// keys are invalid input; every other failure is FileNotFound.
func ErrorExitCode(err error) int {
	if errors.Is(err, source.ErrInvalidRef) || errors.Is(err, state.ErrLegacyStateConfig) {
		return exitcode.InvalidInput
	}
	return exitcode.FileNotFound
}

// reportLoaded writes the verbose summary of a prepared solution to the writer
// in ctx: its name, version, directory and path, plus a hint when discovery
// found an alternate solution or actions file. No-op when ctx has no writer
// or verbose output is off.
func reportLoaded(ctx context.Context, result *Result) {
	w := writer.FromContext(ctx)
	if w == nil || !w.VerboseEnabled() {
		return
	}
	sol := result.Solution
	name := sol.Metadata.Name
	var ver string
	if sol.Metadata.Version != nil {
		ver = sol.Metadata.Version.String()
	}
	path := sol.GetPath()

	w.Verbosef("Solution loaded: %s (version=%s, dir=%s)", name, ver, result.SolutionDir)

	switch {
	case name != "" && ver != "" && path != "":
		w.Verbosef("Solution: %s@%s (%s)", name, ver, path)
	case name != "" && ver != "":
		w.Verbosef("Solution: %s@%s", name, ver)
	case name != "" && path != "":
		w.Verbosef("Solution: %s (%s)", name, path)
	case name != "":
		w.Verbosef("Solution: %s", name)
	case path != "":
		w.Verbosef("Solution: %s", path)
	}

	disc := result.DiscoveredFrom
	if disc.AlternatePath != "" {
		binaryName := settings.BinaryNameFromContext(ctx)
		if disc.IsActionFile {
			w.Verbosef("  (solution file also found at %s; use '%s run solution' to run it)", disc.AlternatePath, binaryName)
		} else {
			w.Verbosef("  (actions.yaml also found at %s; use '%s run action' to execute actions)", disc.AlternatePath, binaryName)
		}
	}
}
