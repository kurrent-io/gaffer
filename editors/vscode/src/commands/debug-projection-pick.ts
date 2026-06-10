// gaffer.debugProjectionPick: lens-driven "Debug from..." flow. Pops a
// QuickPick of the projection's fixtures and configured environments,
// then kicks the session via the supplied start fn. Lives here for
// symmetry with run-projection.ts so the activate() wiring file doesn't
// host one-off command bodies.

import * as vscode from "vscode";
import type { DebugProjectionArgs } from "../debugging/session-controller.js";

export interface DebugProjectionPickDeps {
	start: (args: DebugProjectionArgs) => Promise<void>;
}

// A configured [env.<name>] as the lens reports it.
export interface DebugProjectionPickEnv {
	name: string;
	default: boolean;
}

export interface DebugProjectionPickArgs {
	name: string;
	tomlUri: vscode.Uri;
	fixtureNames: string[];
	envs: DebugProjectionPickEnv[];
}

// A picker row carries the source it selects: a fixture name (run
// offline) or an env name (run live). Exactly one is set.
type SourceItem = vscode.QuickPickItem & { fixture?: string; env?: string };

// buildSourceItems lists the debug sources for the "Debug from..."
// picker: declared fixtures first, then configured environments. Mirrors
// the CLI `gaffer dev` picker's `Fixture:` / `Env:` labelling; the
// default env is tagged.
export function buildSourceItems(
	fixtureNames: string[],
	envs: DebugProjectionPickEnv[],
): SourceItem[] {
	const items: SourceItem[] = [];
	for (const f of fixtureNames) {
		items.push({ label: `Fixture: ${f}`, fixture: f });
	}
	for (const e of envs) {
		items.push({
			label: `Env: ${e.name}`,
			...(e.default ? { description: "default" } : {}),
			env: e.name,
		});
	}
	return items;
}

export function debugProjectionPick(
	deps: DebugProjectionPickDeps,
): (args: DebugProjectionPickArgs) => Promise<void> {
	return async (args) => {
		const items = buildSourceItems(args.fixtureNames, args.envs);
		if (items.length === 0) return;
		const picked = await vscode.window.showQuickPick(items, {
			placeHolder: `Pick a source to debug ${args.name}`,
		});
		if (!picked) return;
		await deps.start({
			name: args.name,
			tomlUri: args.tomlUri,
			...(picked.env !== undefined ? { env: picked.env } : {}),
			...(picked.fixture !== undefined ? { fixture: picked.fixture } : {}),
		});
	};
}
