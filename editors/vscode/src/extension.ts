import * as vscode from "vscode";
import { buildGafferArgv, tryFetchManifest } from "./discovery/cli.js";
import { createProjectIndex } from "./discovery/project-index.js";
import { TomlCodeLensProvider } from "./lensing/toml-provider.js";
import { JsCodeLensProvider } from "./lensing/js-provider.js";
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
	setTomlDiagnostics,
} from "./diagnostics.js";
import {
	showManifestFailure,
	showNoProjections,
	showTrustWarning,
} from "./notifications.js";

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

	// Initial snapshots - both awaited up front. Lens providers are
	// constructed with the loaded data and registered after, so first
	// provideCodeLenses call sees real state.
	const initialIndex = await createProjectIndex();
	const initialManifest = await tryFetchManifest(
		initialIndex.projectRoot,
		showManifestFailure,
	);

	const stepProvider = new StepProvider();
	const stateProvider = new StateProvider();
	const statusProvider = new StatusViewProvider();
	const phaseTracker = new PhaseTracker((phase) =>
		statusProvider.setPhase(phase),
	);
	const tomlCodeLens = new TomlCodeLensProvider(initialManifest);
	const jsCodeLens = new JsCodeLensProvider(initialIndex, initialManifest);

	const controller = new SessionController({
		buildArgv: buildGafferArgv,
		stepProvider,
		stateProvider,
		statusProvider,
		phaseTracker,
		pushDebugState: (state) => {
			tomlCodeLens.setDebugState(state);
			jsCodeLens.setDebugState(state);
		},
		readDebugPort: () =>
			vscode.workspace.getConfiguration("gaffer").get<number>("debugPort", -1),
	});
	controller.register(context);

	// Async orchestrator, serialised on a promise chain so overlapping
	// events (toml save + config change in quick succession) can't
	// interleave setters out of order. The body is try/caught so a
	// transient failure (e.g. workspace becoming briefly unavailable)
	// can't poison the chain and brick all future refreshes.
	let refreshChain: Promise<void> = Promise.resolve();
	const reloadLensState = (): Promise<void> => {
		refreshChain = refreshChain.then(async () => {
			try {
				const idx = await createProjectIndex();
				const m = await tryFetchManifest(idx.projectRoot, showManifestFailure);
				jsCodeLens.setIndex(idx);
				jsCodeLens.setManifest(m);
				tomlCodeLens.setManifest(m);
			} catch (err) {
				log(
					`Lens state reload failed: ${err instanceof Error ? err.message : String(err)}`,
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
			{ scheme: "file", pattern: "**/gaffer.toml" },
			tomlCodeLens,
		),
		vscode.languages.registerCodeLensProvider(
			{ scheme: "file", language: "javascript" },
			jsCodeLens,
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
		const index = await createProjectIndex();
		if (index.size === 0) {
			void showNoProjections();
			return;
		}
		const picked = await vscode.window.showQuickPick(
			index.projections.map((p) => ({
				label: p.name,
				description: vscode.workspace.asRelativePath(p.tomlUri),
				projection: p,
			})),
			{ placeHolder: "Select a projection to debug" },
		);
		if (!picked) return;
		const manifest = await tryFetchManifest(
			index.projectRoot,
			showManifestFailure,
		);
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

	const tomlWatcher =
		vscode.workspace.createFileSystemWatcher("**/gaffer.toml");
	tomlWatcher.onDidChange(async () => {
		log("gaffer.toml changed");
		await reloadLensState();
	});
	tomlWatcher.onDidCreate(async () => {
		log("gaffer.toml created");
		await reloadLensState();
	});
	tomlWatcher.onDidDelete(async (uri) => {
		log("gaffer.toml deleted");
		// provideCodeLenses is the only writer of toml diagnostics for
		// a given URI, but it never re-fires once the document is gone.
		// Clear here so a deleted toml's invalid-fixture warnings don't
		// linger in the Problems panel.
		setTomlDiagnostics(uri, []);
		await reloadLensState();
	});
	context.subscriptions.push(tomlWatcher);

	context.subscriptions.push(
		vscode.workspace.onDidChangeConfiguration(async (e) => {
			if (e.affectsConfiguration("gaffer.command")) {
				log("gaffer.command setting changed");
				await reloadLensState();
			}
		}),
		vscode.workspace.onDidGrantWorkspaceTrust(async () => {
			log("workspace trusted");
			await reloadLensState();
		}),
	);
}

export function deactivate(): void {}
