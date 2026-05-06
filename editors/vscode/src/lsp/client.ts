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
 * Start the gaffer LSP client iff the workspace is currently
 * trusted, and re-attempt a start when trust is later granted.
 *
 * The trust gate matches the manifest fetch path
 * (`tryFetchManifest` in `discovery/cli.ts`) and the spawn
 * promise declared in `package.json`'s `untrustedWorkspaces`
 * capability ("debugging is disabled until the workspace is
 * trusted"). The LSP server walks workspace files and parses
 * `gaffer.toml`s; an untrusted workspace's content shouldn't be
 * fed into a process the user implicitly trusts.
 *
 * Spawns `gaffer lsp` over stdio using the User-scope
 * `gaffer.command` argv. Document selector covers `gaffer.toml`
 * and JavaScript files. The watcher pattern is registered
 * server-side via dynamic capability registration.
 *
 * onReady fires exactly once with the live client when start
 * succeeds. Callers register lens providers etc. that depend
 * on a working client.
 */
export function startLanguageClient(
	context: vscode.ExtensionContext,
	onReady?: (client: LanguageClient) => void,
): void {
	const tryStart = (): void => {
		if (!vscode.workspace.isTrusted) {
			log("LSP client: workspace untrusted, deferring spawn");
			return;
		}
		void spawnLanguageClient(context, onReady);
	};
	tryStart();
	context.subscriptions.push(
		vscode.workspace.onDidGrantWorkspaceTrust(() => {
			if (!client) tryStart();
		}),
	);
}

async function spawnLanguageClient(
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
	const channel = vscode.window.createOutputChannel("Gaffer LSP");
	context.subscriptions.push(channel);
	const clientOptions: LanguageClientOptions = {
		documentSelector: [
			{ scheme: "file", pattern: "**/gaffer.toml" },
			{ scheme: "file", language: "javascript" },
		],
		outputChannel: channel,
	};
	const c = new LanguageClient(
		"gaffer-lsp",
		"Gaffer LSP",
		serverOptions,
		clientOptions,
	);
	// Push the disposable BEFORE awaiting start so a mid-init
	// dispose cancels cleanly. The dispose returns the stop()
	// Promise so VS Code (which awaits disposables on
	// extension teardown) waits for the child process to flush.
	let disposed = false;
	context.subscriptions.push({
		dispose: () => {
			disposed = true;
			return c.stop();
		},
	});
	try {
		await c.start();
		// If the disposable fired between push and now, c.stop()
		// already ran (or is racing); don't promote the dead
		// client to the module-level `client` or fire onReady on
		// a torn-down session.
		if (disposed) return;
		client = c;
		log("LSP client started");
		onReady?.(c);
	} catch (err) {
		const msg = err instanceof Error ? err.message : String(err);
		log(`LSP client failed to start: ${msg}`);
		void vscode.window.showWarningMessage(
			`Gaffer LSP failed to start: ${msg}. Check the "Gaffer LSP" output channel for details.`,
		);
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
