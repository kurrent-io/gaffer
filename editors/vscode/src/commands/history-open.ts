// gaffer.history: opens the history viewer for one projection on one env, from
// the per-projection action menu. Cold-spawns `gaffer history --json`, parses the
// ledger, and shows it in the HistoryView panel. The same loader refreshes the
// panel after a rollback.

import * as path from "node:path";
import * as vscode from "vscode";
import { log } from "../output.js";
import { parseHistoryReport } from "./history-schema.js";
import type { HistoryContext, HistoryView } from "../panels/history-view.js";

// The outcome of a `gaffer history --json` spawn, classified by the wiring:
// stdout on a clean exit, or a failure split into auth (exit 4, needs sign-in)
// vs a reason (the stderr message). `history` is a plain read - a non-zero exit
// is a real error, not a status code with a payload - so its reason must be kept
// rather than parsed away as empty JSON.
export type HistoryOutcome =
	| { ok: true; stdout: string }
	| { ok: false; auth: boolean; reason: string };

export interface HistoryOpenDeps {
	runHistory: (
		cwd: string,
		env: string,
		name: string,
	) => Promise<HistoryOutcome>;
	view: HistoryView;
}

// makeLoadHistory reads the ledger and shows/refreshes the panel for ctx. Shared
// by the open command and the rollback reload, so a rollback's fresh ledger
// re-renders the same panel.
export function makeLoadHistory(
	deps: HistoryOpenDeps,
): (ctx: HistoryContext) => Promise<void> {
	return async (ctx) => {
		const cwd = path.dirname(ctx.tomlUri.fsPath);
		const res = await deps.runHistory(cwd, ctx.env, ctx.name);
		if (!res.ok) {
			if (res.auth) {
				// Clear any stale timeline in an already-open panel (a refresh whose
				// env now needs sign-in), then offer to sign in.
				deps.view.reportError(
					`${ctx.env} needs sign-in to read history for "${ctx.name}".`,
				);
				await offerSignIn(ctx);
			} else {
				await fail(
					ctx,
					`Couldn't read history for "${ctx.name}": ${res.reason}`,
					deps.view,
				);
			}
			return;
		}
		const entries = parseHistoryReport(res.stdout);
		if (!entries) {
			await fail(
				ctx,
				`Couldn't read the history for "${ctx.name}".`,
				deps.view,
			);
			return;
		}
		deps.view.show(entries, ctx);
	};
}

export interface HistoryCommandArgs {
	name: string;
	tomlUri: vscode.Uri;
	env: string;
	// Picks the rollback confirm tier; undefined when not yet known.
	production?: boolean;
}

export function historyCommand(
	load: (ctx: HistoryContext) => Promise<void>,
): (args: HistoryCommandArgs) => Promise<void> {
	return async (args) => {
		if (!vscode.workspace.isTrusted) return;
		await load({
			env: args.env,
			tomlUri: args.tomlUri,
			name: args.name,
			production: args.production,
		});
	};
}

// fail logs and toasts, and updates an already-open panel (a refresh after a
// rollback) so a stale timeline isn't left implying the read succeeded.
async function fail(
	ctx: HistoryContext,
	message: string,
	view: HistoryView,
): Promise<void> {
	log(`history ${ctx.name} --env ${ctx.env}: ${message}`);
	view.reportError(message);
	await vscode.window.showErrorMessage(message);
}

async function offerSignIn(ctx: HistoryContext): Promise<void> {
	const signIn = "Sign in";
	const choice = await vscode.window.showErrorMessage(
		`${ctx.env} needs sign-in to read history for "${ctx.name}".`,
		signIn,
	);
	if (choice === signIn) {
		await vscode.commands.executeCommand("gaffer.signIn", {
			env: ctx.env,
			tomlUri: ctx.tomlUri,
		});
	}
}
