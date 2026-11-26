// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package prepare

import (
	"context"
	"errors"
	"io/fs"
	"strings"

	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/catalog/catalogindex"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/httpc"
	"github.com/oakwood-commons/scafctl/pkg/plugin"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/bundler"
	"github.com/oakwood-commons/scafctl/pkg/solution/load"
	"github.com/oakwood-commons/scafctl/pkg/solution/source"
	"github.com/oakwood-commons/scafctl/pkg/state"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
)

// ErrorExitCode maps a solution selection, load, or preparation failure to
// its canonical CLI exit code using ordered classification:
//
//  1. nil -> Success (0)
//  2. Host configuration and policy errors -> ConfigError (7)
//  3. Invalid CLI input or solution declaration errors -> InvalidInput (3)
//  4. Catalog or plugin acquisition failures -> CatalogError (8)
//  5. Filesystem permission errors -> PermissionDenied (10)
//  6. Everything else (missing or unreadable local/catalog solution) -> FileNotFound (4)
func ErrorExitCode(err error) int {
	if err == nil {
		return exitcode.Success
	}

	if errors.Is(err, ErrSignaturePolicy) ||
		errors.Is(err, ErrUnconfiguredCatalog) ||
		errors.Is(err, plugin.ErrUnconfiguredRegistry) ||
		errors.Is(err, catalog.ErrCatalogNotConfigured) ||
		errors.Is(err, catalog.ErrCatalogPolicyDenied) ||
		errors.Is(err, catalogindex.ErrUnknownCatalog) ||
		errors.Is(err, plugin.ErrPluginNotAllowed) ||
		errors.Is(err, plugin.ErrExternalDisabled) ||
		errors.Is(err, plugin.ErrLocalBuildNotAllowed) ||
		errors.Is(err, httpc.ErrBlockedByPolicy) ||
		isPrepareConfigError(err) {
		return exitcode.ConfigError
	}

	if errors.Is(err, source.ErrInvalidRef) ||
		errors.Is(err, state.ErrLegacyStateConfig) ||
		errors.Is(err, load.ErrAmbiguousSolution) ||
		errors.Is(err, ErrUndeclaredProvider) ||
		errors.Is(err, ErrMissingLockFile) ||
		errors.Is(err, ErrInvalidLock) ||
		errors.Is(err, bundler.ErrInvalidLockFile) ||
		errors.Is(err, ErrInvalidBundle) ||
		errors.Is(err, solution.ErrInvalidPluginSource) ||
		errors.Is(err, catalog.ErrInvalidQuery) ||
		errors.Is(err, catalog.ErrInvalidReference) ||
		errors.Is(err, plugin.ErrCatalogConflict) ||
		errors.Is(err, plugin.ErrLockStale) ||
		isPrepareLockValidationError(err) {
		return exitcode.InvalidInput
	}

	var catBuildErr *catalog.CatalogBuildError
	if errors.Is(err, catalog.ErrCatalogUnreachable) ||
		errors.Is(err, catalog.ErrCredentialsRejected) ||
		errors.Is(err, load.ErrFetch) ||
		errors.As(err, &catBuildErr) {
		return exitcode.CatalogError
	}
	if stage, ok := plugin.ErrorStage(err); ok && isCatalogAcquisitionStage(stage) {
		return exitcode.CatalogError
	}

	if errors.Is(err, fs.ErrPermission) {
		return exitcode.PermissionDenied
	}

	return exitcode.FileNotFound
}

func isPrepareConfigError(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "lock entry resolved to registry ") &&
		strings.Contains(msg, ", which is not a configured catalog")
}

func isPrepareLockValidationError(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "parsing embedded lock layer:") ||
		strings.Contains(msg, "embedded lock layer has unsupported version ") ||
		strings.Contains(msg, "; the solution lock is out of sync with bundle.plugins") ||
		strings.Contains(msg, ": lock entry has no resolved version; cannot pin in strict mode") ||
		strings.Contains(msg, ": no version constraint in bundle.plugins or lock entry; cannot resolve in constrained mode") ||
		strings.Contains(msg, ": unknown lock mode ")
}

func isCatalogAcquisitionStage(stage plugin.Stage) bool {
	switch stage {
	case plugin.StageResolve,
		plugin.StageDownload,
		plugin.StageDigest,
		plugin.StageSignature,
		plugin.StageCache,
		plugin.StageSpawn,
		plugin.StageHandshake:
		return true
	case plugin.StageBind, plugin.StageLock:
		return false
	}
	return false
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
