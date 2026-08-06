import * as vscode from 'vscode';
import {
  LanguageClient,
  LanguageClientOptions,
  RevealOutputChannelOn,
  ServerOptions,
} from 'vscode-languageclient/node';
import { binaryNameFromCommand, checkBinary, resolveCommand } from './serverResolution';
import { buildDocumentSelector, fetchRecognizedFiles } from './documentSelectors';
import { buildLensArgv, shouldSaveBeforeRun, toShellCommand } from './codeLensCommands';

let client: LanguageClient | undefined;
let output: vscode.OutputChannel | undefined;
let lensTerminal: vscode.Terminal | undefined;

// restartQueue serializes stop/start cycles. The restart command, config-change
// events, and initial activation can otherwise interleave their stop/start
// calls, leaving multiple clients or a stopped client. Chaining every cycle
// onto a single in-flight promise makes them run strictly in order.
let restartQueue: Promise<void> = Promise.resolve();

/** Config keys that require the language client to be recreated when changed. */
const selectorConfigKeys = [
  'scafctl.serverPath',
  'scafctl.solutionFilePatterns',
  'scafctl.solutionFilePatterns.replaceDefaults',
  'scafctl.language.enable',
];

/**
 * enqueueRestart serializes client lifecycle transitions. `task` runs after any
 * prior transition settles; its errors are logged and swallowed so a background
 * restart (triggered via `void` from a config-change event) can never surface as
 * an unhandled promise rejection or break the queue for later transitions.
 */
function enqueueRestart(task: () => Promise<void>): Promise<void> {
  const run = async (): Promise<void> => {
    try {
      await task();
    } catch (err) {
      const message = err instanceof Error ? err.message : String(err);
      output?.appendLine(`scafctl: client restart failed: ${message}`);
    }
  };
  restartQueue = restartQueue.then(run);
  return restartQueue;
}

/** restartClient stops any running client and starts a fresh one. */
async function restartClient(): Promise<void> {
  await stopClient();
  await startClient();
}

/**
 * lensTerminalFor returns a reusable "scafctl" terminal, recreating it if the
 * user closed the previous one.
 */
function lensTerminalFor(): vscode.Terminal {
  if (!lensTerminal || lensTerminal.exitStatus !== undefined) {
    lensTerminal = vscode.window.createTerminal('scafctl');
  }
  return lensTerminal;
}

/**
 * runLensCommand handles the `scafctl.run` / `scafctl.preview` code-lens
 * commands. The server supplies the document URI and the CLI arguments (e.g.
 * `run resolver env`, or with `--dry-run` for an action preview); the extension
 * resolves the configured binary and spawns it against the document's file in a
 * terminal. The CLI reads the file from disk, so a dirty on-disk document is
 * saved first (untitled/virtual documents are skipped). Malformed arguments are
 * ignored rather than throwing.
 */
async function runLensCommand(uriStr: unknown, cliArgs: unknown): Promise<void> {
  if (typeof uriStr !== 'string' || !Array.isArray(cliArgs)) {
    return;
  }
  const args = cliArgs.map((a) => String(a));
  const uri = vscode.Uri.parse(uriStr);

  // Flush unsaved edits so the CLI runs the content the user sees.
  const doc = vscode.workspace.textDocuments.find((d) => d.uri.toString() === uri.toString());
  if (doc && shouldSaveBeforeRun(uri.scheme, doc.isDirty)) {
    await doc.save();
  }

  const binary = resolveCommand(
    vscode.workspace.getConfiguration().get<string>('scafctl.serverPath'),
  );
  const argv = buildLensArgv(binary, args, uri.fsPath);
  const terminal = lensTerminalFor();
  terminal.show();
  terminal.sendText(toShellCommand(argv));
}

