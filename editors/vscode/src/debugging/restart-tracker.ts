import * as vscode from "vscode";
import type { StatusViewProvider } from "../panels/status.js";
import type { StateProvider } from "../panels/state.js";
import type { StepProvider } from "../panels/step.js";
import type { PhaseTracker } from "./phase-tracker.js";

// Watches the DAP wire for the `initialized` event. The first one is
// the normal startup signal and gets ignored; any subsequent one means
// the CLI emitted Initialized after a `restart` request - the editor's
// extension-owned panels (Step / State / Status) and PhaseTracker need
// to reset to a fresh-session state since VS Code's debug framework
// won't reset them for us. VS Code's own Variables / Watch / Call
// Stack views reset on this event automatically.
export class RestartTrackerFactory
	implements vscode.DebugAdapterTrackerFactory
{
	readonly #stepProvider: StepProvider;
	readonly #stateProvider: StateProvider;
	readonly #statusProvider: StatusViewProvider;
	readonly #phaseTracker: PhaseTracker;
	readonly #sessionName: () => string;

	constructor(deps: {
		stepProvider: StepProvider;
		stateProvider: StateProvider;
		statusProvider: StatusViewProvider;
		phaseTracker: PhaseTracker;
		sessionName: () => string;
	}) {
		this.#stepProvider = deps.stepProvider;
		this.#stateProvider = deps.stateProvider;
		this.#statusProvider = deps.statusProvider;
		this.#phaseTracker = deps.phaseTracker;
		this.#sessionName = deps.sessionName;
	}

	createDebugAdapterTracker(
		session: vscode.DebugSession,
	): vscode.DebugAdapterTracker | undefined {
		if (session.type !== "gaffer") return undefined;
		let seenInitialized = false;
		return {
			onDidSendMessage: (message: unknown) => {
				if (!isInitializedEvent(message)) return;
				if (!seenInitialized) {
					seenInitialized = true;
					return;
				}
				this.#stepProvider.clear();
				this.#stateProvider.clear();
				this.#statusProvider.reset(this.#sessionName());
				this.#phaseTracker.reset();
			},
		};
	}
}

function isInitializedEvent(message: unknown): boolean {
	if (typeof message !== "object" || message === null) return false;
	const m = message as { type?: unknown; event?: unknown };
	return m.type === "event" && m.event === "initialized";
}
