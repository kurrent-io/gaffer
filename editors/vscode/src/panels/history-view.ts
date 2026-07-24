// Editor-area webview that renders a projection's deploy history as a timeline,
// newest first: the ledger from `gaffer history --json`, drawn with the same
// grammar the CLI uses - a run-state node per version (enabled/disabled/deleted),
// a run-state-tinted spine, revert brackets as lanes with a dotted bridge, and a
// recreate as a terminus. Each content version's hover actions open a native diff
// (this version vs the previous, or vs local) or roll back to it.
//
// One reusable panel, mirroring DeployPlanView: a re-open reveals and re-renders
// the existing tab. The UI is a Solid bundle (src/webviews/history) loaded via
// the shared webview shell; rendered once, then driven by postMessage. The
// classify->collapse->graph pipeline runs host-side in the one tested model
// (panels/history-graph.ts), and the host posts the collapsed rows + lane
// layout so the webview only renders.

import * as vscode from "vscode";
import type { HistoryEntry } from "../commands/history-schema.js";
import type { HistoryInbound } from "../webviews/history/protocol.js";
import type { WebviewErrorMessage } from "../webviews/shared/webview-error-message.js";
import { collapseHistory, computeHistoryGraph } from "./history-graph.js";
import { webviewHtml, webviewRoots } from "./webview-shell.js";

export interface HistoryContext {
	env: string;
	tomlUri: vscode.Uri;
	name: string;
	// Picks the rollback confirm tier; undefined when the env's production status
	// isn't known yet (the confirm then fails safe to a modal).
	production: boolean | undefined;
}

// A diff to open in the native editor: two refs the CLI understands (a content
// hash, or "local") and a tab title.
export interface HistoryDiffRequest {
	name: string;
	env: string;
	left: string;
	right: string;
	title: string;
}

// Extension -> webview. `history` (re)renders the timeline; the rollback messages
// settle one row's state as an in-flight rollback runs. The shape is the shared
// contract the webview reads (see webviews/history/protocol.ts).
export type HistoryMessage = HistoryInbound;

export type HistorySend = (message: HistoryMessage) => void;

export interface HistoryViewHandlers {
	// A row's "diff previous" / "compare with local" action, resolved to two refs.
	onDiff: (ctx: HistoryContext, req: HistoryDiffRequest) => void;
	// A row's "rollback" action: the target's full content hash. `send` streams the
	// outcome back so the panel can settle the row and refresh. Returns a promise so
	// the panel can release its in-flight guard if the handler throws before it
	// settles (it otherwise settles via `send`).
	onRollback: (
		ctx: HistoryContext,
		target: { version: number; hash: string },
		send: HistorySend,
	) => Promise<void>;
}

export class HistoryView implements vscode.Disposable {
	#panel: vscode.WebviewPanel | undefined;
	#ctx: HistoryContext | undefined;
	#entries: HistoryEntry[] = [];
	// True while a rollback runs, guarding against a second one and against a
	// refresh landing mid-rollback.
	#rollingBack = false;
	// Bumped on every render; a rollback click echoes the token it was shown
	// against, so a click against a since-refreshed timeline is dropped.
	#token = 0;
	readonly #extensionUri: vscode.Uri;
	readonly #handlers: HistoryViewHandlers;
	readonly #reportError: ((msg: WebviewErrorMessage) => void) | undefined;

	constructor(
		extensionUri: vscode.Uri,
		handlers: HistoryViewHandlers,
		reportError?: (msg: WebviewErrorMessage) => void,
	) {
		this.#extensionUri = extensionUri;
		this.#handlers = handlers;
		this.#reportError = reportError;
	}

