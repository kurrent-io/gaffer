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
	#renderTimer: NodeJS.Timeout | null = null;

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
		this.#postUpdate();
	}

	addProcessed(): void {
		this.#processed++;
		this.#scheduleUpdate();
	}

	addSkipped(): void {
		this.#skipped++;
		this.#scheduleUpdate();
	}

	addError(): void {
		this.#errors++;
		this.#scheduleUpdate();
	}

	#scheduleUpdate(): void {
		if (this.#renderTimer) return;
		this.#renderTimer = setTimeout(() => {
			this.#renderTimer = null;
			this.#postUpdate();
		}, 200);
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
		if (stats.length === 0) {
			stats.push("Waiting for events...");
		}

		const update: UpdateMessage = {
			type: "update",
			title: `Running ${this.#name || "projection"}...`,
			stats,
			showPauseButton: true,
		};
		void this.#view.webview.postMessage(update);
	}
}

function generateNonce(): string {
	return crypto.randomUUID().replaceAll("-", "");
}
