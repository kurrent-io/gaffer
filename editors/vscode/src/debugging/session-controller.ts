// Owns the active debug session and the state that follows from it.
//
// Before this controller, the teardown sequence (dispose session, clear
// debugState, refresh lenses, reset context flags) was duplicated across
// five exit paths in extension.ts. Centralising it here makes the
// state-machine impossible to leave inconsistent, and replaces the
// per-session onDidTerminateDebugSession registration with one
// persistent listener registered at construction.

import * as vscode from "vscode";
import { GafferSession } from "../ipc/session.js";
import type { GafferCli } from "../discovery/cli.js";
import type { StepProvider } from "../panels/step.js";
import type { StateProvider } from "../panels/state.js";
import type { StatusViewProvider } from "../panels/status.js";
import type { DebugState } from "../types.js";

const DEBUG_PORT = 4711;

export interface DebugProjectionArgs {
	name: string;
	tomlUri: vscode.Uri;
}

export interface SessionControllerDeps {
	cli: GafferCli;
	stepProvider: StepProvider;
	stateProvider: StateProvider;
	statusProvider: StatusViewProvider;
	debugState: DebugState;
	refreshLenses: () => void;
	log: (msg: string) => void;
}

export class SessionController {
	readonly #cli: GafferCli;
	readonly #stepProvider: StepProvider;
	readonly #stateProvider: StateProvider;
	readonly #statusProvider: StatusViewProvider;
	readonly #debugState: DebugState;
	readonly #refreshLenses: () => void;
	readonly #log: (msg: string) => void;
	#activeSession: GafferSession | null = null;

	constructor(deps: SessionControllerDeps) {
		this.#cli = deps.cli;
		this.#stepProvider = deps.stepProvider;
		this.#stateProvider = deps.stateProvider;
		this.#statusProvider = deps.statusProvider;
		this.#debugState = deps.debugState;
		this.#refreshLenses = deps.refreshLenses;
		this.#log = deps.log;
	}

	// Register the one persistent terminate listener. Replaces the
	// per-session disposable pattern in the old code.
	register(context: vscode.ExtensionContext): void {
		context.subscriptions.push(
			vscode.debug.onDidTerminateDebugSession((dbgSession) => {
				if (dbgSession.name === `Gaffer: ${this.#debugState.name}`) {
					this.#log("Debug session ended");
					void this.#cleanup();
				}
			}),
		);
	}

	async start(args: DebugProjectionArgs): Promise<void> {
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

		if (this.#debugState.status !== "idle") {
			this.#log(
				`Ignoring debug request: ${this.#debugState.name ?? "session"} is ${this.#debugState.status}`,
			);
			return;
		}

		const { name, tomlUri } = args;
		const tomlDir = vscode.Uri.joinPath(tomlUri, "..").fsPath;
		const argv = this.#cli.buildArgv([
			"dev",
			name,
			"--json",
			"--debug",
			"--debug-port",
			String(DEBUG_PORT),
		]);

		this.#stepProvider.clear();
		this.#stateProvider.clear();

		await this.#setMode(name, "starting");
		this.#log(`Starting: ${name}`);

		const session = new GafferSession(name, argv, {
			log: this.#log,
			cwd: tomlDir,
		});
		this.#activeSession = session;

		session.on("exit", async (msg) => {
			if (this.#activeSession !== session) return;
			// During "starting", waitForDebug's catch surfaces the error.
			// Once "debugging", the persistent terminate listener tears down.
			// Only surface here for non-zero exits while debugging.
			if (msg.code !== 0 && this.#debugState.status === "debugging") {
				this.#log(`CLI exited with code ${msg.code}`);
				await vscode.window.showErrorMessage(
					`Gaffer: projection faulted (exit code ${msg.code})`,
				);
				await this.#cleanup();
			}
		});

		session
			.on("result", (msg) => {
				if (msg.status === "processed") this.#statusProvider.addProcessed();
				else if (msg.status === "skipped") this.#statusProvider.addSkipped();
			})
			.on("error", () => this.#statusProvider.addError());

		this.#statusProvider.reset(name);
		session.start();
		await vscode.commands.executeCommand("gaffer.status.focus");

		let debugPort: number;
		try {
			const msg = await session.waitForDebug();
			debugPort = msg.port;
			this.#log(`Debug server listening on port ${debugPort}`);
		} catch (err) {
			const errMsg = err instanceof Error ? err.message : String(err);
			this.#log(`Failed to start: ${errMsg}`);
			await vscode.window.showErrorMessage(`Gaffer: ${errMsg}`);
			await this.#cleanup();
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
			this.#log("Debug session failed to start");
			await this.#cleanup();
			return;
		}

		await this.#setMode(name, "debugging");
	}

	async stop(): Promise<void> {
		if (!this.#activeSession) return;
		await vscode.debug.stopDebugging();
		await this.#cleanup();
	}

	// Set the gaffer.inspecting context flag. Called from dap-dispatch on
	// `gaffer/mode` events. Distinct from the session's own mode (idle /
	// starting / debugging) which is tracked in #debugState.
	async setInspecting(inspecting: boolean): Promise<void> {
		await vscode.commands.executeCommand(
			"setContext",
			"gaffer.inspecting",
			inspecting,
		);
	}

	async #cleanup(): Promise<void> {
		if (this.#activeSession) {
			this.#activeSession.dispose();
			this.#activeSession = null;
		}
		await this.#setMode(null, "idle");
	}

	async #setMode(
		name: string | null,
		status: DebugState["status"],
	): Promise<void> {
		this.#debugState.name = name;
		this.#debugState.status = status;
		this.#refreshLenses();
		await vscode.commands.executeCommand(
			"setContext",
			"gaffer.sessionActive",
			status !== "idle",
		);
		if (status === "idle") {
			await this.setInspecting(false);
		}
	}
}
