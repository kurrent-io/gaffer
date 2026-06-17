import { describe, expect, it } from "vitest";
import type * as vscode from "vscode";
import { getOrCreateKeyringPassword } from "./keyring-secret.js";

function fakeSecrets(initial?: Record<string, string>): vscode.SecretStorage {
	const store = new Map<string, string>(Object.entries(initial ?? {}));
	return {
		get: (key: string) => Promise.resolve(store.get(key)),
		store: (key: string, value: string) => {
			store.set(key, value);
			return Promise.resolve();
		},
		delete: (key: string) => {
			store.delete(key);
			return Promise.resolve();
		},
		onDidChange: () => ({ dispose: () => {} }),
	} as unknown as vscode.SecretStorage;
}

describe("getOrCreateKeyringPassword", () => {
	it("returns the stored passphrase when one exists", async () => {
		const secrets = fakeSecrets({ "gaffer.keyringPassword": "existing" });
		expect(await getOrCreateKeyringPassword(secrets)).toBe("existing");
	});

	it("generates, persists, and reuses a passphrase on first use", async () => {
		const secrets = fakeSecrets();
		const first = await getOrCreateKeyringPassword(secrets);
		expect(first).toMatch(/^[0-9a-f]{64}$/);
		// A second call returns the same persisted value, not a fresh one.
		expect(await getOrCreateKeyringPassword(secrets)).toBe(first);
	});
});
