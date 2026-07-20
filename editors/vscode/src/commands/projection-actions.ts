// gaffer.projectionActions: the per-projection action menu opened from the
// "Manage..." CodeLens. Pops a single QuickPick grouped by environment - one
// separator header per env, the env's actions listed under it - so a single
// pick runs an action against a specific env (delete adds a second step for its
// emitted-streams scope). Actions: diff against deployed, and the state-aware
// operate verbs (pause/resume/abort/delete).
// Lives here for symmetry with the other lens-driven command bodies
// (debug-projection-pick.ts), keeping activate() free of command logic.

import * as vscode from "vscode";
import type { OperateVerb } from "../lsp/operate.js";

// A configured [env.<name>] as the lens reports it, with the two operate inputs:
// production (picks the confirm tier) and this projection's runtime state (picks
// pause vs resume). state is undefined/"" when unknown - not deployed, not yet
// fetched, or sign-in needed - and the menu then offers both pause and resume.
export interface ProjectionActionsEnv {
	name: string;
	default: boolean;
	production?: boolean;
	state?: string;
	emits?: boolean;
}

export interface ProjectionActionsArgs {
	name: string;
	tomlUri: vscode.Uri;
	envs: ProjectionActionsEnv[];
}

// What a chosen menu row runs: an action against one env. `action` is a
// discriminant so the dispatch stays exhaustive as verbs are added. production
// is tri-state for the operate verbs - true/false/undefined (not yet known) - so
// the confirm tier can fail safe when it's unknown; emits tells the delete verb
// whether to offer the emitted-streams choice.
export interface ProjectionAction {
	env: string;
	action: "diff" | OperateVerb;
	production: boolean | undefined;
	emits?: boolean;
}

export interface ProjectionActionsDeps {
	diff: (args: {
		name: string;
		tomlUri: vscode.Uri;
		env: string;
	}) => Promise<void>;
	operate: (args: {
		name: string;
		tomlUri: vscode.Uri;
		env: string;
		verb: OperateVerb;
		production: boolean | undefined;
		emits: boolean;
	}) => Promise<void>;
}

type ActionItem = vscode.QuickPickItem & { pick?: ProjectionAction };

// operateRows lists the state-aware operate verbs for one env: pause + abort when
// running, resume when not, both pause and resume when the state is unknown.
// Delete is always offered as a single row; its emitted-streams scope is a second
// step handled by the command.
function operateRows(env: ProjectionActionsEnv): ActionItem[] {
	const production = env.production;
	const running = env.state === "running";
	const known = env.state !== undefined && env.state !== "";
	const rows: ActionItem[] = [];
	if (running || !known) {
		rows.push({
			label: "$(debug-pause) Pause",
			pick: { env: env.name, action: "pause", production },
		});
	}
	if (!running || !known) {
		rows.push({
			label: "$(debug-start) Resume",
			pick: { env: env.name, action: "resume", production },
		});
	}
	if (running) {
		rows.push({
			label: "$(debug-stop) Abort",
			pick: { env: env.name, action: "abort", production },
		});
	}
	// One Delete row; the emitted-streams choice is a second step in the command,
	// offered only when the projection emits, so the menu stays uncluttered.
	rows.push({
		label: "$(trash) Delete",
		pick: {
			env: env.name,
			action: "delete",
			production,
			emits: env.emits ?? false,
		},
	});
	return rows;
}

// buildActionItems lays out the menu grouped by environment: an env-name
// separator header, then that env's actions beneath it. Always grouped, even for
// a single env, so the env's placement is the same whatever the env count. The
// default env leads; item order within an env is the action order.
export function buildActionItems(envs: ProjectionActionsEnv[]): ActionItem[] {
	const ordered = [...envs].sort(
		(a, b) => Number(b.default) - Number(a.default),
	);
	const items: ActionItem[] = [];
	for (const env of ordered) {
		items.push({
			label: env.default ? `${env.name} (default)` : env.name,
			kind: vscode.QuickPickItemKind.Separator,
		});
		items.push({
			label: "$(diff-single) Diff against deployed",
			pick: { env: env.name, action: "diff", production: env.production },
		});
		items.push(...operateRows(env));
	}
	return items;
}

export function projectionActions(
	deps: ProjectionActionsDeps,
): (args: ProjectionActionsArgs) => Promise<void> {
	return async (args) => {
		if (args.envs.length === 0) return;
		const items = buildActionItems(args.envs);
		const picked = await vscode.window.showQuickPick(items, {
			placeHolder: `${args.name}: pick an action`,
		});
		if (!picked?.pick) return;
		const pick = picked.pick;
		if (pick.action === "diff") {
			await deps.diff({
				name: args.name,
				tomlUri: args.tomlUri,
				env: pick.env,
			});
			return;
		}
		await deps.operate({
			name: args.name,
			tomlUri: args.tomlUri,
			env: pick.env,
			verb: pick.action,
			production: pick.production,
			emits: pick.emits ?? false,
		});
	};
}
