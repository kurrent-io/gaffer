// Opens a native diff editor for two versions of a projection, driving the
// history viewer's "diff previous" / "compare with local" actions. Unlike
// diff-projection.ts (deployed↔local over the warm LSP), these compare arbitrary
// versions by content hash, so they cold-spawn `gaffer diff --left --right --json`
// and render its canonical sources as read-only virtual documents.

import * as path from "node:path";
import * as vscode from "vscode";
import { log } from "../output.js";
import { parseDiffReport } from "./history-schema.js";
import type {
	HistoryContext,
	HistoryDiffRequest,
} from "../panels/history-view.js";

export const GAFFER_HISTORY_DIFF_SCHEME = "gaffer-history-diff";

// captureGafferCommand's result: stdout + exit code kept on any exit (diff exits
// non-zero when the versions differ, like a deploy dry-run, so the JSON is read
// regardless of code), or a spawn failure. Exit code 4 is "auth required".
export type DiffCapture =
	| { ok: true; stdout: string; code: number | null }
	| { ok: false; err: string };

const EXIT_AUTH = 4;

// Serves the two diff sides as read-only virtual documents, keyed by (env, name,
// ref) so re-running the same comparison reuses its documents and refreshes an
// open editor in place.
export class HistoryDiffContentProvider
	implements vscode.TextDocumentContentProvider, vscode.Disposable
{
	readonly #onDidChange = new vscode.EventEmitter<vscode.Uri>();
	readonly onDidChange = this.#onDidChange.event;
	readonly #contents = new Map<string, string>();

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
		this.#onDidChange.fire(l);
		this.#onDidChange.fire(r);
		return { left: l, right: r };
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
	// Cold-spawns `gaffer diff --left --right --json` in cwd and returns its capture.
	runDiff: (
		cwd: string,
		env: string,
		name: string,
		left: string,
		right: string,
	) => Promise<DiffCapture>;
	provider: HistoryDiffContentProvider;
}

export function openHistoryDiff(
	deps: HistoryDiffDeps,
): (ctx: HistoryContext, req: HistoryDiffRequest) => Promise<void> {
	return async (ctx, req) => {
		if (!vscode.workspace.isTrusted) return;

		const cwd = path.dirname(ctx.tomlUri.fsPath);
		const res = await deps.runDiff(cwd, req.env, req.name, req.left, req.right);
		if (!res.ok) {
			log(
				`history diff ${req.name} (${req.left}↔${req.right}) failed: ${res.err}`,
			);
			await vscode.window.showErrorMessage(
				`Couldn't diff "${req.name}": ${res.err}`,
			);
			return;
		}
		if (res.code === EXIT_AUTH) {
			await offerSignIn(req.name, ctx);
			return;
		}
		const report = parseDiffReport(res.stdout);
		if (!report) {
			log(`history diff ${req.name}: unparseable output (code ${res.code})`);
			await vscode.window.showErrorMessage(
				`Couldn't read the diff for "${req.name}".`,
			);
			return;
		}
		const { left, right } = deps.provider.setSides(
			req.env,
			req.name,
			req.left,
			report.left.source,
			req.right,
			report.right.source,
		);
		await vscode.commands.executeCommand("vscode.diff", left, right, req.title);
	};
}

async function offerSignIn(name: string, ctx: HistoryContext): Promise<void> {
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
}
