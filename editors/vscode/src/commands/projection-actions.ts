// gaffer.projectionActions: the per-projection action menu opened from the
// "Manage..." CodeLens. Pops a single QuickPick grouped by environment - one
// separator header per env, the env's actions listed under it - so a single
// pick runs an action against a specific env (delete adds a second step for its
// emitted-streams scope). Actions: deploy this projection (opens the scoped plan
// webview via gaffer.deployPreview), diff against deployed, and the state-aware
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
	// "auth" (sign-in needed) or "unavailable" (a failed read); empty when the env
	// resolved or has no status yet. Both collapse the env to a single row - "auth"
	// to a sign-in, "unavailable" to a non-actionable notice - since no action can
	// run against an env that isn't reachable or authenticated.
	status?: string;
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
	action: "deploy" | "diff" | "signin" | OperateVerb;
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
	// The server sends "" for an indeterminate state, but harden against a raw
	// "unknown" so a server regression can't hide the offer-both fallback. A known
	// non-running state (stopped/aborted/faulted) still offers only Resume.
	const known =
		env.state !== undefined && env.state !== "" && env.state !== "unknown";
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
// a single env, so the env's placement is the same whatever the env count. Envs
// appear in gaffer.toml order (the default is labelled, not reordered); item
// order within an env is the action order.
// separatorLabel is the env header row: its name and whether it's the default.
// An auth env adds a "sign-in needed" note, since its single row ("Sign in") is
// the fix, not the status. An unavailable env carries no note - its single row
// states the status itself, so a note would just repeat it.
function separatorLabel(env: ProjectionActionsEnv): string {
	const base = env.default ? `${env.name} (default)` : env.name;
	return env.status === "auth" ? `${base} · sign-in needed` : base;
}

export function buildActionItems(envs: ProjectionActionsEnv[]): ActionItem[] {
	const items: ActionItem[] = [];
	for (const env of envs) {
		items.push({
			label: separatorLabel(env),
			kind: vscode.QuickPickItemKind.Separator,
		});
		// A sign-in-needed env can't diff or operate until it's authenticated, and
		// every action would just funnel to a sign-in - so collapse to one.
		if (env.status === "auth") {
			items.push({
				label: "$(key) Sign in",
				pick: { env: env.name, action: "signin", production: env.production },
			});
			continue;
		}
		// A failed status read means nothing can run against this env - deploy, diff,
		// and the operate verbs would all just report the same failure - so collapse
		// to a single non-actionable notice rather than a menu of dead actions. No
		// pick payload, so selecting it is a no-op.
		if (env.status === "unavailable") {
			items.push({
				label: "$(warning) Unavailable",
				description: "the environment's status couldn't be read",
			});
			continue;
		}
		items.push({
			label: "$(rocket) Deploy",
			pick: { env: env.name, action: "deploy", production: env.production },
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
		if (pick.action === "deploy") {
			await vscode.commands.executeCommand("gaffer.deployPreview", {
				name: args.name,
				env: pick.env,
				tomlUri: args.tomlUri,
			});
			return;
		}
		if (pick.action === "diff") {
			await deps.diff({
				name: args.name,
				tomlUri: args.tomlUri,
				env: pick.env,
			});
			return;
		}
		if (pick.action === "signin") {
			await vscode.commands.executeCommand("gaffer.signIn", {
				env: pick.env,
				tomlUri: args.tomlUri,
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
