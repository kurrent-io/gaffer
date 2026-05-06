import * as vscode from "vscode";
import { buildGafferArgv, tryFetchManifest } from "./discovery/cli.js";
import { LspCodeLensProvider } from "./lsp/lens-provider.js";
import { fetchProjections } from "./lsp/symbols.js";
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
import {
	showManifestFailure,
	showNoProjections,
	showTrustWarning,
} from "./notifications.js";
import { startLanguageClient, stopLanguageClient } from "./lsp/client.js";

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
	// dev/--debug capability set instead of bailing on a null
	// manifest. cwd defaults to the workspace root via the
	// underlying execFile when not provided.
	const initialManifest = await tryFetchManifest(
		undefined,
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

	// Spawn the LSP server. The lens provider activates once
	// the client is ready; until then provideCodeLenses returns
	// [] (briefly, while initialize completes). startLanguageClient
	// owns the trust gate and will defer the spawn until trust
	// is granted if the workspace is currently untrusted.
	startLanguageClient(context, (client) => {
		lspCodeLens.setClient(client);
	});

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
				const m = await tryFetchManifest(undefined, showManifestFailure);
				lspCodeLens.setManifest(m);
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

	// Commands fetch fresh state per call - no shared cache reads. The
	// QuickPick appears instantly from the local index; manifest fetch
	// is deferred until after the user picks so it doesn't gate the UI.
	// Trust is checked explicitly: tryFetchManifest is silent on untrust
	// (returns null without onError), so without this gate the user
	// would pick a projection and see nothing happen.
	const runProjection = async (): Promise<void> => {
		if (!vscode.workspace.isTrusted) {
			void showTrustWarning();
			return;
		}
		const projections = await fetchProjections();
		if (projections.length === 0) {
			void showNoProjections();
			return;
		}
		const picked = await vscode.window.showQuickPick(
			projections.map((p) => ({
				label: p.name,
				description: vscode.workspace.asRelativePath(p.tomlUri),
				projection: p,
			})),
			{ placeHolder: "Select a projection to debug" },
		);
		if (!picked) return;
		const manifest = await tryFetchManifest(undefined, showManifestFailure);
		if (!manifest) return;
		await controller.start({
			name: picked.projection.name,
			tomlUri: picked.projection.tomlUri,
		});
	};

	const debugProjectionPick = async (args: {
		name: string;
		tomlUri: vscode.Uri;
		fixtureNames: string[];
	}): Promise<void> => {
		if (args.fixtureNames.length === 0) return;
		const picked = await vscode.window.showQuickPick(args.fixtureNames, {
			placeHolder: `Pick a fixture to debug ${args.name} with`,
		});
		if (!picked) return;
		await controller.start({
			name: args.name,
			tomlUri: args.tomlUri,
			fixture: picked,
		});
	};

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
			debugProjectionPick,
		),
		vscode.commands.registerCommand("gaffer.runProjection", runProjection),
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
