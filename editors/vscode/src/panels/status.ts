// Webview that shows running counters during a debug session: events
// processed, errors, plus a "Pause at next event" button. Phase /
// catch-up state is owned by PhaseTracker and pushed in via
// setDescription; this provider is just stats + mode.
//
// HTML lives in status.html (loaded as a raw string at build time).
// Rendered once on resolveWebviewView; subsequent updates are posted
// through `webview.postMessage` and the inline script patches the DOM.
// Avoids the focus-drop / state-reset that came from reassigning
// `webview.html` on every counter tick.
//
// CSP locked down to the loaded HTML's nonce and the webview's
// cspSource for styles. `localResourceRoots: []` since the template is
// fully self-contained.

import * as vscode from "vscode";
import { type Phase, PHASE_LABELS } from "../debugging/phase-tracker.js";
import statusTemplate from "./status.html?raw";

interface UpdateMessage {
	type: "update";
	mode: "running" | "ended";
	title: string;
	stats: string[];
	showPauseButton: boolean;
	pauseButtonLabel: string;
	pauseButtonDisabled: boolean;
}

export class StatusViewProvider implements vscode.WebviewViewProvider {
	#view: vscode.WebviewView | null = null;
	#name = "";
	#processed = 0;
	#errors = 0;
	// Stored on the provider so that view reconstruction (when VS Code
	// re-shows after the visibility when-clause flips) re-renders with
	// the right mode. The webview instance is recreated on re-show; the
	// provider is the singleton that remembers state across.
	#mode: "running" | "ended" = "running";
	// True between the user requesting a pause (via this panel's
	// button, the debug toolbar, or F6) and the engine actually
	// stopping on the next event. When caught up, no events arrive,
	// so the click can sit unresolved indefinitely - we surface the
	// in-flight state so the button isn't a black hole. Driven by
	// PausePendingTrackerFactory tapping the DAP wire.
	#pausePending = false;
	// Latest phase from PhaseTracker. While connecting, the engine
	// hasn't started talking to us yet - "Pause at next event" would
	// do nothing, so we hide it, and the stats placeholder reads
	// "Connecting..." instead of "Waiting for events...".
	#phase: Phase = "connecting";

	resolveWebviewView(webviewView: vscode.WebviewView): void {
		this.#view = webviewView;
		webviewView.webview.options = {
			enableScripts: true,
			localResourceRoots: [],
		};
		webviewView.description = PHASE_LABELS[this.#phase];

		const nonce = generateNonce();
		webviewView.webview.html = statusTemplate
			.replaceAll("{{NONCE}}", nonce)
			.replaceAll("{{CSP_SOURCE}}", webviewView.webview.cspSource);

		webviewView.webview.onDidReceiveMessage((msg: { command?: string }) => {
			if (msg.command === "pause") {
				void vscode.commands.executeCommand("workbench.action.debug.pause");
			}
		});

		webviewView.onDidDispose(() => {
			this.#view = null;
		});

		this.#postUpdate();
	}

	// Called by PhaseTracker. Drives both the description chip
	// (re-applied on every resolveWebviewView so it survives panel
	// switches) and the in-panel UI (Connecting placeholder / pause
	// button visibility).
	setPhase(phase: Phase): void {
		if (this.#phase === phase) return;
		this.#phase = phase;
		if (this.#view) this.#view.description = PHASE_LABELS[phase];
		this.#postUpdate();
	}

	reset(name: string): void {
		this.#name = name;
		this.#processed = 0;
		this.#errors = 0;
		this.#mode = "running";
		this.#pausePending = false;
		this.#phase = "connecting";
		this.#postUpdate();
	}

	markEnded(): void {
		this.#mode = "ended";
		this.#pausePending = false;
		this.#postUpdate();
	}

	setPausePending(pending: boolean): void {
		if (this.#pausePending === pending) return;
		this.#pausePending = pending;
		this.#postUpdate();
	}

	// Cumulative replace, fed by the CLI's gaffer/stats DAP event.
	// The CLI throttles its emit cadence so a 200ms render coalesce
	// here is unnecessary - by the time setStats fires the values are
	// already at most 100ms behind the engine.
	//
	// Skipped events are intentionally not surfaced: they're noise from
	// the user's perspective ("we couldn't filter this on the server but
	// got it and didn't want it"). Tracked internally on the CLI side
	// for future verbose/debug surfaces.
	setStats(processed: number, errors: number): void {
		if (this.#processed === processed && this.#errors === errors) return;
		this.#processed = processed;
		this.#errors = errors;
		this.#postUpdate();
	}

	#postUpdate(): void {
		if (!this.#view) return;
		const connecting = this.#mode === "running" && this.#phase === "connecting";
		// Defensive: phase=disconnected with mode=running shouldn't be
		// reachable (idle cleanup ends the phase tracker but hides the
		// panel via the gaffer.mode when-clause), but if we ever land
		// here with a phantom session we don't want a clickable pause
		// button on a dead DAP socket.
		const stale = this.#mode === "running" && this.#phase === "disconnected";
		const stats: string[] = [];
		if (this.#processed > 0) {
			stats.push(`${this.#processed.toLocaleString()} events processed`);
		}
		if (this.#errors > 0) {
			stats.push(`${this.#errors.toLocaleString()} errors`);
		}
		if (stats.length === 0 && this.#mode === "running") {
			stats.push(connecting ? "Connecting..." : "Waiting for events...");
		}

		const name = this.#name || "projection";
		const update: UpdateMessage =
			this.#mode === "ended"
				? {
						type: "update",
						mode: "ended",
						title: `Finished ${name}`,
						stats,
						showPauseButton: false,
						pauseButtonLabel: "Pause at next event",
						pauseButtonDisabled: false,
					}
				: {
						type: "update",
						mode: "running",
						title: `Running ${name}...`,
						stats,
						showPauseButton: !connecting && !stale,
						pauseButtonLabel: this.#pausePending
							? "Waiting for event to pause"
							: "Pause at next event",
						pauseButtonDisabled: this.#pausePending,
					};
		void this.#view.webview.postMessage(update);
	}
}

function generateNonce(): string {
	return crypto.randomUUID().replaceAll("-", "");
}
