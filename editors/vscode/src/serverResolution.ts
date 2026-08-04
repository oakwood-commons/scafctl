import { execFile } from 'node:child_process';

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
  return `The '${command}' executable does not support the language server ('${command} lsp' failed: ${err.message}). Update scafctl, or set 'scafctl.serverPath'.`;
}
