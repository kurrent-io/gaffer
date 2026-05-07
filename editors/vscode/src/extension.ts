import * as vscode from "vscode";
import { buildGafferArgv, tryFetchManifest } from "./discovery/cli.js";
import type { Manifest } from "./discovery/schemas.js";
import { LspCodeLensProvider } from "./lsp/lens-provider.js";
import { StepProvider } from "./panels/step.js";
import { StateProvider } from "./panels/state.js";
import { StatusViewProvider } from "./panels/status.js";
import { dispatchDapEvent } from "./debugging/dap-dispatch.js";
import { PausePendingTrackerFactory } from "./debugging/pause-pending-tracker.js";
import { PhaseTracker } from "./debugging/phase-tracker.js";
import { RestartTrackerFactory } from "./debugging/restart-tracker.js";
import {
	SessionController,
	type DebugProjectionArgs,
} from "./debugging/session-controller.js";
import { initOutput, log } from "./output.js";
import {
	DismissDiagnosticActionProvider,
	clearDiagnosticsForUri,
	initDiagnostics,
} from "./diagnostics.js";
import { showManifestFailure } from "./notifications.js";
import {
	retryStartLanguageClient,
	startLanguageClient,
	stopLanguageClient,
} from "./lsp/client.js";
import { registerTypeScriptPlugin } from "./lsp/typescript-plugin.js";
import { GafferMcpProvider } from "./mcp/provider.js";
import { runProjection } from "./commands/run-projection.js";
import { debugProjectionPick } from "./commands/debug-projection-pick.js";

// workspaceCwd returns the first workspace folder's filesystem
// path so child processes (e.g. gaffer manifest) spawn relative
// to the user's project, not the editor's launch cwd. Returns
// undefined for single-buffer sessions with no workspace.
function workspaceCwd(): string | undefined {
	return vscode.workspace.workspaceFolders?.[0]?.uri.fsPath;
}

