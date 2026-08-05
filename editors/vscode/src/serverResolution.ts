import { execFile } from 'node:child_process';
import { basename } from 'node:path';

/** DefaultServerCommand is the command used when no explicit path is configured. */
export const DefaultServerCommand = 'scafctl';

/**
 * resolveCommand picks the server command from a configured path (when non-empty)
 * or falls back to the default, which is resolved from PATH.
 */
export function resolveCommand(configuredPath: string | undefined): string {
  const trimmed = (configuredPath ?? '').trim();
  return trimmed.length > 0 ? trimmed : DefaultServerCommand;
}

/**
 * binaryNameFromCommand derives a CLI binary name from a resolved command path
 * (e.g. `/opt/mycli` -> `mycli`, `mycli.exe` -> `mycli`, `scafctl` -> `scafctl`).
 * It mirrors Go's settings.SanitizeBinaryName: strip the directory, strip the
 * final extension (so `mycli.cmd`/`.bat`/`.ps1` don't leak into selectors),
 * replace unsafe characters, and fall back to the default when empty. Keeping it
 * aligned with the Go sanitizer ensures the fallback binary name matches the
 * CLI's discovery names when the server cannot report its own recognized files.
 */
export function binaryNameFromCommand(command: string): string {
  let name = basename(command.trim());
  // Strip a single trailing extension, matching Go's filepath.Ext stripping.
  const dot = name.lastIndexOf('.');
  if (dot > 0) {
    name = name.slice(0, dot);
  }
  // Replace unsafe characters with underscores and trim them (mirrors safeNameRe
  // [^A-Za-z0-9._-] plus a leading/trailing underscore trim).
  name = name.replace(/[^A-Za-z0-9._-]/g, '_').replace(/^_+|_+$/g, '');
  // Fall back to the default for empty or dot-only names, matching Go's
  // SanitizeBinaryName (which returns the default for "", ".", and "..").
  if (name.length === 0 || name === '.' || name === '..') {
    return DefaultServerCommand;
  }
  return name;
}

/**
 * checkBinary runs `<command> lsp --help` to confirm the executable exists and
 * supports the language server. Resolves to null on success, or a human-readable
 * error message describing what to fix.
 */
export function checkBinary(command: string, timeoutMs = 5000): Promise<string | null> {
  return new Promise((resolve) => {
    execFile(command, ['lsp', '--help'], { timeout: timeoutMs }, (err) => {
      resolve(err ? describeError(command, err as NodeJS.ErrnoException) : null);
    });
  });
}

function describeError(command: string, err: NodeJS.ErrnoException): string {
  if (err.code === 'ENOENT') {
    return `Could not find the '${command}' executable. Install scafctl and ensure it is on your PATH, or set 'scafctl.serverPath' in Settings.`;
  }
  if (err.code === 'EACCES') {
    return `The '${command}' executable is not runnable (permission denied). Check its file permissions, or set 'scafctl.serverPath' to a valid scafctl binary.`;
  }
  return `The '${command}' executable does not support the language server ('${command} lsp' failed: ${err.message}). Update scafctl, or set 'scafctl.serverPath'.`;
}
