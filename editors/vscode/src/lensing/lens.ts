// Shared CodeLens decision tree used by both the TOML and JS providers.
//
// Returns the right lens for the projection's current state:
// - currently being debugged here -> Stop button
// - currently starting here -> spinner (Stop button while #14 lands)
// - workspace untrusted -> "Trust workspace" prompt
// - manifest not loaded / `dev --debug` not in manifest -> no lens
// - otherwise -> Debug button

import * as vscode from "vscode";
import type { GafferCli } from "../discovery/cli.js";
import type { DebugState } from "../types.js";

export function buildLens(
	cli: GafferCli,
	debugState: DebugState,
	name: string,
	range: vscode.Range,
	tomlUri: vscode.Uri,
): vscode.CodeLens | null {
	if (debugState.name === name) {
		const labels: Record<DebugState["status"], string> = {
			idle: "idle",
			starting: "$(sync~spin) Starting",
			debugging: "$(debug-stop) Debugging",
		};
		const label = labels[debugState.status];
		if (debugState.status === "debugging") {
			return new vscode.CodeLens(range, {
				title: label,
				command: "gaffer.stopDebug",
			});
		}
		// Informational lens (no command). VS Code accepts a command-less lens at runtime;
		// the cast satisfies @types/vscode which marks `command` as required.
		return new vscode.CodeLens(range, { title: label } as vscode.Command);
	}

	if (!vscode.workspace.isTrusted) {
		return new vscode.CodeLens(range, {
			title: "$(workspace-untrusted) Trust workspace to debug",
			command: "workbench.trust.manage",
		});
	}

	if (!cli.hasCommand("dev") || !cli.hasFlag("dev", "debug")) return null;

	return new vscode.CodeLens(range, {
		title: "$(debug-start) Debug",
		command: "gaffer.debugProjection",
		arguments: [{ name, tomlUri }],
	});
}
