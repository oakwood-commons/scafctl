import * as vscode from 'vscode';
import {
  LanguageClient,
  LanguageClientOptions,
  RevealOutputChannelOn,
  ServerOptions,
  TransportKind,
} from 'vscode-languageclient/node';
import { binaryNameFromCommand, checkBinary, resolveCommand } from './serverResolution';
import { buildDocumentSelector, fetchRecognizedFiles } from './documentSelectors';

let client: LanguageClient | undefined;
let output: vscode.OutputChannel | undefined;

/** Config keys that require the language client to be recreated when changed. */
const selectorConfigKeys = [
  'scafctl.serverPath',
  'scafctl.solutionFilePatterns',
  'scafctl.solutionFilePatterns.replaceDefaults',
  'scafctl.language.enable',
];

export async function activate(context: vscode.ExtensionContext): Promise<void> {
  output = vscode.window.createOutputChannel('scafctl');
  context.subscriptions.push(output);

  context.subscriptions.push(
    vscode.commands.registerCommand('scafctl.restartServer', async () => {
      await stopClient();
      await startClient();
    }),
  );

  // Recreate the client when a selector-affecting setting changes so new file
  // patterns / language mode / server path take effect without a reload.
  context.subscriptions.push(
    vscode.workspace.onDidChangeConfiguration(async (event) => {
      if (selectorConfigKeys.some((key) => event.affectsConfiguration(key))) {
        await stopClient();
        await startClient();
      }
    }),
  );

  await startClient();
}

export async function deactivate(): Promise<void> {
  await stopClient();
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

  const serverOptions: ServerOptions = {
    command,
    args: ['lsp'],
    transport: TransportKind.stdio,
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