	// Show (or refresh) the timeline for one projection on one env. Creates the
	// panel on first use, else reveals and re-renders it. A refresh landing while a
	// rollback streams is dropped rather than wiping the in-flight state.
	show(entries: HistoryEntry[], ctx: HistoryContext): void {
		if (this.#rollingBack) return;
		this.#ctx = ctx;
		// Run the CLI's classify->collapse->graph pipeline here, in the one tested
		// model (panels/history-graph.ts), and post the collapsed rows + lane layout.
		// The webview then only renders - no duplicated graph logic - and the
		// diff/rollback lookups resolve against the same collapsed list the user sees.
		const rows = collapseHistory(entries);
		const graph = computeHistoryGraph(rows);
		this.#entries = rows;
		if (!this.#panel) {
			this.#panel = vscode.window.createWebviewPanel(
				"gaffer.history",
				title(ctx),
				{ viewColumn: vscode.ViewColumn.Active, preserveFocus: false },
				{
					enableScripts: true,
					retainContextWhenHidden: true,
					localResourceRoots: webviewRoots(this.#extensionUri),
				},
			);
			this.#panel.webview.html = webviewHtml(
				this.#panel.webview,
				this.#extensionUri,
				"history",
			);
			this.#panel.webview.onDidReceiveMessage((msg: unknown) => {
				this.#handleMessage(msg);
			});
			this.#panel.onDidDispose(() => {
				this.#panel = undefined;
				this.#ctx = undefined;
				this.#entries = [];
				this.#rollingBack = false;
			});
		}
		this.#token += 1;
		this.#panel.title = title(ctx);
		this.#panel.reveal(this.#panel.viewColumn);
		this.#post({
			type: "history",
			name: ctx.name,
			env: ctx.env,
			entries: rows,
			graph,
			token: this.#token,
		});
	}

	// reportError surfaces a load failure in an already-open panel (e.g. a refresh
	// after a rollback that couldn't re-read). No panel means nothing to update.
	reportError(message: string): void {
		this.#post({ type: "error", message });
	}

	#handleMessage(msg: unknown): void {
		if (typeof msg !== "object" || msg === null) return;
		const command = (msg as { command?: unknown }).command;
		if (command === "error") {
			this.#reportError?.(msg as WebviewErrorMessage);
			return;
		}
		if (!this.#ctx) return;
		if (command === "diff") {
			this.#onDiff(msg);
			return;
		}
		if (command === "rollback") {
			this.#onRollback(msg);
		}
	}

	#onDiff(msg: unknown): void {
		if (!this.#ctx) return;
		const version = (msg as { version?: unknown }).version;
		const framing = (msg as { framing?: unknown }).framing;
		if (typeof version !== "number") return;
		const entry = this.#entries.find((e) => e.version === version);
		if (!entry || !entry.contentHash) return;
		if (framing === "local") {
			this.#handlers.onDiff(this.#ctx, {
				name: this.#ctx.name,
				env: this.#ctx.env,
				left: entry.contentHash,
				right: "local",
				title: `${this.#ctx.name}: v${version} ↔ local`,
			});
			return;
		}
		if (framing === "previous") {
			const base = this.#previousContent(version);
			if (!base) {
				void vscode.window.showInformationMessage(
					`v${version} is the first version of "${this.#ctx.name}" - nothing before it to compare.`,
				);
				return;
			}
			this.#handlers.onDiff(this.#ctx, {
				name: this.#ctx.name,
				env: this.#ctx.env,
				left: base.contentHash,
				right: entry.contentHash,
				title: `${this.#ctx.name}: v${base.version} ↔ v${version}`,
			});
		}
	}

	// The nearest older entry carrying a content hash - the version this one
	// changed from. State changes and the tombstone carry no content, so they're
	// skipped.
	#previousContent(version: number): HistoryEntry | undefined {
		const i = this.#entries.findIndex((e) => e.version === version);
		if (i < 0) return undefined;
		for (let j = i + 1; j < this.#entries.length; j++) {
			const e = this.#entries[j];
			if (e && e.contentHash) return e;
		}
		return undefined;
	}

	#onRollback(msg: unknown): void {
		if (!this.#ctx) return;
		if (this.#rollingBack) return;
		if ((msg as { token?: unknown }).token !== this.#token) return;
		const version = (msg as { version?: unknown }).version;
		if (typeof version !== "number") return;
		const entry = this.#entries.find((e) => e.version === version);
		if (!entry || !entry.contentHash) return;
		this.#rollingBack = true;
		// The handler settles the guard via `send`; if it throws before doing so,
		// release the guard here so the panel doesn't freeze (no further rollback,
		// and every refresh early-returns while rolling back).
		void this.#handlers
			.onRollback(this.#ctx, { version, hash: entry.contentHash }, (m) =>
				this.#sendRollback(m),
			)
			.catch(() => {
				this.#sendRollback({
					type: "rollback-error",
					version,
					message: "",
				});
			});
	}

	// Streams a rollback message to the webview, releasing the in-flight guard once
	// it settles so a further rollback or refresh can run.
	#sendRollback(message: HistoryMessage): void {
		if (message.type === "rollback-done" || message.type === "rollback-error") {
			this.#rollingBack = false;
		}
		this.#post(message);
	}

	#post(message: HistoryMessage): void {
		void this.#panel?.webview.postMessage(message);
	}

	dispose(): void {
		this.#panel?.dispose();
		this.#panel = undefined;
		this.#rollingBack = false;
	}
}

function title(ctx: HistoryContext): string {
	return `History: ${ctx.name}`;
}
