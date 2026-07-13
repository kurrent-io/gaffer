import crossSpawn from "cross-spawn";
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
} from "vscode-languageclient/node";
import {
	buildGafferArgv,
	gafferRunEnv,
	type SpawnTelemetry,
} from "../discovery/cli.js";
import { log } from "../output.js";
import { showLspCrashed, showLspFailedToStart } from "../notifications/lsp.js";

let client: LanguageClient | undefined;

// Single OutputChannel for the LSP server's lifetime. Created on the
// first spawn, reused across restarts (including the give-up-then-retry
// path). Creating it inside spawnLanguageClient would stack duplicate
// "Gaffer (LSP)" entries in the user's Output dropdown.
let lspChannel: vscode.OutputChannel | undefined;

/** Bound on every `client.stop` call. VS Code's deactivate budget is
 * ~5s; a stuck server can't be allowed to eat the whole budget. */
const STOP_TIMEOUT_MS = 2000;

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
	telemetry: SpawnTelemetry,
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
		void spawnLanguageClient(context, telemetry, onReady);
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
	telemetry: SpawnTelemetry,
	onReady?: (client: LanguageClient) => void,
): Promise<void> {
	// Bail before construction when gaffer.command is empty so we
	// don't sit with a never-startable LanguageClient.
	if (
		buildGafferArgv(["lsp"], { invokerId: telemetry.invokerId() })[0] ===
		undefined
	) {
		log("LSP client: empty gaffer.command, skipping spawn");
		return;
	}
	// Factory-form ServerOptions: vscode-languageclient invokes it on
	// every start AND on every CloseAction.Restart, so a mid-session
	// opt-out (or a gaffer.command change) is picked up by auto-
	// restarts without our intervention.
	const serverOptions: ServerOptions = () => {
		const argv = buildGafferArgv(["lsp"], {
			invokerId: telemetry.invokerId(),
		});
		const command = argv[0];
		if (command === undefined) {
			throw new Error("LSP client: empty gaffer.command");
		}
		// gafferRunEnv (not gafferSpawnEnv): the LSP now dials KurrentDB to
		// fetch deploy status, so it needs GAFFER_KEYRING_PASSWORD to unlock
		// the OAuth token store without a prompt, like the run/debug/mcp spawns.
		const env = gafferRunEnv(telemetry.isOptedOut());
		// Routed through cross-spawn so the Windows PATHEXT lookup
		// works: an npm-installed `gaffer` resolves to a `gaffer.cmd`
		// shim, which Node's bare `spawn(...)` (shell: false) won't
		// find. cross-spawn re-routes .cmd/.bat through cmd.exe with
		// proper arg quoting.
		return Promise.resolve(
			crossSpawn(
				command,
				argv.slice(1),
				env !== undefined ? { env } : undefined,
			),
		);
	};
	if (lspChannel === undefined) {
		lspChannel = vscode.window.createOutputChannel("Gaffer (LSP)");
		context.subscriptions.push(lspChannel);
	}
	const channel = lspChannel;
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
		"Gaffer (LSP)",
		serverOptions,
		clientOptions,
	);
	// Push the disposable BEFORE awaiting start so a mid-init
	// dispose cancels cleanly. The dispose returns the stop()
	// Promise so VS Code (which awaits disposables on
	// extension teardown) waits for the child process to flush.
	// Bounded by the same STOP_TIMEOUT_MS as the deactivate path so
	// a stuck server can't hang teardown via either route.
	let disposed = false;
	context.subscriptions.push({
		dispose: () => {
			disposed = true;
			return c.stop(STOP_TIMEOUT_MS);
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

// Must match MethodRefreshStatus in cli/internal/lsp/protocol.go.
const refreshStatusMethod = "gaffer/refreshStatus";

// requestStatusRefresh asks the LSP server to re-fetch deploy status for one
// gaffer.toml (the manual-refresh command). Fire-and-forget: the fresh status
// arrives via the server's codeLens refresh once it lands. No-op when the
// client isn't running (untrusted workspace, CLI missing, etc.).
export function requestStatusRefresh(uri: vscode.Uri): void {
	const c = client;
	if (!c) return;
	void c.sendNotification(refreshStatusMethod, { uri: uri.toString() });
}

// Restart on the first MAX_RESTART_COUNT closes within
// RESTART_WINDOW_MS, give up on the next one. The give-up branch
// surfaces a toast (showLspCrashed) so the user knows features
// are broken instead of silently degrading - that's the
// difference from vscode-languageclient's default handler.
const MAX_RESTART_COUNT = 4;
const RESTART_WINDOW_MS = 3 * 60 * 1000;

// Exported for direct testing of the restart-budget policy. The
// production caller is spawnLanguageClient; nothing else should
// reach for this.
export function makeErrorHandler(channel: vscode.OutputChannel): ErrorHandler {
	const restarts: number[] = [];
	let toastShown = false;
	const giveUp = (): void => {
		// Drop our handle on the dead client so retryStartLanguageClient
		// can spawn a new one once the user fixes whatever broke.
		// Without this the gate's `if (client) return` branch keeps
		// the never-running stale handle and recovery becomes
		// "reload window".
		client = undefined;
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
	if (!client) {
		lspChannel = undefined;
		return;
	}
	try {
		// vscode-languageclient force-kills on timeout.
		await client.stop(STOP_TIMEOUT_MS);
	} catch (err) {
		const msg = err instanceof Error ? err.message : String(err);
		log(`LSP client stop failed: ${msg}`);
	}
	client = undefined;
	lspChannel = undefined;
}
