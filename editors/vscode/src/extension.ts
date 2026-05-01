import * as vscode from "vscode";
import { GafferCli } from "./discovery/cli.js";
import { ProjectIndex } from "./discovery/project-index.js";
import { TomlCodeLensProvider } from "./lensing/toml-provider.js";
import { JsCodeLensProvider } from "./lensing/js-provider.js";
import { StepProvider } from "./panels/step.js";
import { StateProvider } from "./panels/state.js";
import { StatusViewProvider } from "./panels/status.js";
import { dispatchDapEvent } from "./debugging/dap-dispatch.js";
import {
	SessionController,
	type DebugProjectionArgs,
} from "./debugging/session-controller.js";
import type { DebugState } from "./types.js";

const DEBUG_PORT = 4711;

export function activate(context: vscode.ExtensionContext): void {
	const output = vscode.window.createOutputChannel("Gaffer", "log");
	context.subscriptions.push(output);
	const log = (msg: string): void => {
		output.appendLine(msg);
		console.log(`Gaffer: ${msg}`);
	};

	const cli = new GafferCli(log);
	const projectIndex = new ProjectIndex();

	const debugState: DebugState = { name: null, status: "idle" };

	const stepProvider = new StepProvider();
	const stateProvider = new StateProvider();
	const statusProvider = new StatusViewProvider();
	const tomlCodeLens = new TomlCodeLensProvider(cli, debugState);
	const jsCodeLens = new JsCodeLensProvider(cli, projectIndex, debugState);

	const refreshLenses = (): void => {
		tomlCodeLens.refresh();
		jsCodeLens.refresh();
	};

	const controller = new SessionController({
		cli,
		stepProvider,
		stateProvider,
		statusProvider,
		debugState,
		refreshLenses,
		log,
		output,
	});
	controller.register(context);

	context.subscriptions.push(
		vscode.window.registerTreeDataProvider("gaffer.step", stepProvider),
		vscode.window.registerTreeDataProvider("gaffer.state", stateProvider),
		vscode.window.registerWebviewViewProvider("gaffer.status", statusProvider),
	);

	context.subscriptions.push(
		vscode.debug.registerDebugAdapterDescriptorFactory("gaffer", {
			createDebugAdapterDescriptor(session) {
				const configured = session.configuration.port;
				const port = typeof configured === "number" ? configured : DEBUG_PORT;
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
				setInspecting: (inspecting) => controller.setInspecting(inspecting),
				log,
			}),
		),
	);

	const refreshAll = (): void => {
		refreshLenses();
		void projectIndex.refresh().then(() => jsCodeLens.refresh());
	};

	const showManifestFailure = (err: unknown): void => {
		const raw = err instanceof Error ? err.message : String(err);
		const truncated = raw.length > 200 ? `${raw.slice(0, 200)}…` : raw;
		void vscode.window
			.showErrorMessage(
				`Gaffer CLI failed: ${truncated}`,
				"View Output",
				"Open Settings",
			)
			.then((choice) => {
				if (choice === "View Output") {
					output.show();
				} else if (choice === "Open Settings") {
					void vscode.commands.executeCommand(
						"workbench.action.openSettings",
						"gaffer.command",
					);
				}
			});
	};

	const tryFetchManifest = async (): Promise<void> => {
		if (!vscode.workspace.isTrusted) {
			log("workspace untrusted, skipping manifest fetch");
			return;
		}
		try {
			await cli.fetchManifest(projectIndex.projectRoot);
			refreshAll();
		} catch (err) {
			showManifestFailure(err);
		}
	};

	// One-shot retry used by the command handlers. Lens-driven Debug already
	// requires a loaded manifest (the lens hides itself otherwise), but the
	// palette-invoked Run Projection has no such gate, and a stale null
	// manifest after a failed activation fetch shouldn't permanently brick
	// the commands.
	const ensureManifest = async (): Promise<boolean> => {
		if (cli.manifest) return true;
		if (!vscode.workspace.isTrusted) {
			showManifestFailure(new Error("workspace not trusted"));
			return false;
		}
		try {
			await cli.fetchManifest(projectIndex.projectRoot);
			refreshAll();
			return true;
		} catch (err) {
			showManifestFailure(err);
			return false;
		}
	};

	const runProjection = async (): Promise<void> => {
		await projectIndex.refresh();
		const projections = projectIndex.projections;
		// Empty check before manifest fetch: a workspace with no gaffer.toml
		// would otherwise misreport "Gaffer CLI failed" when the real
		// diagnostic is "no projections here".
		if (projections.length === 0) {
			void vscode.window.showInformationMessage(
				"Gaffer: no projections found in this workspace.",
			);
			return;
		}
		if (!(await ensureManifest())) return;
		const picked = await vscode.window.showQuickPick(
			projections.map((p) => ({
				label: p.name,
				description: vscode.workspace.asRelativePath(p.tomlUri),
				projection: p,
			})),
			{ placeHolder: "Select a projection to debug" },
		);
		if (!picked) return;
		await controller.start({
			name: picked.projection.name,
			tomlUri: picked.projection.tomlUri,
		});
	};

	context.subscriptions.push(
		vscode.commands.registerCommand("gaffer.stopDebug", () =>
			controller.stop(),
		),
		vscode.commands.registerCommand(
			"gaffer.debugProjection",
			async (args: DebugProjectionArgs) => {
				if (!(await ensureManifest())) return;
				await controller.start(args);
			},
		),
		vscode.commands.registerCommand("gaffer.runProjection", runProjection),
	);

	const tomlWatcher =
		vscode.workspace.createFileSystemWatcher("**/gaffer.toml");
	tomlWatcher.onDidChange(() => {
		log("gaffer.toml changed");
		refreshAll();
	});
	tomlWatcher.onDidCreate(() => {
		log("gaffer.toml created");
		void tryFetchManifest();
	});
	tomlWatcher.onDidDelete(() => {
		log("gaffer.toml deleted");
		refreshAll();
	});
	context.subscriptions.push(tomlWatcher);

	context.subscriptions.push(
		vscode.workspace.onDidChangeConfiguration((e) => {
			if (e.affectsConfiguration("gaffer.command")) {
				log("gaffer.command setting changed, refetching manifest");
				void tryFetchManifest();
			}
		}),
	);

	context.subscriptions.push(
		vscode.workspace.onDidGrantWorkspaceTrust(() => {
			log("workspace trusted, fetching manifest");
			void tryFetchManifest();
		}),
	);

	void projectIndex.refresh().then(tryFetchManifest);
}

export function deactivate(): void {}
