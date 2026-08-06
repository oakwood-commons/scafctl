// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Pure helpers for the code-lens Run / Preview commands. Kept free of any
// `vscode` import so they are unit-testable under `node --test`; the extension
// runs the resulting argv WITHOUT a shell (a terminal created with
// shellPath/shellArgs), so there is no shell quoting or injection surface.

/**
 * buildLensArgs assembles the argument vector passed to the scafctl binary for a
 * lens-invoked run: the server-provided CLI arguments (e.g. `run resolver env`,
 * or with `--dry-run` for an action preview) followed by `-f <file>` pointing at
 * the document the lens came from. The binary itself is not included -- it is the
 * process being executed, not an argument.
 */
export function buildLensArgs(cliArgs: string[], fsPath: string): string[] {
  return [...cliArgs, '-f', fsPath];
}

/**
 * shouldSaveBeforeRun reports whether an open document should be saved before a
 * code-lens Run/Preview spawns the CLI. The CLI reads the solution from disk, so
 * a dirty on-disk (`file://`) document must be flushed first; untitled or virtual
 * documents (non-`file` schemes) have no path to run against and are never saved.
 */
export function shouldSaveBeforeRun(scheme: string, isDirty: boolean): boolean {
  return isDirty && scheme === 'file';
}
