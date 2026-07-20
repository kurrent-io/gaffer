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

import { requestOperateProjection } from "./operate.js";
import { LspUnavailableError } from "./request.js";

const tomlUri = vscode.Uri.parse("file:///p/sub/gaffer.toml");

// The generic request scaffold (auth mapping, no-client) is covered in
// request.test.ts; here we pin the operate-specific wiring - the method name, the
// configURI mapping, verb/deleteEmitted forwarding, and the result schema - the
// same boundary whose diff sibling (the stripped lens fields) shipped a live bug.
describe("requestOperateProjection", () => {
	beforeEach(() => {
		mockClient = undefined;
	});

	it("sends gaffer/operateProjection with configURI + verb, defaulting deleteEmitted", async () => {
		const calls: { method: string; params: unknown }[] = [];
		mockClient = {
			sendRequest: (method, params) => {
				calls.push({ method, params });
				return Promise.resolve({
					name: "checkout",
					outcome: "paused",
					target: "prod",
				});
			},
		};
		const res = await requestOperateProjection({
			name: "checkout",
			tomlUri,
			env: "prod",
			verb: "pause",
		});
		expect(calls[0]?.method).toBe("gaffer/operateProjection");
		expect(calls[0]?.params).toEqual({
			name: "checkout",
			configURI: tomlUri.toString(),
			env: "prod",
			verb: "pause",
			deleteEmitted: false,
		});
		expect(res.outcome).toBe("paused");
		expect(res.target).toBe("prod");
	});

	it("forwards deleteEmitted when set", async () => {
		const calls: { params: unknown }[] = [];
		mockClient = {
			sendRequest: (_method, params) => {
				calls.push({ params });
				return Promise.resolve({
					name: "checkout",
					outcome: "deleted",
					target: "prod",
				});
			},
		};
		await requestOperateProjection({
			name: "checkout",
			tomlUri,
			env: "prod",
			verb: "delete",
			deleteEmitted: true,
		});
		expect((calls[0]?.params as { deleteEmitted: boolean }).deleteEmitted).toBe(
			true,
		);
	});

	it("defaults a missing target to empty", async () => {
		mockClient = {
			sendRequest: () =>
				Promise.resolve({ name: "checkout", outcome: "paused" }),
		};
		const res = await requestOperateProjection({
			name: "checkout",
			tomlUri,
			env: "prod",
			verb: "pause",
		});
		expect(res.target).toBe("");
	});

	it("throws LspUnavailableError on a malformed result", async () => {
		mockClient = {
			// missing the required `name`
			sendRequest: () => Promise.resolve({ outcome: "paused" }),
		};
		await expect(
			requestOperateProjection({
				name: "checkout",
				tomlUri,
				env: "prod",
				verb: "pause",
			}),
		).rejects.toBeInstanceOf(LspUnavailableError);
	});
});
