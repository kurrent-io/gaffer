// gaffer.init: command-palette flow that runs `gaffer init --yes` in
// the workspace. Lives outside extension.ts so the branching UX logic
// (folder resolution, multi-root picker, toml-exists handling) can be
// unit-tested in isolation.

import * as vscode from "vscode";
import { promises as fs } from "node:fs";
import * as path from "node:path";
import { runGafferCommand, type SpawnTelemetry } from "../discovery/cli.js";
import {
	showInitFailure,
	showNoWorkspace,
	showTomlExists,
	showTrustWarning,
} from "../notifications.js";

export interface InitProjectionDeps {
	telemetry: SpawnTelemetry;
}

export function initProjection(deps: InitProjectionDeps): () => Promise<void> {
	return async () => {
		if (!vscode.workspace.isTrusted) {
			void showTrustWarning();
			return;
		}
		const target = await resolveTargetFolder();
		if (!target) return;
		const tomlUri = vscode.Uri.file(path.join(target.fsPath, "gaffer.toml"));
		// Stat the toml ourselves rather than relying on the CLI's
		// "already exists" stderr - keeps the open-existing button
		// path off the spawn entirely and decoupled from the CLI's
		// error wording.
		if (await fileExists(tomlUri.fsPath)) {
			const open = await showTomlExists(path.basename(target.fsPath));
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
			void showInitFailure(result.err);
			return;
		}
		await openFile(tomlUri);
	};
}

// resolveTargetFolder: single workspace folder is used directly,
// multi-root prompts a picker, no workspace surfaces the "open a
// folder first" warning.
export async function resolveTargetFolder(): Promise<vscode.Uri | undefined> {
	const folders = vscode.workspace.workspaceFolders;
	if (!folders || folders.length === 0) {
		void showNoWorkspace();
		return undefined;
	}
	const first = folders[0];
	if (first && folders.length === 1) return first.uri;
	const picked = await vscode.window.showQuickPick(
		folders.map((f) => ({
			label: f.name,
			description: vscode.workspace.asRelativePath(f.uri, true),
			folder: f,
		})),
		{ placeHolder: "Initialize gaffer project in which folder?" },
	);
	return picked?.folder.uri;
}

async function fileExists(path: string): Promise<boolean> {
	try {
		await fs.stat(path);
		return true;
	} catch {
		return false;
	}
}

async function openFile(uri: vscode.Uri): Promise<void> {
	await vscode.commands.executeCommand("vscode.open", uri);
}
