// Minimal ExtensionContext stand-in for tests. Only exposes the
// `subscriptions` array - that's all the production code reads.

import type { Disposable } from "../__mocks__/vscode.js";

export interface FakeContext {
	subscriptions: Disposable[];
}

export function makeContext(): FakeContext {
	return { subscriptions: [] };
}
