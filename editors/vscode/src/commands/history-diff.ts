// Opens a native diff editor for two versions of a projection, driving the
// history viewer's "diff previous" / "compare with local" actions. Like
// diff-projection.ts, the diff is served over the language server's warm per-env
// connection (gaffer/diffVersions) rather than a cold `gaffer diff` spawn, so a
// re-diff is one RPC. Unlike diffProjection it compares arbitrary versions by
// content hash. Both sides render as read-only virtual documents.

import * as vscode from "vscode";
import { log } from "../output.js";
import { type ProjectionDiff } from "../lsp/diff.js";
import { LspAuthRequiredError } from "../lsp/request.js";
import type {
	HistoryContext,
	HistoryDiffRequest,
} from "../panels/history-view.js";

export const GAFFER_HISTORY_DIFF_SCHEME = "gaffer-history-diff";

// Serves the two diff sides as read-only virtual documents, keyed by (env, name,
// ref) so re-running the same comparison reuses its documents and refreshes an
// open editor in place.
export class HistoryDiffContentProvider
	implements vscode.TextDocumentContentProvider, vscode.Disposable
{
	readonly #onDidChange = new vscode.EventEmitter<vscode.Uri>();
	readonly onDidChange = this.#onDidChange.event;
	readonly #contents = new Map<string, string>();

	// Bound the cache so a long session diffing many versions doesn't grow it
	// without limit. Keys are per-side, so this holds the sources for the most
	// recent ~60 comparisons; evicting the oldest by insertion can only blank a
	// diff editor left open past that many later comparisons, which doesn't happen
	// in practice.
	static readonly #maxEntries = 120;

	dispose(): void {
		this.#onDidChange.dispose();
		this.#contents.clear();
	}

	provideTextDocumentContent(uri: vscode.Uri): string {
		return this.#contents.get(uri.toString()) ?? "";
	}

	setSides(
		env: string,
		name: string,
		left: string,
		leftSource: string,
		right: string,
		rightSource: string,
	): { left: vscode.Uri; right: vscode.Uri } {
		const l = this.#uri(env, name, left);
		const r = this.#uri(env, name, right);
		this.#contents.set(l.toString(), leftSource);
		this.#contents.set(r.toString(), rightSource);
		this.#evictOldest();
		this.#onDidChange.fire(l);
		this.#onDidChange.fire(r);
		return { left: l, right: r };
	}

	// Drop the oldest-inserted entries once over the cap. A Map iterates in
	// insertion order, so its first keys are the oldest.
	#evictOldest(): void {
		while (this.#contents.size > HistoryDiffContentProvider.#maxEntries) {
			const oldest = this.#contents.keys().next().value;
			if (oldest === undefined) break;
			this.#contents.delete(oldest);
		}
	}

	// The path ends in `.js` so VS Code syntax-highlights both sides (projection
	// sources are JS today); env/name/ref are separate encoded segments so a
	// special char in any can't collide two distinct comparisons.
	#uri(env: string, name: string, ref: string): vscode.Uri {
		return vscode.Uri.from({
			scheme: GAFFER_HISTORY_DIFF_SCHEME,
			path: `/${encodeURIComponent(env)}/${encodeURIComponent(name)}/${encodeURIComponent(ref)}.js`,
		});
	}
}

export interface HistoryDiffDeps {
	// Fetches the two versions' diff over the LSP warm connection. Throws
	// LspAuthRequiredError when the env needs sign-in, LspUnavailableError
	// otherwise. A field so tests inject a fake in place of a live server.
	requestDiff: (
		name: string,
		tomlUri: vscode.Uri,
		env: string,
		left: string,
		right: string,
	) => Promise<ProjectionDiff>;
	provider: HistoryDiffContentProvider;
}

export function openHistoryDiff(
	deps: HistoryDiffDeps,
): (ctx: HistoryContext, req: HistoryDiffRequest) => Promise<void> {
	return async (ctx, req) => {
		if (!vscode.workspace.isTrusted) return;

		let diff: ProjectionDiff;
		try {
			diff = await deps.requestDiff(
				req.name,
				ctx.tomlUri,
				req.env,
				req.left,
				req.right,
			);
		} catch (err) {
			await reportFailure(req.name, ctx, err);
			return;
		}

		const { left, right } = deps.provider.setSides(
			req.env,
			req.name,
			req.left,
			diff.left.source,
			req.right,
			diff.right.source,
		);
		await vscode.commands.executeCommand("vscode.diff", left, right, req.title);
	};
}

async function reportFailure(
	name: string,
	ctx: HistoryContext,
	err: unknown,
): Promise<void> {
	if (err instanceof LspAuthRequiredError) {
		const signIn = "Sign in";
		const choice = await vscode.window.showErrorMessage(
			`${ctx.env} needs sign-in to diff "${name}".`,
			signIn,
		);
		if (choice === signIn) {
			await vscode.commands.executeCommand("gaffer.signIn", {
				env: ctx.env,
				tomlUri: ctx.tomlUri,
			});
		}
		return;
	}
	const detail = err instanceof Error ? err.message : String(err);
	log(`history diff ${name} --env ${ctx.env} failed: ${detail}`);
	await vscode.window.showErrorMessage(`Couldn't diff "${name}": ${detail}`);
}
