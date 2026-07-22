// The "Preview" action from the env-block lens: run `gaffer deploy --dry-run
// --json` for the whole project against that env and render the plan in the
// deploy-plan webview. Read-only - it stops at the plan, never applies.
//
// A cold spawn (not the LSP warm connection) so the preview resolves exactly the
// way a real deploy will, and the same auth path the apply will take. `deploy
// --dry-run --json` prints the plan envelope on stdout even when it exits 2
// (changes pending) or 1 (blocked), so the exit code is read for auth, not
// treated as failure.

import * as path from "node:path";
import * as vscode from "vscode";
import { log } from "../output.js";
import { parsePlanReport } from "./deploy-plan.js";
import type { DeployPlanView } from "../panels/deploy-plan.js";

// gaffer's exit code when a live command needs an interactive sign-in
// (cli/cmd/root.go exitCodeAuthRequired). The Preview lens is only offered on an
// already-authenticated env, so this covers a token that expired since.
const EXIT_AUTH_REQUIRED = 4;

export interface DeployPreviewArgs {
	env: string;
	tomlUri: vscode.Uri;
	// Set by the per-projection action menu to scope the plan to one projection;
	// undefined for the whole-project env-block Deploy lens.
	name?: string;
}

export type DryRunResult =
	| { ok: true; stdout: string; code: number | null }
	| { ok: false; err: unknown };

export interface DeployPreviewDeps {
	// Structural so tests inject a fake in place of the real webview manager.
	view: Pick<DeployPlanView, "show">;
	// Runs `gaffer deploy [name] --dry-run --json --env <env>` in the project
	// directory, returning its stdout and exit code (or a spawn failure). `name`
	// scopes the plan to one projection; undefined plans the whole project. A field
	// so tests inject a fake in place of a live CLI.
	runDryRun: (
		env: string,
		cwd: string,
		name: string | undefined,
	) => Promise<DryRunResult>;
}

export function deployPreview(
	deps: DeployPreviewDeps,
): (args: DeployPreviewArgs) => Promise<void> {
	return async ({ env, tomlUri, name }) => {
		if (!vscode.workspace.isTrusted) return;

		const cwd = path.dirname(tomlUri.fsPath);
		const result = await deps.runDryRun(env, cwd, name);
		if (!result.ok) {
			const msg =
				result.err instanceof Error ? result.err.message : String(result.err);
			log(`deploy preview failed for ${env}: ${msg}`);
			await vscode.window.showErrorMessage(
				`Couldn't preview the deploy to ${env}: ${msg}`,
			);
			return;
		}
		if (result.code === EXIT_AUTH_REQUIRED) {
			await offerSignIn(env, tomlUri);
			return;
		}
		const report = parsePlanReport(result.stdout);
		if (!report) {
			log(`deploy preview for ${env}: unparseable plan (exit ${result.code})`);
			await vscode.window.showErrorMessage(
				`Couldn't read the deploy plan for ${env}.`,
			);
			return;
		}
		deps.view.show(report, { env, tomlUri, ...(name ? { name } : {}) });
	};
}

async function offerSignIn(env: string, tomlUri: vscode.Uri): Promise<void> {
	const pick = await vscode.window.showWarningMessage(
		`Sign in to ${env} to preview the deploy.`,
		"Sign in",
	);
	if (pick === "Sign in") {
		await vscode.commands.executeCommand("gaffer.signIn", { env, tomlUri });
	}
}
