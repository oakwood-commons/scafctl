// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

// Shared parameter/enum description constants for MCP tool schemas.
//
// These exist purely to avoid repeating near-identical description text
// across dozens of mcp.NewTool registrations in tools_*.go, which bloats
// the ListTools response sent to every client on init. Keep each constant
// scoped to one distinct semantic meaning -- do not collapse genuinely
// different cwd semantics (e.g. solution-relative resolution vs. plain
// file paths) into a single generic string.

const (
	// cwdDescDefault is the generic cwd description for tools that only
	// resolve plain relative paths against a working directory, with no
	// solution-relative ("relativeTo: solution") special-casing.
	cwdDescDefault = "Working directory for path resolution. When set, relative paths resolve against this directory instead of the process CWD."

	// cwdDescSolutionAware is used by tools that accept a solution path and
	// honor `relativeTo: solution` resolvers/actions, where solution-relative
	// reads resolve against the solution's own directory (when it has one)
	// regardless of the cwd override.
	cwdDescSolutionAware = "Working directory for path resolution. When set, relative paths (including the solution path itself) resolve against this directory instead of the process CWD. Solution-relative reads (relativeTo: solution) resolve against the solution's own directory regardless of this setting when the solution has a local directory (a local file path or an extracted catalog bundle); for stdin (-) and unbundled catalog references there is no solution directory, so these reads fall back to the process CWD."

	// cwdDescSolutionAwareNoWhenSet is identical in meaning to
	// cwdDescSolutionAware but phrased without a leading "When set," clause
	// (used by run_solution, whose cwd parameter has different surrounding
	// prose).
	cwdDescSolutionAwareNoWhenSet = "Working directory for path resolution. Relative paths resolve against this directory instead of the process CWD. Solution-relative reads (relativeTo: solution) resolve against the solution's own directory regardless of this setting when the solution has a local directory (a local file path or an extracted catalog bundle); for stdin (-) and unbundled catalog references there is no solution directory, so these reads fall back to the process CWD."

	// cwdDescSolutionPathOnly covers tools that resolve the solution path
	// itself against cwd but do not implement relativeTo:solution semantics.
	cwdDescSolutionPathOnly = "Working directory for path resolution. When set, relative paths (including the solution path itself) resolve against this directory instead of the process CWD."

	// cwdDescFilePaths is used by tools that resolve plain file paths (not a
	// solution path) against cwd.
	cwdDescFilePaths = "Working directory for path resolution. When set, relative file paths resolve against this directory instead of the process CWD."

	// cwdDescPlatformBinaryPaths is used by multi-platform plugin packaging
	// tools that resolve per-platform binary paths against cwd.
	cwdDescPlatformBinaryPaths = "Working directory for path resolution. When set, relative platform binary paths resolve against this directory instead of the process CWD."
)

// onConflictEnumValues are the shared on_conflict enum values.
var onConflictEnumValues = []string{"error", "overwrite", "skip", "skip-unchanged", "append"}

// providerCapabilityEnumValues are the shared provider capability enum values.
var providerCapabilityEnumValues = []string{"from", "transform", "validation", "authentication", "action"}
