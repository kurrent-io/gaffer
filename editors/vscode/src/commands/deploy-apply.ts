// The Deploy button in the deploy-plan webview: a native tiered confirm, then a
// streaming `gaffer deploy --yes --json --stream` whose per-projection progress
// is fed back into the webview in place.
//
// Confirm tiers mirror the MCP deploy_gate, on (production, hasRebuild):
//   - silent: known non-production and no rebuild -> apply straight away (the
//     preview was the review).
//   - accept: production XOR rebuild, or unknown production -> a modal accept.
//   - type-name: production AND rebuild -> type the environment name to confirm.
// The apply itself is a cold spawn (not the LSP warm connection), the same auth
// path the preview took, so what you previewed is what deploys.

import * as path from "node:path";
import * as vscode from "vscode";
import { log } from "../output.js";
import type { CliMessage } from "../ipc/schemas.js";
import type { PlanReport } from "./deploy-plan.js";
import type { DeployPlanContext, DeploySend } from "../panels/deploy-plan.js";

// gaffer's exit code for a live command needing an interactive sign-in
// (cli/cmd/root.go exitCodeAuthRequired).
const EXIT_AUTH_REQUIRED = 4;

export interface DeployApplyDeps {
	// Spawns `deploy [name] --yes --json --stream` (plus --no-validate when
	// bypassing) in the project directory and drives the callbacks with its NDJSON
	// progress and exit code. `name` scopes the apply to one projection; undefined
	// applies the whole project. A field so tests inject a fake in place of a live
	// spawn.
	run: (
		env: string,
		cwd: string,
		noValidate: boolean,
		name: string | undefined,
		handlers: {
			onLine: (msg: CliMessage) => void;
			onExit: (code: number | null) => void;
		},
	) => void;
}

export function deployApply(
	deps: DeployApplyDeps,
): (
	ctx: DeployPlanContext,
	report: PlanReport,
	noValidate: boolean,
	send: DeploySend,
) => Promise<void> {
	return async (ctx, report, noValidate, send) => {
		if (!vscode.workspace.isTrusted) return;

		const hasRebuild = (report.plan ?? []).some((p) => p.outcome === "rebuilt");
		if (!(await confirmDeploy(ctx.env, report.production, hasRebuild))) return;

		send({ type: "deploy-started" });
		const cwd = path.dirname(ctx.tomlUri.fsPath);
		let sawSummary = false;
		deps.run(ctx.env, cwd, noValidate, ctx.name, {
			onLine: (msg) => {
				if (msg.type === "deploy_start") {
					send({ type: "deploy-active", name: msg.name });
				} else if (msg.type === "deploy_result") {
					const detail = msg.error ?? msg.reason;
					send({
						type: "deploy-item",
						name: msg.name,
						outcome: msg.outcome,
						...(detail ? { detail } : {}),
					});
				} else if (msg.type === "deploy_summary") {
					sawSummary = true;
					send({
						type: "deploy-done",
						summary: {
							created: msg.created,
							updated: msg.updated,
							rebuilt: msg.rebuilt,
							skipped: msg.skipped,
							refused: msg.refused,
							invalid: msg.invalid,
							failed: msg.failed,
						},
					});
				}
			},
			onExit: (code) => {
				// A clean run ends on deploy_summary (deploy-done already sent). A
				// non-zero exit without one means the apply couldn't run.
				if (sawSummary) return;
				if (code === EXIT_AUTH_REQUIRED) {
					send({ type: "deploy-error", message: `${ctx.env} needs sign-in.` });
					void offerSignIn(ctx.env, ctx.tomlUri);
					return;
				}
				log(`deploy --stream for ${ctx.env} exited ${code} with no summary`);
				send({
					type: "deploy-error",
					message: `Deploy exited unexpectedly (code ${code ?? "unknown"}).`,
				});
			},
		});
	};
}

// confirmDeploy renders the tier and reports whether to proceed. production is
// tri-state: a silent apply needs it *known* non-production, and type-the-name
// needs it *known* production - unknown production falls through to the accept
// modal, so a prod deploy is never run silently just because a plan didn't
// resolve it.
async function confirmDeploy(
	env: string,
	production: boolean | undefined,
	hasRebuild: boolean,
): Promise<boolean> {
	const knownProd = production === true;
	const knownNonProd = production === false;

	if (knownNonProd && !hasRebuild) return true;

	if (knownProd && hasRebuild) {
		const typed = await vscode.window.showInputBox({
			title: `Deploy to ${env}`,
			prompt: `Deploying to production and rebuilding at least one projection from zero. Type the environment name "${env}" to confirm.`,
			ignoreFocusOut: true,
			validateInput: (val) =>
				val === env ? undefined : `Type "${env}" to confirm.`,
		});
		return typed === env;
	}

	const where = knownProd ? `PRODUCTION [${env}]` : env;
	const consequence = hasRebuild
		? " At least one projection rebuilds from zero."
		: "";
	const choice = await vscode.window.showWarningMessage(
		`Deploy to ${where}?${consequence}`,
		{ modal: true },
		"Deploy",
	);
	return choice === "Deploy";
}

async function offerSignIn(env: string, tomlUri: vscode.Uri): Promise<void> {
	const pick = await vscode.window.showErrorMessage(
		`${env} needs sign-in to deploy.`,
		"Sign in",
	);
	if (pick === "Sign in") {
		await vscode.commands.executeCommand("gaffer.signIn", { env, tomlUri });
	}
}
