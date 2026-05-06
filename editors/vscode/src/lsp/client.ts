import * as vscode from "vscode";
import {
	LanguageClient,
	type LanguageClientOptions,
	type ServerOptions,
	TransportKind,
} from "vscode-languageclient/node";
import { buildGafferArgv } from "../discovery/cli.js";
import { log } from "../output.js";

let client: LanguageClient | undefined;

/**
 * Start the gaffer LSP client. Spawns `gaffer lsp` over stdio
 * via the configured `gaffer.command` argv.
 *
 * The document selector covers gaffer.toml (where the server
 * does the actual parsing) and JavaScript files (where the
 * server emits entry-script lenses by cross-referencing every
 * cached toml's projection entry paths). The watcher pattern
 * is registered server-side via dynamic capability
 * registration; the client doesn't synchronize anything from
 * VS Code's `FileSystemWatcher` directly.
 *
 * Resolves once the client is ready (initialize handshake
 * complete). Failures during spawn or initialize log and
 * return without throwing - the rest of the extension
 * (commands, panels, DAP) keeps working.
 */
export async function startLanguageClient(
	context: vscode.ExtensionContext,
): Promise<void> {
	const argv = buildGafferArgv(["lsp"]);
	const command = argv[0];
	if (command === undefined) {
		log("LSP client: empty gaffer.command, skipping spawn");
		return;
	}
	const args = argv.slice(1);
	const serverOptions: ServerOptions = {
		run: { command, args, transport: TransportKind.stdio },
		debug: { command, args, transport: TransportKind.stdio },
	};
	const clientOptions: LanguageClientOptions = {
		documentSelector: [
			{ scheme: "file", pattern: "**/gaffer.toml" },
			{ scheme: "file", language: "javascript" },
		],
		outputChannel: vscode.window.createOutputChannel("Gaffer LSP"),
	};
	const c = new LanguageClient(
		"gaffer-lsp",
		"Gaffer LSP",
		serverOptions,
		clientOptions,
	);
	try {
		await c.start();
		client = c;
		context.subscriptions.push({
			dispose: () => {
				void c.stop();
			},
		});
		log("LSP client started");
	} catch (err) {
		const msg = err instanceof Error ? err.message : String(err);
		log(`LSP client failed to start: ${msg}`);
	}
}

/**
 * Returns the active LSP client, or undefined if startup
 * hasn't completed (or failed).
 */
export function getLanguageClient(): LanguageClient | undefined {
	return client;
}
