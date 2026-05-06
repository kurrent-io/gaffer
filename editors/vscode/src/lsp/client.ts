import * as vscode from "vscode";
import {
	CloseAction,
	type CloseHandlerResult,
	ErrorAction,
	type ErrorHandler,
	type ErrorHandlerResult,
	LanguageClient,
	type LanguageClientOptions,
	type Message,
	RevealOutputChannelOn,
	type ServerOptions,
	TransportKind,
} from "vscode-languageclient/node";
import { buildGafferArgv } from "../discovery/cli.js";
import { log } from "../output.js";
import { showLspCrashed, showLspFailedToStart } from "../notifications.js";

let client: LanguageClient | undefined;

// Latched by startLanguageClient and called from retryStartLanguageClient
// after a successful manifest reload. Re-running startLanguageClient
// instead would double-register the trust-grant listener.
let trySpawn: () => void = () => {};

/**
 * Start the gaffer LSP client iff the workspace is currently
 * trusted AND the manifest fetched successfully, and re-attempt
 * a start when either gate is later cleared.
 *
 * The trust gate matches the manifest fetch path
 * (`tryFetchManifest` in `discovery/cli.ts`) and the spawn
 * promise declared in `package.json`'s `untrustedWorkspaces`
 * capability ("debugging is disabled until the workspace is
 * trusted"). The LSP server walks workspace files and parses
 * `gaffer.toml`s; an untrusted workspace's content shouldn't be
 * fed into a process the user implicitly trusts.
 *
 * The manifest gate is a UX kindness: a busted gaffer.command
 * surfaces via showManifestFailure already; spawning the LSP
 * with the same broken argv would just stack another crash
 * toast for the same root cause.
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
	isManifestAvailable: () => boolean,
	onReady?: (client: LanguageClient) => void,
): void {
	trySpawn = (): void => {
		if (client) return;
		if (!vscode.workspace.isTrusted) {
			log("LSP client: workspace untrusted, deferring spawn");
			return;
		}
		if (!isManifestAvailable()) {
			log("LSP client: manifest unavailable, deferring spawn");
			return;
		}
		void spawnLanguageClient(context, onReady);
	};
	trySpawn();
	context.subscriptions.push(
		vscode.workspace.onDidGrantWorkspaceTrust(() => {
			trySpawn();
		}),
	);
}

/**
 * Re-evaluate the spawn gates and start the LSP if newly
 * eligible. Idempotent: a no-op if the client is already
 * running, or if any gate (trust, manifest) still fails.
 * Called from the manifest reload chain so a fixed
 * `gaffer.command` recovers without an extension restart.
 */
export function retryStartLanguageClient(): void {
	trySpawn();
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
		// Suppress vscode-languageclient's built-in auto-toasts. By
		// default it fires `showErrorMessage("Client ...is erroring",
		// "Go to output")` on every transport error written to its
		// channel, stacking duplicates while the errorHandler is
		// busy retrying. Our errorHandler.closed surfaces a single
		// summary toast on permanent failure - that's the only
		// surface the user needs.
		revealOutputChannelOn: RevealOutputChannelOn.Never,
		// Suppress vscode-languageclient's bare-message toast on
		// initialize failure (lib/common/client.js:914 fires
		// `showErrorMessage(error.message)` with no buttons and no
		// source label - e.g. just "write EPIPE"). Returning false
		// short-circuits that path; our outer-catch toast
		// (showLspFailedToStart) is the single user-facing surface
		// for this branch.
		initializationFailedHandler: () => false,
		// Surface server crashes to the user. The default
		// vscode-languageclient handler logs to the LSP output
		// channel and silently auto-restarts up to 5 times in 3
		// minutes before giving up - the user just sees a feature
		// silently stop working. Wrap that policy with a toast on
		// permanent failure so the broken state is observable.
		errorHandler: makeErrorHandler(channel),
		middleware: {
			// The server advertises codeLensProvider so other editors
			// (zed, neovim, etc.) get lenses via the standard path.
			// vscode-languageclient honours that by registering its
			// own built-in CodeLensProvider that emits the raw server
			// lenses verbatim - which doubles up with our
			// LspCodeLensProvider's decorated output (codicons, trust
			// gating, debug-state swap). Short-circuit the built-in
			// here so only our decorated lenses surface in VS Code.
			provideCodeLenses: () => [],
			resolveCodeLens: (lens) => lens,
		},
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
		void showLspFailedToStart(msg, channel);
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

// vscode-languageclient's default policy: tolerate the first
// `maxRestartCount` (5) restarts in a 3-minute window, then give
// up. Mirror that, but on the give-up branch surface a toast so
// the user knows features are broken instead of silently
// degrading. Keeps the same restart budget - just makes the
// final outcome observable.
const MAX_RESTART_COUNT = 4;
const RESTART_WINDOW_MS = 3 * 60 * 1000;

function makeErrorHandler(channel: vscode.OutputChannel): ErrorHandler {
	const restarts: number[] = [];
	let toastShown = false;
	const giveUp = (): void => {
		if (!toastShown) {
			toastShown = true;
			void showLspCrashed(channel);
		}
	};
	// `handled: true` opts out of vscode-languageclient's built-in
	// "Client X: connection to server is erroring" / "Connection to
	// server got closed" toasts at lib/common/client.js:1195/1202
	// and ~handleConnectionClosed. Our showLspCrashed toast (fired
	// from giveUp on permanent failure) is the single user-facing
	// surface; everything else is just channel-logged.
	return {
		// Runtime errors on the wire (parse failures, write
		// failures). vscode-languageclient retries the connection
		// after `count` of these. Pass through.
		error: (
			_error: Error,
			_message: Message | undefined,
			count: number | undefined,
		): ErrorHandlerResult => {
			if (count !== undefined && count <= 3) {
				return { action: ErrorAction.Continue, handled: true };
			}
			return { action: ErrorAction.Shutdown, handled: true };
		},
		// The transport closed (server exited, pipe broken).
		// Restart up to MAX_RESTART_COUNT times within the window;
		// after that, surface to the user.
		closed: (): CloseHandlerResult => {
			const now = Date.now();
			// Drop entries older than the budget window so we count
			// restarts within a sliding 3-minute frame, not all-time.
			while (restarts.length > 0) {
				const oldest = restarts[0];
				if (oldest === undefined || now - oldest <= RESTART_WINDOW_MS) {
					break;
				}
				restarts.shift();
			}
			restarts.push(now);
			if (restarts.length > MAX_RESTART_COUNT) {
				log(
					`LSP client crashed ${restarts.length} times in ${RESTART_WINDOW_MS / 1000}s; giving up`,
				);
				giveUp();
				return { action: CloseAction.DoNotRestart, handled: true };
			}
			log(`LSP client closed (attempt ${restarts.length}); restarting`);
			return { action: CloseAction.Restart, handled: true };
		},
	};
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
