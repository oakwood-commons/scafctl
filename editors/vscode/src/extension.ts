import * as vscode from 'vscode';
import {
  LanguageClient,
  LanguageClientOptions,
  ServerOptions,
  TransportKind,
} from 'vscode-languageclient/node';
import { checkBinary, resolveCommand } from './serverResolution';

let client: LanguageClient | undefined;

export async function activate(context: vscode.ExtensionContext): Promise<void> {
  context.subscriptions.push(
    vscode.commands.registerCommand('scafctl.restartServer', async () => {
      await stopClient();
      await startClient();
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

  // Scope the language server to solution files (scafctl auto-discovers these
  // names), leaving other YAML documents untouched.
  const clientOptions: LanguageClientOptions = {
    documentSelector: [
      { language: 'yaml', pattern: '**/{solution,scafctl}.{yaml,yml}' },
    ],
  };

  client = new LanguageClient('scafctl', 'scafctl', serverOptions, clientOptions);
  await client.start();
}

async function stopClient(): Promise<void> {
  if (client) {
    await client.stop();
    client = undefined;
  }
}
