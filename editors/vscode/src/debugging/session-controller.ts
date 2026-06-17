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
import type { InvokedVia } from "../discovery/cli.js";
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
} from "../notifications/debug.js";
import { showTrustWarning } from "../notifications/trust.js";
import { showAuthRequired } from "../notifications/auth.js";
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
	// When set, run against the named fixture from gaffer.toml's
	// [[projection.fixtures]]; the CLI resolves the name to a path.
	// Omit for a live KurrentDB run (default).
	fixture?: string;
	// When set (and no fixture), run live against this [env.<name>].
	// Omitted falls back to the gaffer.toml default env, matching the
	// CLI's own --env resolution. Mutually exclusive with fixture.
	env?: string;
}

export interface SessionControllerDeps {
	/** Builds the full CLI argv. `invokedVia` is appended via
	 * `--invoked-via=...` for telemetry-stitching at the CLI side. */
	buildArgv: (args: string[], invokedVia: InvokedVia) => string[];
	/** Env to hand to the spawned CLI, or `undefined` to inherit the
	 * extension host's `process.env`. Resolved at spawn time so a
	 * mid-session opt-out is honoured by the next `gaffer dev`/`debug`. */
	getSpawnEnv: () => NodeJS.ProcessEnv | undefined;
	stepProvider: StepProvider;
	stateProvider: StateProvider;
	statusProvider: StatusViewProvider;
	phaseTracker: PhaseTracker;
	// Called after every status transition with the new state. Lens
	// providers each hold their own copy and rerender via the state's
	// onDidChange. Push semantics (no shared mutable reference) - the
	// state value is the contract, not the object identity.
	pushDebugState: (state: Readonly<DebugState>) => void;
	// Reads the user's preferred DAP port (gaffer.debugPort). Returns
	// -1 when unset; the controller omits --debug-port in that case
	// and lets the CLI auto-pick a free port. Injected so the
	// controller stays decoupled from vscode.workspace.
	readDebugPort: () => number;
	// Factory for the underlying CLI-driving session. Production
	// passes createGafferSession; tests substitute a fake to avoid
	// spawning a real subprocess.
	createSession?: CreateSession;
}

