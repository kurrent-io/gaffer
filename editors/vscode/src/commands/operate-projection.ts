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
	// Tri-state: true/false, or undefined when the env's production status isn't
	// known yet. Unknown never takes the silent path - the confirm fails safe.
	production: boolean | undefined;
	// Whether the deployed projection emits streams; only then does delete offer
	// the second step to also remove them.
	emits: boolean;
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
		deleteEmitted: boolean;
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

function consequenceOf(verb: OperateVerb, deleteEmitted: boolean): string {
	if (verb === "delete" && deleteEmitted) {
		return "Removes the projection, its state, checkpoints, and the streams it emitted. No undo.";
	}
	return VERBS[verb].consequence;
}

// deleteScope asks whether to also remove the projection's emitted streams. Only
// called for delete when the projection emits; returns the chosen deleteEmitted,
// or undefined if the user dismissed the pick (cancel the whole operation).
async function deleteScope(
	name: string,
	env: string,
): Promise<boolean | undefined> {
	const picked = await vscode.window.showQuickPick(
		[
			{
				label: "Delete",
				detail: "Remove the projection, its state, and checkpoints.",
				emitted: false,
			},
			{
				label: "Delete, and the streams it emitted",
				detail: "Also remove the streams the projection emitted.",
				emitted: true,
			},
		],
		{ placeHolder: `Delete ${name} on ${env}` },
	);
	return picked?.emitted;
}

// confirm renders the tier and reports whether to proceed. production is
// tri-state: a silent run needs it *known* non-production, and type-the-name
// needs it *known* production - unknown production falls through to the accept
// modal, so a prod op is never run silently just because status hasn't loaded.
async function confirm(
	args: OperateProjectionArgs,
	consequence: string,
): Promise<boolean> {
	const spec = VERBS[args.verb];
	const knownProd = args.production === true;
	const knownNonProd = args.production === false;

	// silent: known non-prod and reversible.
	if (knownNonProd && !spec.noUndo) return true;

	// type-the-name: known production and no-undo (delete on prod).
	if (knownProd && spec.noUndo) {
		const typed = await vscode.window.showInputBox({
			title: `${spec.title} ${args.name} on ${args.env}`,
			prompt: `${consequence} Type the projection name "${args.name}" to confirm.`,
			ignoreFocusOut: true,
			validateInput: (val) =>
				val === args.name ? undefined : `Type "${args.name}" to confirm.`,
		});
		return typed === args.name;
	}

	// accept: everything else - known-prod reversible, non-prod no-undo, and any
	// unknown-production verb - gets a modal accept/cancel.
	const where = knownProd ? `PRODUCTION [${args.env}]` : args.env;
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

		// Deleting an emitting projection asks whether to also remove its emitted
		// streams, as a second step, before the confirm tier. Everything else, and
		// a non-emitting delete, skips straight to the confirm.
		let deleteEmitted = false;
		if (args.verb === "delete" && args.emits) {
			const scope = await deleteScope(args.name, args.env);
			if (scope === undefined) return; // dismissed
			deleteEmitted = scope;
		}

		const consequence = consequenceOf(args.verb, deleteEmitted);
		if (!(await confirm(args, consequence))) return;

		let result: OperateResult;
		try {
			result = await deps.request({
				name: args.name,
				tomlUri: args.tomlUri,
				env: args.env,
				verb: args.verb,
				deleteEmitted,
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
