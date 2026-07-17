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

import {
	DiffAuthRequiredError,
	DiffUnavailableError,
	diffRequestError,
	LSP_AUTH_REQUIRED,
	parseDiffResponse,
	requestProjectionDiff,
} from "./diff.js";

const tomlUri = vscode.Uri.parse("file:///p/sub/gaffer.toml");

describe("diffRequestError", () => {
	it("maps the server's auth code to DiffAuthRequiredError", () => {
		const err = diffRequestError({ code: LSP_AUTH_REQUIRED, message: "nope" });
		expect(err).toBeInstanceOf(DiffAuthRequiredError);
	});

	it("maps any other rejection to DiffUnavailableError with the message", () => {
		const rpc = diffRequestError({ code: -32603, message: "boom" });
		expect(rpc).toBeInstanceOf(DiffUnavailableError);

		const plain = diffRequestError(new Error("connection refused"));
		expect(plain).toBeInstanceOf(DiffUnavailableError);
		expect(plain.message).toBe("connection refused");

		const str = diffRequestError("weird");
		expect(str).toBeInstanceOf(DiffUnavailableError);
		expect(str.message).toBe("weird");
	});
});

describe("parseDiffResponse", () => {
	it("accepts a well-formed payload and passes unmodelled fields through", () => {
		const diff = parseDiffResponse({
			name: "checkout",
			left: { ref: "deployed", hash: "aaa", source: "OLD" },
			right: { ref: "local", source: "NEW" },
			verdict: { drift: "drifted" },
			lines: [{ kind: "equal", text: "x" }],
		});
		expect(diff.name).toBe("checkout");
		expect(diff.left.source).toBe("OLD");
		expect(diff.right.source).toBe("NEW");
		expect(diff.verdict?.drift).toBe("drifted");
	});

	it("defaults a missing source to empty (the not-deployed signal)", () => {
		const diff = parseDiffResponse({
			name: "checkout",
			left: { ref: "deployed" },
			right: { ref: "local", source: "NEW" },
		});
		expect(diff.left.source).toBe("");
	});

	it("throws DiffUnavailableError on a malformed payload", () => {
		expect(() => parseDiffResponse({ name: "checkout" })).toThrow(
			DiffUnavailableError,
		);
		expect(() => parseDiffResponse("not an object")).toThrow(
			DiffUnavailableError,
		);
	});
});

describe("requestProjectionDiff", () => {
	beforeEach(() => {
		mockClient = undefined;
	});

	it("throws DiffUnavailableError when the language server isn't running", async () => {
		mockClient = undefined;
		await expect(
			requestProjectionDiff("checkout", tomlUri, "prod"),
		).rejects.toBeInstanceOf(DiffUnavailableError);
	});

	it("sends gaffer/diffProjection with configURI derived from the toml URI", async () => {
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
		expect(calls).toHaveLength(1);
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

	it("maps the server's auth error code to DiffAuthRequiredError", async () => {
		mockClient = {
			sendRequest: () =>
				Promise.reject({ code: LSP_AUTH_REQUIRED, message: "nope" }),
		};
		await expect(
			requestProjectionDiff("checkout", tomlUri, "prod"),
		).rejects.toBeInstanceOf(DiffAuthRequiredError);
	});

	it("maps any other rejection to DiffUnavailableError", async () => {
		mockClient = {
			sendRequest: () => Promise.reject(new Error("connection dropped")),
		};
		await expect(
			requestProjectionDiff("checkout", tomlUri, "prod"),
		).rejects.toBeInstanceOf(DiffUnavailableError);
	});

	it("throws DiffUnavailableError on a malformed response", async () => {
		mockClient = {
			sendRequest: () => Promise.resolve({ name: "checkout" }),
		};
		await expect(
			requestProjectionDiff("checkout", tomlUri, "prod"),
		).rejects.toBeInstanceOf(DiffUnavailableError);
	});
});
