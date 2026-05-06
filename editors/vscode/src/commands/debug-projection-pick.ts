// gaffer.debugProjectionPick: lens-driven flow for the multi-fixture
// "Debug from fixture..." path. Pops a fixture QuickPick, then kicks
// the session via the supplied start fn. Trivial; lives here for
// symmetry with run-projection.ts so the activate() wiring file
// doesn't host one-off command bodies.

import * as vscode from "vscode";
import type { DebugProjectionArgs } from "../debugging/session-controller.js";

export interface DebugProjectionPickDeps {
	start: (args: DebugProjectionArgs) => Promise<void>;
}

export interface DebugProjectionPickArgs {
	name: string;
	tomlUri: vscode.Uri;
	fixtureNames: string[];
}

export function debugProjectionPick(
	deps: DebugProjectionPickDeps,
): (args: DebugProjectionPickArgs) => Promise<void> {
	return async (args) => {
		if (args.fixtureNames.length === 0) return;
		const picked = await vscode.window.showQuickPick(args.fixtureNames, {
			placeHolder: `Pick a fixture to debug ${args.name} with`,
		});
		if (!picked) return;
		await deps.start({
			name: args.name,
			tomlUri: args.tomlUri,
			fixture: picked,
		});
	};
}
