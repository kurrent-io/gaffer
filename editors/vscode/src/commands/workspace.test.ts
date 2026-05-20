import * as fs from "node:fs";
import * as os from "node:os";
import * as path from "node:path";
import * as vscode from "vscode";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { findProjectRoot, resolveTargetFolder } from "./workspace.js";
import {
	getState,
	queueQuickPick,
	resetVscode,
	setTrusted,
	setWorkspaceFolders,
} from "../../test/testutil/vscode-state.js";

function makeFolder(name: string, fsPath: string): vscode.WorkspaceFolder {
	return { uri: vscode.Uri.file(fsPath), name, index: 0 };
}

describe("resolveTargetFolder", () => {
	beforeEach(() => {
		resetVscode();
		setTrusted(true);
	});

	it("returns undefined when no workspace is open", async () => {
		setWorkspaceFolders([]);
		const resolved = await resolveTargetFolder("placeholder");
		expect(resolved).toBeUndefined();
		// Callers decide how to surface the no-workspace case; this
		// helper stays silent so init and scaffold can pair the
		// warning with their own command-specific copy.
		expect(getState().quickPickCalls).toEqual([]);
	});

	it("returns the single workspace folder without prompting", async () => {
		setWorkspaceFolders([makeFolder("solo", "/ws/solo")]);
		const resolved = await resolveTargetFolder("placeholder");
		expect(resolved?.fsPath).toBe("/ws/solo");
		expect(getState().quickPickCalls).toEqual([]);
	});

	it("prompts a quick-pick with the caller's placeholder when multiple folders are open", async () => {
		setWorkspaceFolders([makeFolder("a", "/ws/a"), makeFolder("b", "/ws/b")]);
		queueQuickPick({
			label: "b",
			description: "/ws/b",
			folder: makeFolder("b", "/ws/b"),
		});
		const resolved = await resolveTargetFolder("Pick a folder for scaffold");
		expect(resolved?.fsPath).toBe("/ws/b");
		const lastCall = getState().quickPickCalls.at(-1);
		const labels = (lastCall?.items as ReadonlyArray<{ label: string }>).map(
			(i) => i.label,
		);
		expect(labels).toEqual(["a", "b"]);
		expect(
			(lastCall?.options as { placeHolder?: string } | undefined)?.placeHolder,
		).toBe("Pick a folder for scaffold");
	});

	it("returns undefined when the user dismisses the multi-root picker", async () => {
		setWorkspaceFolders([makeFolder("a", "/ws/a"), makeFolder("b", "/ws/b")]);
		const resolved = await resolveTargetFolder("placeholder");
		expect(resolved).toBeUndefined();
	});
});

describe("findProjectRoot", () => {
	let tmpRoot: string;

	beforeEach(() => {
		resetVscode();
		setTrusted(true);
		tmpRoot = fs.mkdtempSync(path.join(os.tmpdir(), "gaffer-findroot-"));
		setWorkspaceFolders([makeFolder("ws", tmpRoot)]);
	});

	afterEach(() => {
		fs.rmSync(tmpRoot, { recursive: true, force: true });
	});

	it("returns the start folder when gaffer.toml is right there", async () => {
		fs.writeFileSync(path.join(tmpRoot, "gaffer.toml"), "engine_version = 2\n");
		const root = await findProjectRoot(vscode.Uri.file(tmpRoot));
		expect(root?.fsPath).toBe(tmpRoot);
	});

	it("walks upward to find gaffer.toml at the workspace root from a nested folder", async () => {
		fs.writeFileSync(path.join(tmpRoot, "gaffer.toml"), "engine_version = 2\n");
		const nested = path.join(tmpRoot, "my", "great", "projections");
		fs.mkdirSync(nested, { recursive: true });
		const root = await findProjectRoot(vscode.Uri.file(nested));
		expect(root?.fsPath).toBe(tmpRoot);
	});

	it("returns undefined when no gaffer.toml is found within the workspace boundary", async () => {
		// No toml anywhere; walking up stops at the workspace root.
		const nested = path.join(tmpRoot, "deep");
		fs.mkdirSync(nested);
		const root = await findProjectRoot(vscode.Uri.file(nested));
		expect(root).toBeUndefined();
	});

	it("does not walk past the workspace boundary even if a parent has a gaffer.toml", async () => {
		// A toml in tmpRoot's parent should NOT be picked up - the walk
		// is bounded by the workspace folder. The mock's
		// getWorkspaceFolder returns the first registered folder, so
		// tmpRoot is the boundary.
		const parent = path.dirname(tmpRoot);
		const parentToml = path.join(parent, "gaffer.toml");
		const hadParentToml = fs.existsSync(parentToml);
		if (!hadParentToml) {
			fs.writeFileSync(parentToml, "engine_version = 2\n");
		}
		try {
			const root = await findProjectRoot(vscode.Uri.file(tmpRoot));
			expect(root).toBeUndefined();
		} finally {
			if (!hadParentToml) fs.rmSync(parentToml);
		}
	});

	it("returns undefined when start is outside any workspace folder", async () => {
		// Without an enclosing workspace folder the walk would have
		// no upper bound and could surface a stray gaffer.toml from
		// outside the workspace tree. The function's contract is to
		// stay workspace-bounded regardless of caller.
		setWorkspaceFolders([]);
		const root = await findProjectRoot(vscode.Uri.file(tmpRoot));
		expect(root).toBeUndefined();
	});
});
