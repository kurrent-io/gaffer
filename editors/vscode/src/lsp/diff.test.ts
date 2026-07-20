import { beforeEach, describe, expect, it, vi } from "vitest";
import * as vscode from "vscode";

// Mutable stand-in for the language client. The `mock` prefix lets the hoisted
// vi.mock factory reference it.
let mockClient:
	| { sendRequest: (method: string, params: unknown) => Promise<unknown> }
	| undefined;
vi.mock("./client.js", () => ({
	getLanguageClient: () => mockClient,
}));

import { requestProjectionDiff } from "./diff.js";

const tomlUri = vscode.Uri.parse("file:///p/sub/gaffer.toml");

// The generic request scaffold (auth mapping, malformed handling, no-client) is
// covered in request.test.ts; here we pin the diff-specific wiring: the method
// name, the configURI mapping, and the diff schema (incl. the not-deployed
// empty-source default).
describe("requestProjectionDiff", () => {
	beforeEach(() => {
		mockClient = undefined;
	});

	it("sends gaffer/diffProjection with configURI from the toml URI and returns the diff", async () => {
		const calls: { method: string; params: unknown }[] = [];
		mockClient = {
			sendRequest: (method, params) => {
				calls.push({ method, params });
				return Promise.resolve({
					name: "checkout",
					left: { ref: "deployed", source: "OLD" },
					right: { ref: "local", source: "NEW" },
				});
			},
		};
		const diff = await requestProjectionDiff("checkout", tomlUri, "prod");
		expect(calls[0]?.method).toBe("gaffer/diffProjection");
		// configURI must match what the server resolves; assert against the URI's
		// own toString so the test survives the mock's URI normalization.
		expect(calls[0]?.params).toEqual({
			name: "checkout",
			configURI: tomlUri.toString(),
			env: "prod",
		});
		expect(diff.left.source).toBe("OLD");
		expect(diff.right.source).toBe("NEW");
	});

	it("defaults a missing deployed source to empty (the not-deployed signal)", async () => {
		mockClient = {
			sendRequest: () =>
				Promise.resolve({
					name: "checkout",
					left: { ref: "deployed" },
					right: { ref: "local", source: "NEW" },
				}),
		};
		const diff = await requestProjectionDiff("checkout", tomlUri, "prod");
		expect(diff.left.source).toBe("");
	});
});
