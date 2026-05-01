// Shared CodeLens decision tree used by both the TOML and JS providers.
//
// Returns the right lens for the projection's current state:
// - currently being debugged here -> Stop button (debugging or starting)
// - workspace untrusted -> "Trust workspace" prompt
// - manifest not loaded / `dev --debug` not in manifest -> no lens
// - otherwise -> Debug button

import * as vscode from "vscode";
import { hasCommand, hasFlag } from "../discovery/cli.js";
import type { Manifest } from "../discovery/schemas.js";
import type { DebugState } from "../types.js";

export function buildLens(
	manifest: Manifest | null,
	debugState: DebugState,
	name: string,
	range: vscode.Range,
	tomlUri: vscode.Uri,
): vscode.CodeLens | null {
	if (debugState.name === name) {
		// Stop is active during both starting and debugging - the user can
		// abort if waitForDebug hangs the 15s timeout.
		const title = stopTitle(debugState.status);
		if (title !== null) {
			return new vscode.CodeLens(range, {
				title,
				command: "gaffer.stopDebug",
			});
		}
	}

	if (!vscode.workspace.isTrusted) {
		return new vscode.CodeLens(range, {
			title: "$(workspace-untrusted) Trust workspace to debug",
			command: "workbench.trust.manage",
		});
	}

	if (!hasCommand(manifest, "dev") || !hasFlag(manifest, "dev", "debug")) {
		return null;
	}

	return new vscode.CodeLens(range, {
		title: "$(debug-start) Debug",
		command: "gaffer.debugProjection",
		arguments: [{ name, tomlUri }],
	});
}

function stopTitle(status: DebugState["status"]): string | null {
	switch (status) {
		case "starting":
			return "$(sync~spin) Starting (cancel)";
		case "debugging":
			return "$(debug-stop) Debugging";
		case "idle":
			return null;
		default: {
			const _exhaustive: never = status;
			void _exhaustive;
			return null;
		}
	}
}