export async function activate(context: vscode.ExtensionContext): Promise<void> {
  output = vscode.window.createOutputChannel('scafctl');
  context.subscriptions.push(output);

  context.subscriptions.push(
    vscode.commands.registerCommand('scafctl.restartServer', () => enqueueRestart(restartClient)),
  );

  // Code-lens Run / Preview commands: the server emits a lens carrying the CLI
  // arguments; the extension spawns the CLI in a terminal against the lens's
  // document. Both ids share one handler (Preview differs only in the args the
  // server provided, e.g. --dry-run for actions).
  const lensHandler = (uriStr: unknown, cliArgs: unknown): Promise<void> =>
    runLensCommand(uriStr, cliArgs);
  context.subscriptions.push(
    vscode.commands.registerCommand('scafctl.run', lensHandler),
    vscode.commands.registerCommand('scafctl.preview', lensHandler),
    // Dispose the shared lens terminal on shutdown (it is created lazily).
    { dispose: () => lensTerminal?.dispose() },
  );

  // Recreate the client when a selector-affecting setting changes so new file
  // patterns / language mode / server path take effect without a reload.
  context.subscriptions.push(
    vscode.workspace.onDidChangeConfiguration((event) => {
      if (selectorConfigKeys.some((key) => event.affectsConfiguration(key))) {
        void enqueueRestart(restartClient);
      }
    }),
  );

  await enqueueRestart(startClient);
}

export async function deactivate(): Promise<void> {
  await enqueueRestart(stopClient);
}

async function startClient(): Promise<void> {
  const config = vscode.workspace.getConfiguration('scafctl');
  const command = resolveCommand(config.get<string>('serverPath'));

  const problem = await checkBinary(command);
  if (problem) {
    const choice = await vscode.window.showErrorMessage(problem, 'Open Settings');
    if (choice === 'Open Settings') {
      await vscode.commands.executeCommand('workbench.action.openSettings', 'scafctl.serverPath');
    }
    return;
  }

  // Communicate over the child process's stdio. Do NOT set
  // `transport: TransportKind.stdio` -- for an Executable, vscode-languageclient
  // appends a `--stdio` flag when that transport is set explicitly, which the
  // `scafctl lsp` cobra command rejects as an unknown flag (exit 1). Omitting
  // `transport` still uses stdio (the Executable default) without the flag.
  const serverOptions: ServerOptions = {
    command,
    args: ['lsp'],
  };

  // Query the binary for the exact set of solution/action file names it
  // auto-discovers (including .json solutions, taskfile.*, actions.*, and any
  // embedder binary name), so editor targeting stays in lockstep with CLI
  // discovery instead of a hardcoded, drift-prone glob. If the binary is too
  // old to answer, fall back to defaults derived from its own file name so an
  // embedder's <binary>.yaml still matches.
  const recognized = await fetchRecognizedFiles(command, {
    fallbackBinaryName: binaryNameFromCommand(command),
    onError: (message) => output?.appendLine(message),
  });

  const userPatterns = config.get<string[]>('solutionFilePatterns') ?? [];
  const replaceDefaults = config.get<boolean>('solutionFilePatterns.replaceDefaults') ?? false;
  const languageEnabled = config.get<boolean>('language.enable') ?? false;

  const documentSelector = buildDocumentSelector(recognized, userPatterns, {
    replaceDefaults,
    languageEnabled,
  });

  const clientOptions: LanguageClientOptions = {
    documentSelector,
    // Route server logs and JSON-RPC traces to our output channel. The client's
    // id ('scafctl', below) makes vscode-languageclient read the trace level
    // from the `scafctl.trace.server` setting automatically. Surface the channel
    // on error so a crashing/failing server is not silent.
    outputChannel: output,
    revealOutputChannelOn: RevealOutputChannelOn.Error,
  };

  client = new LanguageClient('scafctl', 'scafctl', serverOptions, clientOptions);

  // client.start() rejects if the server fails to launch or the LSP handshake
  // fails (a distinct case from a running server erroring later, which
  // revealOutputChannelOn handles). Surface it explicitly so startup failures
  // are not a silent unhandled rejection.
  try {
    await client.start();
  } catch (err) {
    // Attempt to stop the partially-started client so a spawned-but-unhandshaked
    // server process is not leaked. stop() may itself reject for a client that
    // never fully started, so guard it before clearing the reference.
    try {
      await client.stop();
    } catch {
      // Best-effort cleanup; the start error below is what matters.
    }
    client = undefined;
    const message = err instanceof Error ? err.message : String(err);
    output?.appendLine(`scafctl: language server failed to start: ${message}`);
    const choice = await vscode.window.showErrorMessage(
      `scafctl language server failed to start: ${message}`,
      'Show Logs',
    );
    if (choice === 'Show Logs') {
      output?.show(true);
    }
  }
}

async function stopClient(): Promise<void> {
  if (client) {
    await client.stop();
    client = undefined;
  }
}
