// gaffer.projectionActions: the per-projection action menu opened from the
// "Manage..." CodeLens. Pops a single QuickPick grouped by environment - one
// separator header per env, the env's actions listed under it - so a single
// pick runs an action against a specific env with no drill-down. Today the only
// action is "Diff against deployed"; operate / history verbs slot in under each
// env group later. Lives here for symmetry with the other lens-driven command
// bodies (debug-projection-pick.ts), keeping activate() free of command logic.

import * as vscode from "vscode";

// A configured [env.<name>] as the lens reports it.
export interface ProjectionActionsEnv {
	name: string;
	default: boolean;
}

export interface ProjectionActionsArgs {
	name: string;
	tomlUri: vscode.Uri;
	envs: ProjectionActionsEnv[];
}

// What a chosen menu row runs: an action against one env. `action` is a
// discriminant so the dispatch stays exhaustive as verbs are added.
export interface ProjectionAction {
	env: string;
	action: "diff";
}

export interface ProjectionActionsDeps {
	diff: (args: {
		name: string;
		tomlUri: vscode.Uri;
		env: string;
	}) => Promise<void>;
}

type ActionItem = vscode.QuickPickItem & { pick?: ProjectionAction };

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
			pick: { env: env.name, action: "diff" },
		});
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
		switch (picked.pick.action) {
			case "diff":
				await deps.diff({
					name: args.name,
					tomlUri: args.tomlUri,
					env: picked.pick.env,
				});
				return;
		}
	};
}