export async function activate(
	context: vscode.ExtensionContext,
): Promise<void> {
	initOutput(context);
	initDiagnostics(context);

	// Stale-on-edit: any text change to a file with a runtime error
	// invalidates that error (the in-memory content no longer matches
	// what was running when the error fired). Clear preemptively so
	// the user doesn't keep staring at a squiggle on code they're
	// already fixing. Wired here, adjacent to initDiagnostics, so
	// it's live before any awaited work below could surface a fatal.
	// Selector intentionally broad (any file) so we cover whatever
	// path the runtime reports — selector and reportFatalError stay
	// in sync regardless of future projection extensions.
	context.subscriptions.push(
		vscode.workspace.onDidChangeTextDocument((e) => {
			if (e.document.uri.scheme === "file" && e.contentChanges.length > 0) {
				clearDiagnosticsForUri(e.document.uri);
			}
		}),
		vscode.languages.registerCodeActionsProvider(
			{ scheme: "file" },
			new DismissDiagnosticActionProvider(),
			{ providedCodeActionKinds: [vscode.CodeActionKind.QuickFix] },
		),
	);

	// Initial manifest snapshot - awaited up front so the lens
	// provider's first provideCodeLenses call sees the real
	// dev/--debug capability set. cwd is the first workspace
	// folder so gaffer-relative binaries resolve correctly;
	// node's execFile defaults to process.cwd() (the editor's
	// launch directory), not the workspace, so we must pass
	// it explicitly.
	const initialManifest = await tryFetchManifest(
		workspaceCwd(),
		showManifestFailure,
	);

	const stepProvider = new StepProvider();
	const stateProvider = new StateProvider();
	const statusProvider = new StatusViewProvider();
	const phaseTracker = new PhaseTracker((phase) =>
		statusProvider.setPhase(phase),
	);
	const lspCodeLens = new LspCodeLensProvider();
	lspCodeLens.setManifest(initialManifest);

	// Single source of truth for the latest manifest. The LSP spawn
	// gate reads it via predicate; the reload chain updates it.
	let latestManifest: Manifest | null = initialManifest;

	// Spawn the LSP server. The lens provider activates once
	// the client is ready; until then provideCodeLenses returns
	// [] (briefly, while initialize completes). startLanguageClient
	// owns both the trust gate and the manifest gate, deferring
	// the spawn until both clear and reattempting via
	// retryStartLanguageClient when the manifest reload chain
	// publishes a non-null result.
	startLanguageClient(
		context,
		() => latestManifest !== null,
		(client) => {
			lspCodeLens.setClient(client);
		},
	);

	// Wire the tsserver plugin's configuration. Loaded by tsserver
	// via the `typescriptServerPlugins` contribution; configured here
	// with the vendored projection-types path. Static for the session
	// unless the user toggles gaffer.injectProjectionTypes.
	registerTypeScriptPlugin(context);

	// Register `gaffer mcp` as an MCP server with VS Code so
	// Copilot Chat (and any other MCP-aware agent in VS Code) picks
	// it up automatically. Provider returns [] under untrusted
	// workspaces; fires onDidChange on trust grant and on workspace
	// folder changes so the picker tracks reality.
	const mcpProvider = new GafferMcpProvider();
	context.subscriptions.push(
		mcpProvider,
		vscode.lm.registerMcpServerDefinitionProvider("gaffer", mcpProvider),
		vscode.workspace.onDidGrantWorkspaceTrust(() => mcpProvider.refresh()),
		vscode.workspace.onDidChangeWorkspaceFolders(() => mcpProvider.refresh()),
		vscode.workspace.onDidChangeConfiguration((e) => {
			if (e.affectsConfiguration("gaffer.command")) {
				mcpProvider.refresh();
			}
		}),
	);

	const controller = new SessionController({
		buildArgv: buildGafferArgv,
		stepProvider,
		stateProvider,
		statusProvider,
		phaseTracker,
		pushDebugState: (state) => {
			lspCodeLens.setDebugState(state);
		},
		readDebugPort: () =>
			vscode.workspace.getConfiguration("gaffer").get<number>("debugPort", -1),
	});
	controller.register(context);

	// Async orchestrator, serialised on a promise chain so
	// overlapping events (config change + trust grant in quick
	// succession) can't interleave setters out of order. Body is
	// try/caught so a transient failure can't poison the chain.
	// Drives the manifest only - toml content and projection
	// metadata flow through the LSP server's own walker /
	// watcher / cached parses.
	let refreshChain: Promise<void> = Promise.resolve();
	const reloadManifest = (): Promise<void> => {
		refreshChain = refreshChain.then(async () => {
			try {
				const m = await tryFetchManifest(workspaceCwd(), showManifestFailure);
				latestManifest = m;
				lspCodeLens.setManifest(m);
				// Re-evaluate the LSP spawn gate. Idempotent if the
				// client is already running; kicks the spawn if the
				// manifest just transitioned from null to a value
				// (e.g. user fixed gaffer.command after a failed init).
				retryStartLanguageClient();
			} catch (err) {
				log(
					`Manifest reload failed: ${err instanceof Error ? err.message : String(err)}`,
				);
			}
		});
		return refreshChain;
	};

	context.subscriptions.push(
		vscode.window.registerTreeDataProvider("gaffer.step", stepProvider),
		vscode.window.registerTreeDataProvider("gaffer.state", stateProvider),
		vscode.window.registerWebviewViewProvider("gaffer.status", statusProvider, {
			webviewOptions: { retainContextWhenHidden: true },
		}),
	);

	context.subscriptions.push(
		vscode.debug.registerDebugAdapterTrackerFactory(
			"gaffer",
			new PausePendingTrackerFactory(statusProvider),
		),
		vscode.debug.registerDebugAdapterTrackerFactory(
			"gaffer",
			new RestartTrackerFactory({
				stepProvider,
				stateProvider,
				statusProvider,
				phaseTracker,
				sessionName: () => controller.getDebugState().name ?? "projection",
			}),
		),
	);

	context.subscriptions.push(
		vscode.debug.registerDebugAdapterDescriptorFactory("gaffer", {
			createDebugAdapterDescriptor(session) {
				// session.configuration.port is set by SessionController.start
				// (from the CLI's actual bound port via waitForDebug). For
				// launch.json-driven attach the schema defaults it to 4711.
				const port = session.configuration.port;
				if (typeof port !== "number") {
					throw new Error("gaffer debug session missing port in configuration");
				}
				return new vscode.DebugAdapterServer(port);
			},
		}),
	);

	context.subscriptions.push(
		vscode.languages.registerCodeLensProvider(
			[
				{ scheme: "file", pattern: "**/gaffer.toml" },
				{ scheme: "file", language: "javascript" },
			],
			lspCodeLens,
		),
	);

	context.subscriptions.push(
		vscode.debug.onDidReceiveDebugSessionCustomEvent((e) =>
			dispatchDapEvent(e, {
				stepProvider,
				stateProvider,
				statusProvider,
				phaseTracker,
				setEngineMode: (mode) => controller.setEngineMode(mode),
			}),
		),
	);

	// Command handlers live in src/commands/. activate() injects the
	// SessionController.start binding (and workspace cwd resolver for
	// runProjection's manifest fetch); the command bodies own their
	// own UX flows.
	const startSession = (args: DebugProjectionArgs): Promise<void> =>
		controller.start(args);
	context.subscriptions.push(
		vscode.commands.registerCommand("gaffer.stopDebug", () =>
			controller.stop(),
		),
		vscode.commands.registerCommand(
			"gaffer.debugProjection",
			(args: DebugProjectionArgs) => controller.start(args),
		),
		vscode.commands.registerCommand(
			"gaffer.debugProjectionPick",
			debugProjectionPick({ start: startSession }),
		),
		vscode.commands.registerCommand(
			"gaffer.runProjection",
			runProjection({ start: startSession, workspaceCwd }),
		),
		// Click target for the "Invalid fixture: <reason>" lens. The lens
		// is informational; the user fixes the toml. CodeLens.command is
		// required by VS Code, so we route to a registered no-op.
		vscode.commands.registerCommand("gaffer.noop", () => {}),
		// Lightbulb action target for runtime fatal-error squiggles.
		// Clears the diagnostic for the file without requiring an edit.
		vscode.commands.registerCommand(
			"gaffer.dismissDiagnostic",
			(uri: vscode.Uri) => clearDiagnosticsForUri(uri),
		),
	);

	context.subscriptions.push(
		vscode.workspace.onDidChangeConfiguration(async (e) => {
			if (e.affectsConfiguration("gaffer.command")) {
				log("gaffer.command setting changed");
				await reloadManifest();
			}
		}),
		vscode.workspace.onDidGrantWorkspaceTrust(async () => {
			log("workspace trusted");
			lspCodeLens.refresh();
			await reloadManifest();
		}),
	);
}

export async function deactivate(): Promise<void> {
	await stopLanguageClient();
}
