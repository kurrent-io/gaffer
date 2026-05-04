// Owns the active debug session and the state that follows from it.
//
// Five-state machine: idle | starting | running | inspecting | ended.
// All teardown paths route through #cleanupSession(mode), which takes
// "idle" (wipe everything, fresh slate) or "ended" (preserve state and
// counters for post-mortem inspection).
//
// Two persistent listeners are registered at construction:
// - onDidStartDebugSession captures the actual DebugSession reference
//   so we can compare by identity (not by name) on terminate. Same-name
//   re-runs would otherwise race.
// - onDidTerminateDebugSession dispatches cleanup based on current
//   status: starting -> idle (never reached running), otherwise ended.

import * as vscode from "vscode";
import {
	createGafferSession,
	type CreateSession,
	type SessionLike,
} from "../ipc/session.js";
import { log } from "../output.js";
import { clearDiagnostics, reportFatalError } from "../diagnostics.js";
import {
	showPortInUse,
	showProjectionFailed,
	showProjectionFault,
	showStartFailure,
	showTrustWarning,
} from "../notifications.js";
import type { StepProvider } from "../panels/step.js";
import type { StateProvider } from "../panels/state.js";
import type { StatusViewProvider } from "../panels/status.js";
import type { PhaseTracker } from "./phase-tracker.js";
import type { DebugState } from "../types.js";

type Mode = "idle" | "ended";
type EngineMode = "running" | "inspecting";

export interface DebugProjectionArgs {
	name: string;
	tomlUri: vscode.Uri;
}

export interface SessionControllerDeps {
	buildArgv: (args: string[]) => string[];
	stepProvider: StepProvider;
	stateProvider: StateProvider;
	statusProvider: StatusViewProvider;
	phaseTracker: PhaseTracker;
	// Called after every status transition with the new state. Lens
	// providers each hold their own copy and rerender via the state's
	// onDidChange. Push semantics (no shared mutable reference) - the
	// state value is the contract, not the object identity.
	pushDebugState: (state: Readonly<DebugState>) => void;
	// Factory for the underlying CLI-driving session. Production
	// passes createGafferSession; tests substitute a fake to avoid
	// spawning a real subprocess.
	createSession?: CreateSession;
}

export class SessionController implements vscode.Disposable {
	readonly #buildArgv: (args: string[]) => string[];
	readonly #stepProvider: StepProvider;
	readonly #stateProvider: StateProvider;
	readonly #statusProvider: StatusViewProvider;
	readonly #phaseTracker: PhaseTracker;
	// Owned privately. External readers go through getDebugState which
	// returns a Readonly view. Mutations only inside #setStatus, which
	// is the single point that pushes to consumers via #pushDebugState.
	readonly #debugState: DebugState = { name: null, status: "idle" };
	readonly #pushDebugState: (state: Readonly<DebugState>) => void;
	readonly #createSession: CreateSession;
	#activeSession: SessionLike | null = null;
	// Captured via onDidStartDebugSession, compared by reference in the
	// terminate listener. Name-based comparison is racey when the user
	// rapidly Stop+Starts the same projection.
	#activeDebugSession: vscode.DebugSession | null = null;
	// Mode events arriving during `starting` are stashed here and
	// applied on transition to running. Necessary for the eventual
	// #29a "start-paused" CLI flag where the first gaffer/mode=inspect
	// arrives before our status flip; without queueing we'd be stuck
	// in running while the engine is paused.
	#pendingEngineMode: EngineMode | null = null;
	// Set by the fatal_error session listener; checked by the
	// waitForDebug catch and exit handler to suppress their own toasts
	// when fatal_error already surfaced its own. Reset at the start of
	// each new session (not in cleanup) so the catch path can still
	// observe it after cleanup has awaited in the same async chain.
	#fatalErrorSeen = false;

	constructor(deps: SessionControllerDeps) {
		this.#buildArgv = deps.buildArgv;
		this.#stepProvider = deps.stepProvider;
		this.#stateProvider = deps.stateProvider;
		this.#statusProvider = deps.statusProvider;
		this.#phaseTracker = deps.phaseTracker;
		this.#pushDebugState = deps.pushDebugState;
		this.#createSession = deps.createSession ?? createGafferSession;
	}

	getDebugState(): Readonly<DebugState> {
		return this.#debugState;
	}

