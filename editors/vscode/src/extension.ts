import * as vscode from "vscode";
import { GafferCli } from "./lib/cli.js";
import { GafferSession } from "./lib/session.js";
import { ProjectIndex } from "./lib/project.js";
import { TomlCodeLensProvider } from "./lib/codelens-toml.js";
import { JsCodeLensProvider } from "./lib/codelens-js.js";
import { StepProvider } from "./lib/panels/step.js";
import { StateProvider } from "./lib/panels/state.js";
import { StatusViewProvider } from "./lib/panels/status.js";
import type {
	DebugState,
	ModeBody,
	StateBody,
	StepEmitBody,
	StepErrorBody,
	StepLogBody,
	StepResultBody,
	StepStartBody,
} from "./types.js";

interface DebugProjectionArgs {
	name: string;
	tomlUri: vscode.Uri;
}

const DEBUG_PORT = 4711;

export function activate(context: vscode.ExtensionContext): void {
	const output = vscode.window.createOutputChannel("Gaffer");
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

	let activeSession: GafferSession | null = null;

	const setDebugState = (
		name: string | null,
		status: DebugState["status"],
	): void => {
		debugState.name = name;
		debugState.status = status;
		tomlCodeLens.refresh();
		jsCodeLens.refresh();
	};

	const setSessionActive = async (active: boolean): Promise<void> => {
		await vscode.commands.executeCommand(
			"setContext",
			"gaffer.sessionActive",
			active,
		);
	};
	const setInspecting = async (inspecting: boolean): Promise<void> => {
		await vscode.commands.executeCommand(
			"setContext",
			"gaffer.inspecting",
			inspecting,
		);
	};

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
			{ pattern: "**/gaffer.toml" },
			tomlCodeLens,
		),
		vscode.languages.registerCodeLensProvider(
			{ language: "javascript" },
			jsCodeLens,
		),
	);

	context.subscriptions.push(
		vscode.debug.onDidReceiveDebugSessionCustomEvent(async (e) => {
			if (e.session.type !== "gaffer") return;
			stateProvider.setDebugSession(e.session);

			try {
				switch (e.event) {
					case "gaffer/stepStart":
						stepProvider.startStep((e.body as StepStartBody).event);
						break;
					case "gaffer/stepLog":
						stepProvider.addLog((e.body as StepLogBody).message);
						break;
					case "gaffer/stepEmit":
						stepProvider.addEmit(e.body as StepEmitBody);
						break;
					case "gaffer/stepResult":
						stepProvider.setResult((e.body as StepResultBody).result);
						break;
					case "gaffer/stepError": {
						const body = e.body as StepErrorBody;
						stepProvider.setError(body.code, body.description);
						await vscode.window.showErrorMessage(
							`Gaffer: ${body.code} - ${body.description}`,
						);
						break;
					}
					case "gaffer/state":
						stateProvider.updateFromState(e.body as StateBody);
						break;
					case "gaffer/mode":
						await setInspecting((e.body as ModeBody).mode === "inspect");
						break;
				}
			} catch (err) {
				const msg = err instanceof Error ? err.message : String(err);
				log(`Malformed DAP event ${e.event}: ${msg}`);
			}
		}),
	);

	const stopSession = async (): Promise<void> => {
		if (!activeSession) return;
		await vscode.debug.stopDebugging();
		activeSession.dispose();
		activeSession = null;
		setDebugState(null, "idle");
		await setSessionActive(false);
		await setInspecting(false);
	};

	context.subscriptions.push(
		vscode.commands.registerCommand("gaffer.stopDebug", stopSession),
	);

	context.subscriptions.push(
		vscode.commands.registerCommand(
			"gaffer.debugProjection",
			async (args: DebugProjectionArgs) => {
				if (!vscode.workspace.isTrusted) {
					void vscode.window
						.showWarningMessage(
							"Trust this workspace to enable Gaffer debugging.",
							"Manage Trust",
						)
						.then((choice) => {
							if (choice === "Manage Trust") {
								void vscode.commands.executeCommand("workbench.trust.manage");
							}
						});
					return;
				}

				if (debugState.status !== "idle") {
					log(
						`Ignoring debug request: ${debugState.name ?? "session"} is ${debugState.status}`,
					);
					return;
				}

				const { name, tomlUri } = args;
				const tomlDir = vscode.Uri.joinPath(tomlUri, "..").fsPath;
				const port = DEBUG_PORT;
				const argv = cli.buildArgv([
					"dev",
					name,
					"--json",
					"--debug",
					"--debug-port",
					String(port),
				]);

				stepProvider.clear();
				stateProvider.clear();

				setDebugState(name, "starting");
				log(`Starting: ${name}`);
				const session = new GafferSession(name, argv, { log, cwd: tomlDir });
				activeSession = session;

				session.on("exit", async (msg) => {
					if (activeSession !== session) return;
					// During "starting", waitForDebug's catch surfaces the error.
					// Once "debugging", the debug-terminate handler tears down. Only
					// surface here for non-zero exits while idle/transitional.
					if (msg.code !== 0 && debugState.status === "debugging") {
						log(`CLI exited with code ${msg.code}`);
						await vscode.window.showErrorMessage(
							`Gaffer: projection faulted (exit code ${msg.code})`,
						);
						setDebugState(null, "idle");
						await setSessionActive(false);
						await setInspecting(false);
						activeSession = null;
					}
				});

				session
					.on("result", (msg) => {
						if (msg.status === "processed") statusProvider.addProcessed();
						else if (msg.status === "skipped") statusProvider.addSkipped();
					})
					.on("error", () => statusProvider.addError());

				statusProvider.reset(name);
				session.start();
				await setSessionActive(true);
				await setInspecting(false);
				await vscode.commands.executeCommand("gaffer.status.focus");

				let debugPort: number;
				try {
					const msg = await session.waitForDebug();
					debugPort = msg.port;
					log(`Debug server listening on port ${debugPort}`);
				} catch (err) {
					const errMsg = err instanceof Error ? err.message : String(err);
					log(`Failed to start: ${errMsg}`);
					await vscode.window.showErrorMessage(`Gaffer: ${errMsg}`);
					session.dispose();
					if (activeSession === session) activeSession = null;
					setDebugState(null, "idle");
					await setSessionActive(false);
					await setInspecting(false);
					return;
				}

				const started = await vscode.debug.startDebugging(
					vscode.workspace.getWorkspaceFolder(tomlUri),
					{
						type: "gaffer",
						request: "attach",
						name: `Gaffer: ${name}`,
						port: debugPort,
						localRoot: tomlDir,
						internalConsoleOptions: "neverOpen",
					},
				);

				if (!started) {
					log("Debug session failed to start");
					session.dispose();
					activeSession = null;
					setDebugState(null, "idle");
					return;
				}

				setDebugState(name, "debugging");

				const disposable = vscode.debug.onDidTerminateDebugSession(
					async (dbgSession) => {
						if (dbgSession.name === `Gaffer: ${name}`) {
							log("Debug session ended");
							session.dispose();
							if (activeSession === session) activeSession = null;
							setDebugState(null, "idle");
							await setSessionActive(false);
							await setInspecting(false);
							disposable.dispose();
						}
					},
				);
				context.subscriptions.push(disposable);
			},
		),
	);

	const refreshAll = (): void => {
		tomlCodeLens.refresh();
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
