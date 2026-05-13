// Minimal ExtensionContext stand-in for tests. Only exposes the
// fields production code reads (subscriptions, extensionPath,
// globalStorageUri, extension.packageJSON). Cast through unknown
// since the real ExtensionContext has 16 fields we don't fake.

import * as fs from "node:fs";
import * as os from "node:os";
import * as path from "node:path";
import type * as vscode from "vscode";

export function makeContext(): vscode.ExtensionContext {
	const storageDir = fs.mkdtempSync(path.join(os.tmpdir(), "gaffer-ext-ctx-"));
	const globalStorageUri = {
		fsPath: storageDir,
		scheme: "file",
		path: storageDir,
		authority: "",
		query: "",
		fragment: "",
		with: () => globalStorageUri,
		toJSON: () => storageDir,
		toString: () => `file://${storageDir}`,
	} as unknown as vscode.Uri;
	return {
		subscriptions: [],
		extensionPath: "/fake/extension",
		globalStorageUri,
		extension: {
			packageJSON: { version: "0.0.0-test" },
		},
	} as unknown as vscode.ExtensionContext;
}