	register(context: vscode.ExtensionContext): void {
		context.subscriptions.push(
			this,
			vscode.debug.onDidStartDebugSession((s) => {
				if (
					s.configuration.type === "gaffer" &&
					this.#debugState.status === "starting"
				) {
					this.#activeDebugSession = s;
				}
			}),
			vscode.debug.onDidTerminateDebugSession((dbg) => {
				if (dbg !== this.#activeDebugSession) return;
				const s = this.#debugState.status;
				if (s === "idle" || s === "ended") return;
				const mode: Mode = s === "starting" ? "idle" : "ended";
				log("Debug session terminated");
				void this.#cleanupSession(mode);
			}),
		);
	}

	dispose(): void {
		if (this.#activeSession) {
			this.#activeSession.dispose();
			this.#activeSession = null;
		}
		this.#activeDebugSession = null;
	}

	async start(args: DebugProjectionArgs): Promise<void> {
		if (!vscode.workspace.isTrusted) {
			void showTrustWarning();
			return;
		}

		const status = this.#debugState.status;
		if (
			status === "starting" ||
			status === "running" ||
			status === "inspecting"
		) {
			log(
				`Ignoring debug request: ${this.#debugState.name ?? "session"} is ${status}`,
			);
			return;
		}
		if (status === "ended") {
			await this.#cleanupSession("idle");
		}

		// Fresh slate for diagnostics: any squiggles from a previous
		// session (or a previous compile-time fatal that never reached
		// running) get cleared right before we kick off the new one.
		clearDiagnostics();
		// Reset the fatal-error signal at the start (not in cleanup) so
		// the waitForDebug catch can still see it after the exit handler
		// has awaited cleanup - cleanup awaits in the same async chain
		// and would otherwise wipe the flag before the catch checks it.
		this.#fatalErrorSeen = false;

		const { name, tomlUri } = args;
		const tomlDir = vscode.Uri.joinPath(tomlUri, "..").fsPath;
		// Port we ask the CLI to bind to. The CLI confirms via the debug
		// message and we attach using whatever it actually bound (below).
		// gaffer.debugPort defaults to -1 ("unset") - we omit the flag
		// in that case and let the CLI's own default (auto-pick a free
		// port) take effect.
		const requestedPort = vscode.workspace
			.getConfiguration("gaffer")
			.get<number>("debugPort", -1);
		const argv = this.#buildArgv([
			"dev",
			name,
			"--json",
			"--debug",
			...(requestedPort >= 0 ? ["--debug-port", String(requestedPort)] : []),
			// Start-paused is the extension's default UX: clicking Debug
			// lands the user in `inspecting` immediately so the State view
			// is populated and the user can explore before processing
			// begins. With breakpoints set the CLI runs to the first hit
			// instead.
			"--start-paused-if-no-breakpoints",
		]);

		await this.#setStatus(name, "starting");
		log(`Starting: ${name}`);

		const session = this.#createSession(name, argv, { cwd: tomlDir });
		this.#activeSession = session;

		session.on("exit", async (msg) => {
			if (this.#activeSession !== session) return;
			const s = this.#debugState.status;
			// During starting: CLI died after debug message but before/during
			// attach (waitForDebug catch handles the pre-debug case). No
			// post-mortem to preserve - never reached running.
			if (s === "starting") {
				log(`CLI exited during start (code ${msg.code})`);
				if (!this.#fatalErrorSeen) {
					await showStartFailure(`CLI exited (code ${msg.code})`);
				}
				await this.#cleanupSession("idle");
				return;
			}
			// During running/inspecting: route through ended cleanup so the
			// State view + Status counters survive for inspection.
			if (s === "running" || s === "inspecting") {
				log(`CLI exited with code ${msg.code}`);
				if (msg.code !== 0 && !this.#fatalErrorSeen) {
					await showProjectionFault(msg.code);
				}
				await this.#cleanupSession("ended");
			}
			// idle/ended: already cleaned up; ignore.
		});

		session.on("fatal_error", (msg) => {
			this.#fatalErrorSeen = true;
			if (msg.code === "PORT_IN_USE") {
				log(`Port in use: ${msg.description}`);
				void showPortInUse(msg.description);
				return;
			}
			if (msg.file) {
				reportFatalError({
					file: msg.file,
					line: msg.line,
					column: msg.column,
					code: msg.code,
					description: msg.description,
					jsStack: msg.jsStack,
					eventId: msg.eventId,
				});
			} else {
				log(`Fatal error (no file): ${msg.code} - ${msg.description}`);
			}
			void showProjectionFailed();
		});

		this.#statusProvider.reset(name);
		this.#phaseTracker.reset();
		session.start();

		let debugPort: number;
		try {
			const msg = await session.waitForDebug();
			debugPort = msg.port;
			log(`Debug server listening on port ${debugPort}`);
		} catch (err) {
			const errMsg = err instanceof Error ? err.message : String(err);
			log(`Failed to start: ${errMsg}`);
			if (!this.#fatalErrorSeen) {
				await showStartFailure(errMsg);
			}
			await this.#cleanupSession("idle");
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
			await this.#cleanupSession("idle");
			return;
		}

		// Apply any engine mode that arrived during starting. With #29a
		// the CLI emits gaffer/mode=inspect immediately on connect; that
		// event landed during starting and got stashed. Default to
		// "running" if nothing was pending.
		const initial: EngineMode = this.#pendingEngineMode ?? "running";
		this.#pendingEngineMode = null;
		await this.#setStatus(name, initial);

		// Focus the Gaffer panel after the views are revealed. Done last
		// because vscode.debug.startDebugging gives focus to the panel
		// container's last-used tab (often Terminal); we need to override
		// that, and the Gaffer views have when-clauses gated on
		// gaffer.mode so they're not focusable until #setStatus flips it.
		const focusTarget =
			initial === "inspecting" ? "gaffer.step.focus" : "gaffer.status.focus";
		await vscode.commands.executeCommand(focusTarget);
	}

