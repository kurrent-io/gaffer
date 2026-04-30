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

	context.subscriptions.push(
		vscode.commands.registerCommand("gaffer.stopDebug", () =>
			controller.stop(),
		),
		vscode.commands.registerCommand(
			"gaffer.debugProjection",
			(args: DebugProjectionArgs) => controller.start(args),
		),
	);

	const refreshAll = (): void => {
		refreshLenses();
		void projectIndex.refresh().then(() => jsCodeLens.refresh());
	};

	const tryFetchManifest = async (): Promise<void> => {
		if (!vscode.workspace.isTrusted) {
			log("workspace untrusted, skipping manifest fetch");
			return;
		}
		try {
			await cli.fetchManifest(projectIndex.projectRoot);
			refreshAll();
		} catch {
			void vscode.window
				.showWarningMessage(
					'Gaffer CLI not found. Install gaffer or configure "gaffer.command" in settings.',
					"Open Settings",
				)
				.then((choice) => {
					if (choice === "Open Settings") {
						void vscode.commands.executeCommand(
							"workbench.action.openSettings",
							"gaffer.command",
						);
					}
				});
		}
	};

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
