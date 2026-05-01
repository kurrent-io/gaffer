// Shared test fixtures. Anything duplicated across two or more test
// files belongs here. Anything used by exactly one test stays inline.

import * as vscode from "vscode";
import type { Manifest } from "../../src/discovery/schemas.js";

// Manifest declaring a `dev` command with the `--debug` flag - the
// minimum that lets a Debug lens render and a session start.
export const okManifest: Manifest = {
	version: "1.0.0",
	commands: { dev: { flags: ["debug"] } },
};

// Cast-through-unknown TextDocument stand-in. Production reads `.uri`
// and `.getText()` only; the rest of the TextDocument surface (line
// counts, version, save) is never touched, so a duck-type fake is
// sufficient.
export function makeDoc(uri: vscode.Uri, text: string): vscode.TextDocument {
	return {
		uri,
		getText: () => text,
	} as unknown as vscode.TextDocument;
}

// Tree element representing a partition row in the State view. The
// StateProvider distinguishes partitions from regular sections via
// `contextValue === "partition"`, so tests that drive
// `getChildren(element)` for a partition need this exact shape.
export function makePartitionElement(name: string): vscode.TreeItem {
	const item = new vscode.TreeItem(
		name,
		vscode.TreeItemCollapsibleState.Collapsed,
	);
	item.contextValue = "partition";
	return item;
}