	async stop(): Promise<void> {
		// Cancel during starting goes to idle - no post-mortem to preserve
		// since the session never reached running. Stop during running or
		// inspecting goes to ended for inspection.
		const status = this.#debugState.status;
		if (
			status !== "starting" &&
			status !== "running" &&
			status !== "inspecting"
		) {
			return;
		}
		await vscode.debug.stopDebugging();
		await this.#cleanupSession(status === "starting" ? "idle" : "ended");
	}

	// Apply an engine-mode update from the CLI's gaffer/mode DAP event.
	// During starting, the value is stashed and applied on transition to
	// running. Outside running/inspecting/starting, the event is dropped.
	async setEngineMode(mode: EngineMode): Promise<void> {
		const status = this.#debugState.status;
		if (status === "starting") {
			this.#pendingEngineMode = mode;
			return;
		}
		if (status !== "running" && status !== "inspecting") return;
		if (status === mode) return;
		await this.#setStatus(this.#debugState.name, mode);
	}

	async #cleanupSession(mode: Mode): Promise<void> {
		// Idempotency: if already in the target mode, the cleanup
		// already ran (overlap from listener+exit-handler+stop button).
		// The synchronous status flip in #setStatus is what makes this
		// check reliable - the *first* of multiple overlapping cleanups
		// flips status before any await, so subsequent calls early-return.
		if (this.#debugState.status === mode) return;

		if (this.#activeSession) {
			this.#activeSession.dispose();
			this.#activeSession = null;
		}
		this.#activeDebugSession = null;
		this.#pendingEngineMode = null;

		if (mode === "idle") {
			this.#stepProvider.clear();
			this.#stateProvider.clear();
			// Phase tracker still terminates cleanly even on the idle
			// path so a future session.start()'s reset() flips us off
			// "Disconnected" cleanly. Without this an aborted-during-
			// starting session would leave the chip stuck at whatever
			// state it last reached.
			this.#phaseTracker.markEnded();
			await this.#setStatus(null, "idle");
		} else {
			// "ended" - preserve state + counters for post-mortem
			// inspection.
			this.#stepProvider.clear();
			this.#stateProvider.markEnded();
			this.#statusProvider.markEnded();
			this.#phaseTracker.markEnded();
			await this.#setStatus(this.#debugState.name, "ended");
		}
		// Diagnostics are NOT cleared here. A compile-time fatal_error
		// fires during `starting` and routes through cleanup("idle") -
		// wiping the diagnostic in the same tick would defeat the whole
		// point. Diagnostics survive across cleanups; they get cleared
		// at the start of the next session (see start()).
	}

	async #setStatus(
		name: string | null,
		status: DebugState["status"],
	): Promise<void> {
		// Synchronous flip first, before any await. This is what makes
		// cleanupSession's idempotency check reliable - overlapping
		// callers see the new status before any of them yield.
		this.#debugState.name = name;
		this.#debugState.status = status;
		this.#pushDebugState(this.#debugState);
		await vscode.commands.executeCommand(
			"setContext",
			"gaffer.mode",
			contextValue(status),
		);
	}
}

// Internal status -> gaffer.mode context value. Idle and starting map
// to undefined (context unset) - the lens spinner is the indicator
// during starting; idle has no UI.
function contextValue(status: DebugState["status"]): string | undefined {
	switch (status) {
		case "running":
		case "inspecting":
		case "ended":
			return status;
		case "idle":
		case "starting":
			return undefined;
		default: {
			const _exhaustive: never = status;
			void _exhaustive;
			return undefined;
		}
	}
}
