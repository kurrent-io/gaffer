// The history viewer's "rollback" action: redeploy a prior version by content
// hash. Renders the confirm tier natively, spawns `gaffer rollback --yes --json`,
// then reloads the timeline. Rollback is reversible (it rewrites the live query
// and keeps state), so it's never the type-the-name tier - silent off-prod, a
// modal accept on production (or when production isn't known yet).
//
// Consequence to surface: the live query moves, state is kept, and local files
// are untouched - so they'll read as drift until updated.

import * as path from "node:path";
import * as vscode from "vscode";
import { parseRollbackResult } from "./history-schema.js";
import type { HistoryContext, HistorySend } from "../panels/history-view.js";

// The wiring adapts a `gaffer rollback --json` spawn to this: stdout on a clean
// exit, or a classified failure (auth vs a refusal/error reason).
export type RollbackOutcome =
	| { ok: true; stdout: string }
	| { ok: false; auth: boolean; reason: string };

const CONSEQUENCE =
	"Rewrites the live query to this version. State is kept, and your local files are unchanged - so they'll read as drift until you update them.";

export interface HistoryRollbackDeps {
	runRollback: (
		cwd: string,
		env: string,
		name: string,
		hash: string,
	) => Promise<RollbackOutcome>;
	// Re-read the ledger and re-render the panel after a rollback lands.
	reload: (ctx: HistoryContext) => Promise<void>;
}

// confirm renders the tier and reports whether to proceed. production is
// tri-state: only a known non-prod rollback runs silently; known-prod and
// unknown-prod both get the modal, so a prod rollback is never silent just
// because status hasn't loaded.
async function confirm(ctx: HistoryContext, version: number): Promise<boolean> {
	if (ctx.production === false) return true;
	const where = ctx.production === true ? `PRODUCTION [${ctx.env}]` : ctx.env;
	const choice = await vscode.window.showWarningMessage(
		`Roll back "${ctx.name}" to v${version} on ${where}? ${CONSEQUENCE}`,
		{ modal: true },
		"Roll back",
	);
	return choice === "Roll back";
}

export function rollbackFromHistory(
	deps: HistoryRollbackDeps,
): (
	ctx: HistoryContext,
	target: { version: number; hash: string },
	send: HistorySend,
) => Promise<void> {
	return async (ctx, target, send) => {
		if (!vscode.workspace.isTrusted) {
			send({ type: "rollback-error", version: target.version, message: "" });
			return;
		}
		if (!(await confirm(ctx, target.version))) {
			// Cancelled: release the panel's in-flight guard without a toast.
			send({ type: "rollback-error", version: target.version, message: "" });
			return;
		}

		send({ type: "rollback-active", version: target.version });
		const cwd = path.dirname(ctx.tomlUri.fsPath);
		const res = await deps.runRollback(cwd, ctx.env, ctx.name, target.hash);

		if (!res.ok) {
			if (res.auth) {
				await offerSignIn(ctx);
			} else {
				await vscode.window.showErrorMessage(
					`Couldn't roll back "${ctx.name}" on ${ctx.env}: ${res.reason}`,
				);
			}
			send({
				type: "rollback-error",
				version: target.version,
				message: res.reason,
			});
			return;
		}

		const result = parseRollbackResult(res.stdout);
		if (!result) {
			await vscode.window.showErrorMessage(
				`Couldn't read the rollback result for "${ctx.name}".`,
			);
			send({ type: "rollback-error", version: target.version, message: "" });
			return;
		}

		// Release the guard, then reload so the fresh ledger re-renders.
		send({
			type: "rollback-done",
			version: target.version,
			outcome: result.outcome,
		});
		if (result.outcome === "rolled-back") {
			await vscode.window.showInformationMessage(
				`Rolled back "${ctx.name}" to ${short(result.hash)} on ${ctx.env}. Local files are unchanged and will show as drift.`,
			);
		} else {
			await vscode.window.showInformationMessage(
				`"${ctx.name}" is already at that version on ${ctx.env}.`,
			);
		}
		await deps.reload(ctx);
	};
}

function short(h: string): string {
	return h.length >= 7 ? h.slice(0, 7) : h;
}

async function offerSignIn(ctx: HistoryContext): Promise<void> {
	const signIn = "Sign in";
	const choice = await vscode.window.showErrorMessage(
		`${ctx.env} needs sign-in to roll back "${ctx.name}".`,
		signIn,
	);
	if (choice === signIn) {
		await vscode.commands.executeCommand("gaffer.signIn", {
			env: ctx.env,
			tomlUri: ctx.tomlUri,
		});
	}
}
