// Webview that shows running counters during a debug session: events
// processed, skipped, errors, plus a "Pause to inspect" button.
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
import statusTemplate from "./status.html?raw";

interface UpdateMessage {
	type: "update";
	mode: "running" | "ended";
	title: string;
	stats: string[];
	showPauseButton: boolean;
}

export class StatusViewProvider implements vscode.WebviewViewProvider {
	#view: vscode.WebviewView | null = null;
	#name = "";
	#processed = 0;
	#skipped = 0;
	#errors = 0;
	// Stored on the provider so that view reconstruction (when VS Code
	// re-shows after the visibility when-clause flips) re-renders with
	// the right mode. The webview instance is recreated on re-show; the
	// provider is the singleton that remembers state across.
	#mode: "running" | "ended" = "running";

	resolveWebviewView(webviewView: vscode.WebviewView): void {
		this.#view = webviewView;
		webviewView.webview.options = {
			enableScripts: true,
			localResourceRoots: [],
		};

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

	reset(name: string): void {
		this.#name = name;
		this.#processed = 0;
		this.#skipped = 0;
		this.#errors = 0;
		this.#mode = "running";
		this.#postUpdate();
	}

	markEnded(): void {
		this.#mode = "ended";
		this.#postUpdate();
	}

	// Cumulative replace, fed by the CLI's gaffer/stats DAP event.
	// The CLI throttles its emit cadence so a 200ms render coalesce
	// here is unnecessary - by the time setStats fires the values are
	// already at most 100ms behind the engine.
	setStats(processed: number, skipped: number, errors: number): void {
		if (
			this.#processed === processed &&
			this.#skipped === skipped &&
			this.#errors === errors
		) {
			return;
		}
		this.#processed = processed;
		this.#skipped = skipped;
		this.#errors = errors;
		this.#postUpdate();
	}

	#postUpdate(): void {
		if (!this.#view) return;
		const stats: string[] = [];
		if (this.#processed > 0) {
			stats.push(`${this.#processed.toLocaleString()} events processed`);
		}
		if (this.#skipped > 0) {
			stats.push(`${this.#skipped.toLocaleString()} events skipped`);
		}
		if (this.#errors > 0) {
			stats.push(`${this.#errors.toLocaleString()} errors`);
		}
		if (stats.length === 0 && this.#mode === "running") {
			stats.push("Waiting for events...");
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
					}
				: {
						type: "update",
						mode: "running",
						title: `Running ${name}...`,
						stats,
						showPauseButton: true,
					};
		void this.#view.webview.postMessage(update);
	}
}

function generateNonce(): string {
	return crypto.randomUUID().replaceAll("-", "");
}
