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
 * via the User-scope `gaffer.command` argv.
 *
 * The document selector covers gaffer.toml (where the server
 * does the actual parsing) and JavaScript files (where the
 * server emits entry-script lenses by cross-referencing every
 * cached toml's projection entry paths). The watcher pattern
 * is registered server-side via dynamic capability
 * registration.
 *
 * Resolves once the client is ready (initialize handshake
 * complete) or has failed to start. Failures log to the
 * "Gaffer LSP" output channel; the rest of the extension
 * (commands, panels, DAP) keeps working.
 *
 * onReady fires exactly once with the live client when start
 * succeeds. Callers register lens providers etc. that depend
 * on a working client.
 */
export async function startLanguageClient(
	context: vscode.ExtensionContext,
	onReady?: (client: LanguageClient) => void,
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
	// Push the disposable BEFORE awaiting start - if activation
	// disposes mid-start, the partially-initialised client still
	// gets stop()ped (no-op on a not-yet-started client).
	context.subscriptions.push({
		dispose: () => {
			void c.stop();
		},
	});
	try {
		await c.start();
		client = c;
		log("LSP client started");
		onReady?.(c);
	} catch (err) {
		const msg = err instanceof Error ? err.message : String(err);
		log(`LSP client failed to start: ${msg}`);
	}
}

/**
 * Returns the active LSP client, or undefined if startup
 * hasn't completed (or failed). Callers that send requests
 * (workspace/symbol, etc.) should handle the undefined case
 * with a sensible fallback - usually an empty result.
 */
export function getLanguageClient(): LanguageClient | undefined {
	return client;
}

/**
 * Stop the active language client. Used by deactivate() to
 * give the server a chance to flush before the host exits.
 */
export async function stopLanguageClient(): Promise<void> {
	if (!client) return;
	try {
		await client.stop();
	} catch (err) {
		const msg = err instanceof Error ? err.message : String(err);
		log(`LSP client stop failed: ${msg}`);
	}
	client = undefined;
}
