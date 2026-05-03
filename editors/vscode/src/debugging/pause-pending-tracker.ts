import * as vscode from "vscode";
import type { StatusViewProvider } from "../panels/status.js";

// Watches the DAP wire to drive the Status panel's "Waiting for event
// to pause" indicator. The pause click can come from the panel's own
// button, the debug toolbar, or F6 - all three funnel into a single
// outgoing `pause` request, so a tracker on the wire is the one
// chokepoint that catches them all without needing the CLI to echo
// state back over a custom event.
//
// Pending flips off when the engine actually stops (incoming DAP
// `stopped` event) - same trigger VS Code uses to render the resume
// button, so the indicator can't get out of sync with the toolbar.
export class PausePendingTrackerFactory
	implements vscode.DebugAdapterTrackerFactory
{
	readonly #statusProvider: StatusViewProvider;

	constructor(statusProvider: StatusViewProvider) {
		this.#statusProvider = statusProvider;
	}

	createDebugAdapterTracker(
		session: vscode.DebugSession,
	): vscode.DebugAdapterTracker | undefined {
		if (session.type !== "gaffer") return undefined;
		const status = this.#statusProvider;
		return {
			onWillReceiveMessage: (message: unknown) => {
				if (isDapRequest(message, "pause")) {
					status.setPausePending(true);
				}
			},
			onDidSendMessage: (message: unknown) => {
				if (isDapEvent(message, "stopped")) {
					status.setPausePending(false);
				}
			},
		};
	}
}

function isDapRequest(message: unknown, command: string): boolean {
	if (typeof message !== "object" || message === null) return false;
	const m = message as { type?: unknown; command?: unknown };
	return m.type === "request" && m.command === command;
}

function isDapEvent(message: unknown, event: string): boolean {
	if (typeof message !== "object" || message === null) return false;
	const m = message as { type?: unknown; event?: unknown };
	return m.type === "event" && m.event === event;
}
