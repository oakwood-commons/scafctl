import * as vscode from 'vscode';
import {
  ExecuteCommandRequest,
  LanguageClient,
  LanguageClientOptions,
  RevealOutputChannelOn,
  ServerOptions,
} from 'vscode-languageclient/node';
import { binaryNameFromCommand, checkBinary, resolveCommand } from './serverResolution';
import { buildDocumentSelector, fetchRecognizedFiles } from './documentSelectors';
import { buildLensArgs, shouldSaveBeforeRun } from './codeLensCommands';

let client: LanguageClient | undefined;
let output: vscode.OutputChannel | undefined;
let lensTerminal: vscode.Terminal | undefined;

/** Resolver / call names must be a leading letter/underscore then word chars. */
const namePattern = /^[a-zA-Z_][a-zA-Z0-9_-]*$/;

/** Providers offered when adding a resolver via the "Add resolver..." action. */
const resolverProviders = [
  'static',
  'parameter',
  'http',
  'cel',
  'go-template',
  'message',
  'exec',
  'shell',
];

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
 * lensTerminalFor returns the "scafctl" code-lens terminal running the given
 * binary directly with argv (no shell). Because a terminal's shellPath/shellArgs
 * are fixed at creation, each run gets a fresh terminal; the previous one is
 * disposed so they do not accumulate. Passing argv as shellArgs means the
 * arguments reach the process verbatim -- there is no shell to interpret
 * metacharacters, so a resolver/action name or file path cannot inject commands.
 */
function lensTerminalFor(binary: string, argv: string[]): vscode.Terminal {
  lensTerminal?.dispose();
  lensTerminal = vscode.window.createTerminal({
    name: 'scafctl',
    shellPath: binary,
    shellArgs: argv,
  });
  return lensTerminal;
}

/**
 * runLensCommand handles the `scafctl.run` / `scafctl.preview` code-lens
 * commands. The server supplies the document URI and the CLI arguments (e.g.
 * `run resolver env`, or with `--dry-run` for an action preview); the extension
 * resolves the configured binary and runs it against the document's file in a
 * terminal, executing the binary directly with an argv array (no shell). The CLI
 * reads the file from disk, so a dirty on-disk document is saved first
 * (untitled/virtual documents are skipped). Malformed arguments are ignored
 * rather than throwing.
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
  const argv = buildLensArgs(args, uri.fsPath);
  lensTerminalFor(binary, argv).show();
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

  // Prompt commands referenced by the server's generative code actions. Each
  // collects user input then invokes the matching server executeCommand, which
  // computes and applies the workspace edit.
  context.subscriptions.push(
    vscode.commands.registerCommand('scafctl.extractToCall', (uri?: string, blockPath?: string) =>
      extractToCall(uri, blockPath),
    ),
    vscode.commands.registerCommand('scafctl.addResolver', (uri?: string) => addResolver(uri)),
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

/**
 * runningClient returns the language client only when it is started and ready to
 * accept requests. It warns and returns undefined otherwise, so command handlers
 * fail gracefully rather than throwing when the server is not running.
 */
function runningClient(): LanguageClient | undefined {
  if (!client) {
    void vscode.window.showWarningMessage('scafctl language server is not running.');
    return undefined;
  }
  return client;
}

/**
 * sendServerCommand invokes a server-side workspace/executeCommand, surfacing any
 * failure via an error message. Returns true on success.
 */
async function sendServerCommand(
  active: LanguageClient,
  command: string,
  args: unknown[],
): Promise<boolean> {
  try {
    await active.sendRequest(ExecuteCommandRequest.type, { command, arguments: args });
    return true;
  } catch (err) {
    const message = err instanceof Error ? err.message : String(err);
    await vscode.window.showErrorMessage(`scafctl: ${command} failed: ${message}`);
    return false;
  }
}

/**
 * extractToCall prompts for a new call name then asks the server to extract the
 * step at blockPath into a reusable spec.calls definition. Arguments arrive from
 * the server's RefactorExtract code action.
 */
async function extractToCall(uri?: string, blockPath?: string): Promise<void> {
  if (!uri || !blockPath) {
    return;
  }
  const active = runningClient();
  if (!active) {
    return;
  }
  const name = await vscode.window.showInputBox({
    prompt: 'New call name',
    validateInput: (value) =>
      namePattern.test(value) ? undefined : 'Name must match ^[a-zA-Z_][a-zA-Z0-9_-]*$',
  });
  if (!name) {
    return;
  }
  await sendServerCommand(active, 'scafctl.applyExtractCall', [uri, blockPath, name]);
}

/**
 * addResolver prompts for a resolver name and provider, then asks the server to
 * insert a stub resolver. Arguments arrive from the server's Source code action.
 */
async function addResolver(uri?: string): Promise<void> {
  if (!uri) {
    return;
  }
  const active = runningClient();
  if (!active) {
    return;
  }
  const name = await vscode.window.showInputBox({
    prompt: 'New resolver name',
    validateInput: (value) =>
      namePattern.test(value) ? undefined : 'Name must match ^[a-zA-Z_][a-zA-Z0-9_-]*$',
  });
  if (!name) {
    return;
  }
  const provider = await vscode.window.showQuickPick(resolverProviders, {
    placeHolder: 'Select a provider for the resolver',
  });
  if (!provider) {
    return;
  }
  await sendServerCommand(active, 'scafctl.applyAddResolver', [uri, name, provider]);
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
