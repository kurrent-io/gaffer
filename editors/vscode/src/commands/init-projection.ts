// gaffer.init: command-palette flow that runs `gaffer init --yes` in
// the workspace. Lives outside extension.ts so the branching UX logic
// (folder resolution, toml-exists handling) can be unit-tested in
// isolation.

import * as vscode from "vscode";
import * as path from "node:path";
import { runGafferCommand, type SpawnTelemetry } from "../discovery/cli.js";
import { showCliCommandFailure } from "../notifications/cli.js";
import { showTomlExists } from "../notifications/workspace.js";
import { showTrustWarning } from "../notifications/trust.js";
import {
	fileExists,
	folderDisplayName,
	openFile,
	resolveTargetForCommand,
} from "./workspace.js";

export interface InitProjectionDeps {
	telemetry: SpawnTelemetry;
}

export function initProjection(deps: InitProjectionDeps): () => Promise<void> {
	return async () => {
		if (!vscode.workspace.isTrusted) {
			void showTrustWarning();
			return;
		}
		const target = await resolveTargetForCommand(
			"Initialize gaffer project in which folder?",
		);
		if (!target) return;
		const tomlUri = vscode.Uri.file(path.join(target.fsPath, "gaffer.toml"));
		// Stat the toml ourselves rather than relying on the CLI's
		// "already exists" stderr - keeps the open-existing button
		// path off the spawn entirely and decoupled from the CLI's
		// error wording.
		if (await fileExists(tomlUri.fsPath)) {
			const open = await showTomlExists(folderDisplayName(target));
			if (open) await openFile(tomlUri);
			return;
		}
		const result = await runGafferCommand(
			["init", "--yes"],
			target.fsPath,
			deps.telemetry,
			"command_palette",
		);
		if (!result.ok) {
			void showCliCommandFailure("init", result.err);
			return;
		}
		await openFile(tomlUri);
	};
}
