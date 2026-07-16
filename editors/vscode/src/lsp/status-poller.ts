import * as vscode from "vscode";

// How often to poll deploy status for visible gaffer.toml editors. The server
// makes each poll cheap - it reuses the cached drift verdict and refreshes only
// live runtime state (see cli/internal/lsp/status.go), and drops a poll that
// lands within its own throttle window (pollThrottleWindow, 3s). This interval
// must stay comfortably above that window so a scheduled tick is never throttled.
export const STATUS_POLL_INTERVAL_MS = 5_000;

// A gaffer.toml on disk - the only document whose deploy status is worth polling.
function isGafferConfig(uri: vscode.Uri): boolean {
	return uri.scheme === "file" && uri.path.split("/").pop() === "gaffer.toml";
}

/**
 * Polls the LSP server for fresh deploy status on a timer while a gaffer.toml is
 * visible, so the per-projection badges track live runtime state (a projection
 * stopping, faulting, catching up) without the user re-opening or saving.
 *
 * Scope and safety: it refreshes only the currently visible gaffer.toml editors,
 * and the timer only runs while at least one is visible - closing the last one
 * stops it. The server owns the "is this cheap?" decision (a poll reuses the
 * cached drift verdict and reads only runtime), so this side just picks when.
 * A future non-VS-Code client gets the same freshness by calling the same
 * `gaffer/refreshStatus` notification on its own timer.
 */
export class StatusPoller implements vscode.Disposable {
	#timer: ReturnType<typeof setInterval> | undefined;
	readonly #sub: vscode.Disposable;
	readonly #refresh: (uri: vscode.Uri) => void;
	readonly #intervalMs: number;

	constructor(
		refresh: (uri: vscode.Uri) => void,
		intervalMs: number = STATUS_POLL_INTERVAL_MS,
	) {
		this.#refresh = refresh;
		this.#intervalMs = intervalMs;
		this.#sub = vscode.window.onDidChangeVisibleTextEditors(() =>
			this.#reconcile(),
		);
		this.#reconcile();
	}

	// Start the timer when a gaffer.toml becomes visible, stop it when the last
	// one goes away, so an idle window (no config in view) runs no timer.
	#reconcile(): void {
		const shouldRun = this.#visibleConfigs().length > 0;
		if (shouldRun && this.#timer === undefined) {
			this.#timer = setInterval(() => this.#tick(), this.#intervalMs);
		} else if (!shouldRun && this.#timer !== undefined) {
			clearInterval(this.#timer);
			this.#timer = undefined;
		}
	}

	#tick(): void {
		for (const doc of this.#visibleConfigs()) {
			// Skip a config with unsaved edits: the server reads the in-memory
			// buffer, and a poll landing mid-edit could read a transiently
			// unparseable state and clear the status. A save fires its own refresh
			// and the next tick resumes polling.
			if (doc.isDirty) continue;
			this.#refresh(doc.uri);
		}
	}

	// The distinct gaffer.toml documents across all visible editors (a split view
	// shows the same document in two editors; count it once).
	#visibleConfigs(): vscode.TextDocument[] {
		const seen = new Set<string>();
		const out: vscode.TextDocument[] = [];
		for (const editor of vscode.window.visibleTextEditors) {
			const doc = editor.document;
			if (!isGafferConfig(doc.uri)) continue;
			const key = doc.uri.toString();
			if (seen.has(key)) continue;
			seen.add(key);
			out.push(doc);
		}
		return out;
	}

	dispose(): void {
		this.#sub.dispose();
		if (this.#timer !== undefined) {
			clearInterval(this.#timer);
			this.#timer = undefined;
		}
	}
}
