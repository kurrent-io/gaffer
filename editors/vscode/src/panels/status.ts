// Webview that shows running counters during a debug session: events
// processed, errors, plus a "Pause at next event" button. Phase /
// catch-up state is owned by PhaseTracker and pushed in via
// setDescription; this provider is just stats + mode.
//
// The UI is a Solid bundle (src/webviews/status) loaded via the shared
// webview shell. Rendered once on resolveWebviewView; subsequent updates
// are posted through `webview.postMessage` and Solid re-renders reactively.
// This provider owns all state and computes the message shape; the webview
// only renders it.

import * as vscode from "vscode";
import type { StatusUpdateMessage } from "../webviews/status/protocol.js";
import { type Phase, PHASE_LABELS } from "../debugging/phase-tracker.js";
import { webviewHtml, webviewRoots } from "./webview-shell.js";

export class StatusViewProvider implements vscode.WebviewViewProvider {
	readonly #extensionUri: vscode.Uri;
	readonly #reportError: ((msg: unknown) => void) | undefined;
	#view: vscode.WebviewView | null = null;
	#name = "";
	#processed = 0;
	#errors = 0;
	#quirks = 0;
	#skipped = 0;
	#skippedByReason: Readonly<Record<string, number>> = {};
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
	// Reason a run failed (a run_error from the CLI). When set it takes
	// precedence over the phase label in the description chip, so the user sees
	// "why" the run stopped rather than a bare "Disconnected". Cleared on reset.
	#errorReason: string | null = null;

	constructor(extensionUri: vscode.Uri, reportError?: (msg: unknown) => void) {
		this.#extensionUri = extensionUri;
		this.#reportError = reportError;
	}

	resolveWebviewView(webviewView: vscode.WebviewView): void {
		this.#view = webviewView;
		webviewView.webview.options = {
			enableScripts: true,
			localResourceRoots: webviewRoots(this.#extensionUri),
		};
		webviewView.description = PHASE_LABELS[this.#phase];

		webviewView.webview.html = webviewHtml(
			webviewView.webview,
			this.#extensionUri,
			"status",
		);

		webviewView.webview.onDidReceiveMessage((msg: { command?: string }) => {
			if (msg.command === "pause") {
				void vscode.commands.executeCommand("workbench.action.debug.pause");
				return;
			}
			if (msg.command === "error") {
				this.#reportError?.(msg);
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

	// Records why a run failed. Shown as a distinct error state in the panel
	// body (the full reason, not truncated like the header chip would), and
	// carried in the toast. Persists until the next reset.
	setError(reason: string): void {
		this.#errorReason = reason;
		this.#postUpdate();
	}

	reset(name: string): void {
		this.#name = name;
		this.#processed = 0;
		this.#errors = 0;
		this.#quirks = 0;
		this.#skipped = 0;
		this.#skippedByReason = {};
		this.#mode = "running";
		this.#pausePending = false;
		this.#phase = "connecting";
		this.#errorReason = null;
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
	setStats(processed: number, errors: number, quirks = 0): void {
		if (
			this.#processed === processed &&
			this.#errors === errors &&
			this.#quirks === quirks
		) {
			return;
		}
		this.#processed = processed;
		this.#errors = errors;
		this.#quirks = quirks;
		this.#postUpdate();
	}

	// Skipped events are surfaced only in fixture mode (the CLI omits
	// the fields in live mode). In live runs the user is watching a real
	// stream and engine-level drops are noise; in fixture mode every
	// drop is a fixture authoring problem worth flagging.
	setSkipped(count: number, byReason: Readonly<Record<string, number>>): void {
		if (
			this.#skipped === count &&
			recordsEqual(this.#skippedByReason, byReason)
		) {
			return;
		}
		this.#skipped = count;
		this.#skippedByReason = { ...byReason };
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
		if (this.#quirks > 0) {
			stats.push(
				`${this.#quirks.toLocaleString()} ${this.#quirks === 1 ? "quirk" : "quirks"}`,
			);
		}
		if (this.#skipped > 0) {
			stats.push(formatSkipped(this.#skipped, this.#skippedByReason));
		}
		if (stats.length === 0 && this.#mode === "running") {
			stats.push(connecting ? "Connecting..." : "Waiting for events...");
		}

		const name = this.#name || "projection";
		// A failed run takes a failure title regardless of mode; the icon is
		// rendered alongside it in the webview.
		const title = this.#errorReason
			? `${name} failed`
			: this.#mode === "ended"
				? `Finished ${name}`
				: `Running ${name}...`;
		const update: StatusUpdateMessage =
			this.#mode === "ended"
				? {
						type: "update",
						mode: "ended",
						title,
						stats,
						showPauseButton: false,
						pauseButtonLabel: "Pause at next event",
						pauseButtonDisabled: false,
						error: this.#errorReason,
					}
				: {
						type: "update",
						mode: "running",
						title,
						stats,
						showPauseButton: !connecting && !stale,
						pauseButtonLabel: this.#pausePending
							? "Waiting for event to pause"
							: "Pause at next event",
						pauseButtonDisabled: this.#pausePending,
						error: this.#errorReason,
					};
		void this.#view.webview.postMessage(update);
	}
}

// "5 skipped (3 wrong-stream, 2 no-handler)" - top-3 reasons by
// count, with "+N more" if there are additional categories. Mirrors
// the CLI text writer's per-reason breakdown so the editor and the
// terminal show the same shape.
function formatSkipped(
	count: number,
	byReason: Readonly<Record<string, number>>,
): string {
	const total = `${count.toLocaleString()} skipped`;
	const entries = Object.entries(byReason)
		.filter(([, n]) => n > 0)
		.sort(([, a], [, b]) => b - a);
	if (entries.length === 0) return total;
	const TOP = 3;
	const top = entries.slice(0, TOP).map(([r, n]) => `${n} ${r}`);
	const overflow = entries.length - TOP;
	const parts = overflow > 0 ? [...top, `+${overflow} more`] : top;
	return `${total} (${parts.join(", ")})`;
}

function recordsEqual(
	a: Readonly<Record<string, number>>,
	b: Readonly<Record<string, number>>,
): boolean {
	const ak = Object.keys(a);
	const bk = Object.keys(b);
	if (ak.length !== bk.length) return false;
	for (const k of ak) if (a[k] !== b[k]) return false;
	return true;
}
