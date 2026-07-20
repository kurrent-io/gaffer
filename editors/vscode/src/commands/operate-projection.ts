// The operate verbs (pause/resume/abort/delete) from the projection action menu.
// Renders the confirm tier natively, then runs the verb over the LSP warm
// connection (gaffer/operateProjection) and reports the outcome.
//
// Confirm tiers mirror the MCP deploy_gate, derived from (production, noUndo):
//   - silent: non-production and reversible -> run, then a completion toast.
//   - accept: production XOR no-undo -> a modal accept/cancel.
//   - type-name: production AND no-undo (delete on prod) -> type the projection
//     name to confirm.
// Delete is the only no-undo verb; the consequence copy is phrased for the editor
// (the MCP strings name MCP tools, so they aren't reused verbatim).

import * as vscode from "vscode";
import { log } from "../output.js";
import { LspAuthRequiredError } from "../lsp/request.js";
import type { OperateResult, OperateVerb } from "../lsp/operate.js";

export interface OperateProjectionArgs {
	name: string;
	tomlUri: vscode.Uri;
	env: string;
	verb: OperateVerb;
	production: boolean;
	deleteEmitted?: boolean;
}

export interface OperateProjectionDeps {
	// Runs the verb over the LSP warm connection. Throws LspAuthRequiredError when
	// the env needs sign-in, LspUnavailableError otherwise. A field so tests inject
	// a fake in place of a live server.
	request: (args: {
		name: string;
		tomlUri: vscode.Uri;
		env: string;
		verb: OperateVerb;
		deleteEmitted?: boolean;
	}) => Promise<OperateResult>;
}

interface verbSpec {
	noUndo: boolean;
	title: string; // imperative, for the confirm heading
	consequence: string;
}

const VERBS: Record<OperateVerb, verbSpec> = {
	pause: {
		noUndo: false,
		title: "Pause",
		consequence: "Stops after a final checkpoint; resume later.",
	},
	resume: {
		noUndo: false,
		title: "Resume",
		consequence: "Restarts from the last checkpoint.",
	},
	abort: {
		noUndo: false,
		title: "Abort",
		consequence:
			"Stops without a final checkpoint; a later resume reprocesses from the last checkpoint.",
	},
	delete: {
		noUndo: true,
		title: "Delete",
		consequence: "Removes the projection, its state, and checkpoints. No undo.",
	},
};

function consequenceOf(args: OperateProjectionArgs): string {
	if (args.verb === "delete" && args.deleteEmitted) {
		return "Removes the projection, its state, checkpoints, and the streams it emitted. No undo.";
	}
	return VERBS[args.verb].consequence;
}

// confirm renders the tier and reports whether to proceed.
async function confirm(
	args: OperateProjectionArgs,
	consequence: string,
): Promise<boolean> {
	const spec = VERBS[args.verb];
	// silent: non-prod and reversible.
	if (!args.production && !spec.noUndo) return true;

	// type-the-name: production and no-undo (delete on prod).
	if (args.production && spec.noUndo) {
		const typed = await vscode.window.showInputBox({
			title: `${spec.title} ${args.name} on ${args.env}`,
			prompt: `${consequence} Type the projection name "${args.name}" to confirm.`,
			ignoreFocusOut: true,
			validateInput: (val) =>
				val === args.name ? undefined : `Type "${args.name}" to confirm.`,
		});
		return typed === args.name;
	}

	// accept: production XOR no-undo -> modal accept/cancel.
	const where = args.production ? `PRODUCTION [${args.env}]` : args.env;
	const choice = await vscode.window.showWarningMessage(
		`${spec.title} ${args.name} on ${where}? ${consequence}`,
		{ modal: true },
		spec.title,
	);
	return choice === spec.title;
}

function capitalize(s: string): string {
	return s.charAt(0).toUpperCase() + s.slice(1);
}

export function operateProjection(
	deps: OperateProjectionDeps,
): (args: OperateProjectionArgs) => Promise<void> {
	return async (args) => {
		if (!vscode.workspace.isTrusted) return;

		const consequence = consequenceOf(args);
		if (!(await confirm(args, consequence))) return;

		let result: OperateResult;
		try {
			result = await deps.request({
				name: args.name,
				tomlUri: args.tomlUri,
				env: args.env,
				verb: args.verb,
				deleteEmitted: args.deleteEmitted ?? false,
			});
		} catch (err) {
			await reportFailure(args, err);
			return;
		}

		const on = result.target ? ` on ${result.target}` : "";
		await vscode.window.showInformationMessage(
			`${capitalize(result.outcome)} ${result.name}${on}.`,
		);
	};
}

async function reportFailure(
	args: OperateProjectionArgs,
	err: unknown,
): Promise<void> {
	if (err instanceof LspAuthRequiredError) {
		const signIn = "Sign in";
		const choice = await vscode.window.showErrorMessage(
			`${args.env} needs sign-in to ${args.verb} "${args.name}".`,
			signIn,
		);
		if (choice === signIn) {
			await vscode.commands.executeCommand("gaffer.signIn", {
				env: args.env,
				tomlUri: args.tomlUri,
			});
		}
		return;
	}
	const detail = err instanceof Error ? err.message : String(err);
	log(`operate ${args.verb} ${args.name} --env ${args.env} failed: ${detail}`);
	await vscode.window.showErrorMessage(
		`Couldn't ${args.verb} "${args.name}" on ${args.env}: ${detail}`,
	);
}
