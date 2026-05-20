// Workspace-related helpers shared by init / scaffold. Kept in a
// dedicated module so both commands stay narrow and tests don't have
// to import one command to exercise the other's UX plumbing.

import * as vscode from "vscode";
import { promises as fs } from "node:fs";
import * as path from "node:path";
import { showNoWorkspace } from "../notifications/workspace.js";

// Bundles the "explorer-context URI wins; else multi-root picker;
// else warn no-workspace" branch that every command-palette flow
// needs. Returns the resolved folder URI or undefined; fires the
// no-workspace warning as a side effect so callers don't have to
// re-check `workspaceFolders` after a null result.
export async function resolveTargetForCommand(
	placeholder: string,
	contextUri?: vscode.Uri,
): Promise<vscode.Uri | undefined> {
	if (contextUri) return contextUri;
	const target = await resolveTargetFolder(placeholder);
	if (!target && !vscode.workspace.workspaceFolders?.length) {
		void showNoWorkspace();
	}
	return target;
}

// resolveTargetFolder: single workspace folder is used directly,
// multi-root prompts a picker, no workspace returns undefined.
// Exported separately because resolveTargetForCommand bundles in the
// no-workspace warning + URI-bypass logic - direct callers that need
// the bare folder pick (none today, but the seam is the natural one).
export async function resolveTargetFolder(
	placeholder: string,
): Promise<vscode.Uri | undefined> {
	const folders = vscode.workspace.workspaceFolders;
	if (!folders || folders.length === 0) return undefined;
	const first = folders[0];
	if (first && folders.length === 1) return first.uri;
	const picked = await vscode.window.showQuickPick(
		folders.map((f) => ({
			label: f.name,
			description: vscode.workspace.asRelativePath(f.uri, true),
			folder: f,
		})),
		{ placeHolder: placeholder },
	);
	return picked?.folder.uri;
}

// Falls back to the full fsPath when basename comes back empty (a
// workspace opened at the filesystem root - rare, but the warning
// reading "No gaffer.toml in ." is worse than the full path).
export function folderDisplayName(uri: vscode.Uri): string {
	return path.basename(uri.fsPath) || uri.fsPath;
}

// Walks upward from `start` looking for a gaffer.toml, mirroring the
// CLI's `project.FindRoot()` (which walks from cwd). Bounded by the
// containing workspace folder so we don't surface a toml living
// outside the user's project tree. Returns the folder URI containing
// the toml, or undefined when none is found.
export async function findProjectRoot(
	start: vscode.Uri,
): Promise<vscode.Uri | undefined> {
	// Bail when start isn't inside any workspace folder. Without
	// this the walk runs to filesystem root and could surface an
	// unrelated gaffer.toml from outside the workspace tree.
	const workspace = vscode.workspace.getWorkspaceFolder(start);
	if (!workspace) return undefined;
	const stopAt = workspace.uri.fsPath;
	let dir = start.fsPath;
	for (;;) {
		if (await fileExists(path.join(dir, "gaffer.toml"))) {
			return vscode.Uri.file(dir);
		}
		if (dir === stopAt) return undefined;
		const parent = path.dirname(dir);
		if (parent === dir) return undefined;
		dir = parent;
	}
}

// Narrowed to ENOENT so a permission / IO error doesn't get
// misclassified as "no file here" - that path would drive the caller
// into a misleading "no toml" or "no projection" branch. Rethrowing
// lets wrapAsync route the error to its exception envelope.
export async function fileExists(path: string): Promise<boolean> {
	try {
		await fs.stat(path);
		return true;
	} catch (err) {
		if ((err as NodeJS.ErrnoException).code === "ENOENT") return false;
		throw err;
	}
}

// Open via the built-in `vscode.open` command so we don't have to
// stand up TextDocument plumbing for one call. VS Code resolves the
// URI to the appropriate editor.
export async function openFile(uri: vscode.Uri): Promise<void> {
	await vscode.commands.executeCommand("vscode.open", uri);
}
