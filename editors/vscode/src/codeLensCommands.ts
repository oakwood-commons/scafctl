// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Pure helpers for the code-lens Run / Preview commands. Kept free of any
// `vscode` import so they are unit-testable under `node --test`; the extension
// wires these into terminal invocations.

/**
 * buildLensArgv assembles the argv for a lens-invoked CLI run: the resolved
 * scafctl binary, the server-provided CLI arguments (e.g. `run resolver env`),
 * and the `-f <file>` flag pointing at the document the lens came from.
 */
export function buildLensArgv(binary: string, cliArgs: string[], fsPath: string): string[] {
  return [binary, ...cliArgs, '-f', fsPath];
}

/**
 * quoteArg makes an argument safe to embed in a shell command line sent to a
 * terminal. An argument consisting only of safe path/identifier characters is
 * passed through; anything else (whitespace or a shell metacharacter such as
 * `$`, `&`, `;`, `(`, backtick) is double-quoted, and the characters that still
 * expand inside POSIX double quotes (`"`, `$`, backtick) are backslash-escaped.
 *
 * Inputs here are the user's OWN workspace file path and their OWN resolver/action
 * name, run in their OWN terminal -- so this is defense against an awkward path or
 * name mangling the command, not a trust boundary. Perfect cross-shell quoting is
 * not achievable through `sendText` (PowerShell/cmd escape differently); this
 * hardens the common POSIX case and leaves the command legible.
 */
export function quoteArg(arg: string): string {
  if (arg.length === 0) {
    return '""';
  }
  if (/^[A-Za-z0-9._\-/:\\]+$/.test(arg)) {
    return arg;
  }
  return `"${arg.replace(/(["$`])/g, '\\$1')}"`;
}

/** toShellCommand renders an argv as a single shell command line. */
export function toShellCommand(argv: string[]): string {
  return argv.map(quoteArg).join(' ');
}
