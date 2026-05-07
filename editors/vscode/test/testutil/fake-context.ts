// Minimal ExtensionContext stand-in for tests. Only exposes the
// fields production code reads (subscriptions, extensionPath). Cast
// through unknown since the real ExtensionContext has 16 fields we
// don't fake.

import type * as vscode from "vscode";

export function makeContext(): vscode.ExtensionContext {
	return {
		subscriptions: [],
		extensionPath: "/fake/extension",
	} as unknown as vscode.ExtensionContext;
}
