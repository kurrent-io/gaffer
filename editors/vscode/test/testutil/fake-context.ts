// Minimal ExtensionContext stand-in for tests. Only exposes the
// `subscriptions` array - that's all the production code reads. Cast
// through unknown since the real ExtensionContext has 16 fields we
// don't fake.

import type * as vscode from "vscode";

export function makeContext(): vscode.ExtensionContext {
	return { subscriptions: [] } as unknown as vscode.ExtensionContext;
}