export class SessionController implements vscode.Disposable {
	readonly #buildArgv: (args: string[], invokedVia: InvokedVia) => string[];
	readonly #getSpawnEnv: () => NodeJS.ProcessEnv | undefined;
	readonly #stepProvider: StepProvider;
	readonly #stateProvider: StateProvider;
	readonly #statusProvider: StatusViewProvider;
	readonly #phaseTracker: PhaseTracker;
	// Owned privately. External readers go through getDebugState which
	// returns a Readonly view. Mutations only inside #setStatus, which
	// is the single point that pushes to consumers via #pushDebugState.
	readonly #debugState: DebugState = { name: null, status: "idle" };
	readonly #pushDebugState: (state: Readonly<DebugState>) => void;
	readonly #readDebugPort: () => number;
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
	// Set by the auth_required listener so the exit handler doesn't also fire a
	// "Projection faulted" toast on top of the "Sign in" prompt - the non-zero
	// exit is the auth failure, already surfaced. Reset alongside #fatalErrorSeen.
	#authRequiredSeen = false;
	// Deferred awaited by start() so it doesn't return until the
	// onDidStartDebugSession-driven post-start work (setStatus + focus)
	// has completed. Set just before vscode.debug.startDebugging, read
	// by the listener, awaited after the call resolves. Necessary
	// because that post-start work moved into the listener (see
	// register()) - calling it inline after startDebugging lost the
	// focus race against VS Code's async panel-show layout pass.
	#startedReady: {
		promise: Promise<void>;
		resolve: () => void;
		reject: (err: unknown) => void;
	} | null = null;

	constructor(deps: SessionControllerDeps) {
		this.#buildArgv = deps.buildArgv;
		this.#getSpawnEnv = deps.getSpawnEnv;
		this.#stepProvider = deps.stepProvider;
		this.#stateProvider = deps.stateProvider;
		this.#statusProvider = deps.statusProvider;
		this.#phaseTracker = deps.phaseTracker;
		this.#pushDebugState = deps.pushDebugState;
		this.#readDebugPort = deps.readDebugPort;
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
					s.configuration.type !== "gaffer" ||
					this.#debugState.status !== "starting"
				) {
					return;
				}
				this.#activeDebugSession = s;
				// Post-start work runs here, not after the startDebugging
				// await in start(). VS Code fires this event after its
				// session-start layout pass completes, so a focus call
				// from inside the listener wins against the panel
				// container's last-tab focus (typically Terminal). Done
				// inline as an async IIFE so the listener registration
				// stays synchronous, with the deferred coordinating
				// completion back to start().
				void this.#runPostStart();
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

	async start(
		args: DebugProjectionArgs,
		invokedVia: InvokedVia,
	): Promise<void> {
		if (!vscode.workspace.isTrusted) {
			void showTrustWarning();
			return;
		}

		// Active session? Stop it before launching the new one. This
		// covers cross-projection clicks (Debug on B while A is running)
		// and per-fixture-lens switches mid-session - both used to
		// silently early-return. stop() routes through cleanupSession
		// which is idempotent w.r.t. the terminate listener, so the
		// listener firing concurrently is harmless.
		const status = this.#debugState.status;
		if (
			status === "starting" ||
			status === "running" ||
			status === "inspecting"
		) {
			log(
				`Stopping ${this.#debugState.name ?? "session"} (${status}) to start ${args.name}`,
			);
			await this.stop();
		}
		// stop() leaves us in idle (was starting) or ended (was running/
		// inspecting). The "ended" branch also catches user clicks after
		// a previous session ended on its own. We DON'T route through
		// cleanupSession("idle") here because that would flip
		// gaffer.mode to undefined for one frame, unmounting the whole
		// Gaffer panel container (no views match), and the user sees
		// the entire panel + tab vanish and reappear. Inline the
		// reset bits we actually need; setStatus("starting") below
		// takes us straight to the next session without touching
		// `idle`.
		if (this.#debugState.status === "ended") {
			this.#stepProvider.clear();
			this.#stateProvider.clear();
			this.#pendingEngineMode = null;
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
		this.#authRequiredSeen = false;

		const { name, tomlUri, fixture, env: envName } = args;
		const tomlDir = vscode.Uri.joinPath(tomlUri, "..").fsPath;
		// Port we ask the CLI to bind to. The CLI confirms via the debug
		// message and we attach using whatever it actually bound (below).
		// readDebugPort returns -1 when unset; we omit the flag in that
		// case and let the CLI auto-pick a free port.
		const requestedPort = this.#readDebugPort();
		// `--` terminates flag parsing before the positional projection
		// name; without it a hostile-toml projection named `--something`
		// would be parsed as a flag by the CLI. Flags go first, name
		// last after the separator.
		const argv = this.#buildArgv(
			[
				"dev",
				"--json",
				"--debug",
				...(requestedPort >= 0 ? ["--debug-port", String(requestedPort)] : []),
				...(fixture ? ["--fixture", fixture] : []),
				// fixture and env are mutually exclusive (a fixture is
				// offline); if both somehow arrive, the fixture wins, matching
				// the CLI, which resolves the fixture before any connection.
				...(envName && !fixture ? ["--env", envName] : []),
				// Start-paused is the extension's default UX: clicking Debug
				// lands the user in `inspecting` immediately so the State view
				// is populated and the user can explore before processing
				// begins. With breakpoints set the CLI runs to the first hit
				// instead.
				"--start-paused-if-no-breakpoints",
				"--",
				name,
			],
			invokedVia,
		);

		await this.#setStatus(name, "starting");
		log(`Starting: ${name}`);

		// Set up the post-start deferred up-front so EVERY exit path
		// from "starting" can settle it: the listener (happy path),
		// waitForDebug rejection, startDebugging returning false, the
		// session.on("exit") / fatal_error race during attach. Each
		// of those routes through #cleanupSession, which resolves
		// #startedReady. Without an up-front deferred, a race where
		// cleanup runs before we'd otherwise create the deferred
		// would leave start() awaiting a stale promise forever.
		const ready = createDeferred<void>();
		this.#startedReady = ready;

		const env = this.#getSpawnEnv();
		const session = this.#createSession(name, argv, {
			cwd: tomlDir,
			...(env !== undefined && { env }),
		});
		this.#activeSession = session;

		session.on("exit", async (msg) => {
			if (this.#activeSession !== session) return;
			const s = this.#debugState.status;
			// During starting: CLI died after debug message but before/during
			// attach (waitForDebug catch handles the pre-debug case). No
			// post-mortem to preserve - never reached running.
			if (s === "starting") {
				log(`CLI exited during start (code ${msg.code})`);
				if (!this.#fatalErrorSeen && !this.#authRequiredSeen) {
					await showStartFailure(`CLI exited (code ${msg.code})`);
				}
				await this.#cleanupSession("idle");
				return;
			}
			// During running/inspecting: route through ended cleanup so the
			// State view + Status counters survive for inspection.
			if (s === "running" || s === "inspecting") {
				log(`CLI exited with code ${msg.code}`);
				if (
					msg.code !== 0 &&
					!this.#fatalErrorSeen &&
					!this.#authRequiredSeen
				) {
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

		session.on("auth_required", (msg) => {
			// Set synchronously so the exit handler (which fires right after the
			// CLI exits non-zero) suppresses its own faulted toast.
			this.#authRequiredSeen = true;
			void this.#handleAuthRequired(msg.env, invokedVia);
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

		// Pre-focus the State view BEFORE startDebugging triggers VS
		// Code's panel-show. State is in the always-on group (visible
		// during starting/running/inspecting/ended) so it's already
		// mounted by the time we get here. By picking it as the panel
		// container's active tab now, we win the upcoming panel-show
		// race - VS Code surfaces the panel area with KurrentDB
		// Projections: State already active, no Terminal flash. The
		// listener fires setStatus + focus afterwards as the final
		// source of truth.
		await vscode.commands.executeCommand("gaffer.state.focus");

		const started = await vscode.debug.startDebugging(
			vscode.workspace.getWorkspaceFolder(tomlUri),
			{
				type: "gaffer",
				request: "attach",
				name,
				port: debugPort,
				localRoot: tomlDir,
				internalConsoleOptions: "neverOpen",
			},
		);

		if (!started) {
			log("Debug session failed to start");
			await this.#cleanupSession("idle");
			// cleanupSession settled the deferred; nothing to await.
			return;
		}

		// Wait for either the onDidStartDebugSession listener
		// (#runPostStart settles the deferred) or any cleanup path
		// (cleanupSession also settles). Either resolves cleanly.
		await ready.promise;
		this.#startedReady = null;
	}

	// Offers to sign in when a run reported auth_required, launching
	// `gaffer auth --env <env>` in a terminal. The terminal is a pty, so an
	// interactive keyring passphrase prompt works there; the spawn env carries
	// the same GAFFER_KEYRING_PASSWORD the run uses, so a sign-in stores a token
	// the run can later unlock.
	async #handleAuthRequired(
		env: string,
		invokedVia: InvokedVia,
	): Promise<void> {
		if (!(await showAuthRequired(env))) return;
		const [shellPath, ...shellArgs] = this.#buildArgv(
			["auth", "--env", env],
			invokedVia,
		);
		if (!shellPath) return;
		const spawnEnv = this.#getSpawnEnv();
		const terminal = vscode.window.createTerminal({
			name: `gaffer auth (${env})`,
			shellPath,
			shellArgs,
			...(spawnEnv ? { env: spawnEnv } : {}),
		});
		terminal.show();
	}

	// Called from the onDidStartDebugSession listener once VS Code has
	// reported the session is up. Applies any engine mode stashed during
	// `starting`, flips gaffer.mode (which makes the views visible), and
	// focuses the appropriate view. Errors propagate to start() via the
	// deferred so the caller doesn't hang.
	async #runPostStart(): Promise<void> {
		const ready = this.#startedReady;
		const name = this.#debugState.name;
		if (!ready || name === null) return;
		try {
			// Apply any engine mode that arrived during starting. With
			// #29a the CLI emits gaffer/mode=inspect immediately on
			// connect; that event landed during starting and got
			// stashed. Default to "running" if nothing was pending.
			const initial: EngineMode = this.#pendingEngineMode ?? "running";
			this.#pendingEngineMode = null;
			await this.#setStatus(name, initial);
			// Focus the State view, which is the only Gaffer view whose
			// when-clause covers every active mode (running, inspecting,
			// ended). Picking a mode-specific view (status / step)
			// races the when-clause propagation: setContext returns,
			// but the view isn't actually instantiated until VS Code's
			// next layout pass, so a focus call between the two no-ops
			// silently and Terminal keeps focus. Targeting an
			// always-visible view makes the focus always land.
			await vscode.commands.executeCommand("gaffer.state.focus");
			ready.resolve();
		} catch (err) {
			ready.reject(err);
		}
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

		// Settle any pending start() deferred. If cleanup runs while
		// start() is waiting on #runPostStart (CLI exited mid-attach,
		// fatal_error during starting, etc.), the listener gate
		// (status === "starting") will skip the post-start work and
		// the deferred would otherwise hang forever. Resolving here
		// lets start() unblock and return cleanly through this
		// cleanup path.
		if (this.#startedReady) {
			this.#startedReady.resolve();
			this.#startedReady = null;
		}

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

interface Deferred<T> {
	promise: Promise<T>;
	resolve: (value: T | PromiseLike<T>) => void;
	reject: (err: unknown) => void;
}

function createDeferred<T>(): Deferred<T> {
	let resolve!: (value: T | PromiseLike<T>) => void;
	let reject!: (err: unknown) => void;
	const promise = new Promise<T>((res, rej) => {
		resolve = res;
		reject = rej;
	});
	return { promise, resolve, reject };
}

// Internal status -> gaffer.mode context value. Idle is the only
// status that maps to undefined (no session, all views hidden).
// "starting" emits its own value so the State + Status views stay
// mounted across the boot phase - otherwise they'd unmount when
// the session goes ended -> idle -> starting and remount when
// starting -> running, which the user sees as a panel flicker.
function contextValue(status: DebugState["status"]): string | undefined {
	switch (status) {
		case "running":
		case "inspecting":
		case "ended":
		case "starting":
			return status;
		case "idle":
			return undefined;
		default: {
			const _exhaustive: never = status;
			void _exhaustive;
			return undefined;
		}
	}
}
