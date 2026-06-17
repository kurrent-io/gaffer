// Minimal ExtensionContext stand-in for tests. Only exposes the
// fields production code reads (subscriptions, extensionPath,
// globalStorageUri, workspaceState, extension.packageJSON). Cast
// through unknown since the real ExtensionContext has 16 fields we
// don't fake.

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
		workspaceState: makeMemento(),
		globalState: makeMemento(),
		secrets: makeSecretStorage(),
		extension: {
			packageJSON: { version: "0.0.0-test" },
		},
	} as unknown as vscode.ExtensionContext;
}

// In-memory SecretStorage for tests. Covers get/store/delete; onDidChange is a
// no-op since production code only reads/writes.
function makeSecretStorage(): vscode.SecretStorage {
	const store = new Map<string, string>();
	return {
		get: (key: string): Thenable<string | undefined> =>
			Promise.resolve(store.get(key)),
		store: (key: string, value: string): Thenable<void> => {
			store.set(key, value);
			return Promise.resolve();
		},
		delete: (key: string): Thenable<void> => {
			store.delete(key);
			return Promise.resolve();
		},
		onDidChange: () => ({ dispose: () => {} }),
	} as unknown as vscode.SecretStorage;
}

// In-memory Memento for tests. Mirrors vscode.Memento for the keys
// production code touches; `undefined` updates delete the key, matching
// the real contract.
function makeMemento(): vscode.Memento {
	const store = new Map<string, unknown>();
	return {
		keys: () => [...store.keys()],
		get: <T>(key: string, defaultValue?: T): T | undefined => {
			if (!store.has(key)) return defaultValue;
			return store.get(key) as T;
		},
		update: (key: string, value: unknown): Thenable<void> => {
			if (value === undefined) store.delete(key);
			else store.set(key, value);
			return Promise.resolve();
		},
	} as vscode.Memento;
}
