import { beforeEach, describe, expect, it, vi } from "vitest";
import * as v from "valibot";

// Mutable stand-in for the language client. The `mock` prefix lets the hoisted
// vi.mock factory reference it.
let mockClient:
	| { sendRequest: (method: string, params: unknown) => Promise<unknown> }
	| undefined;
vi.mock("./client.js", () => ({
	getLanguageClient: () => mockClient,
}));

import {
	LSP_AUTH_REQUIRED,
	LspAuthRequiredError,
	LspUnavailableError,
	requestError,
	sendGafferRequest,
} from "./request.js";

const Schema = v.object({ name: v.string() });

describe("requestError", () => {
	it("maps the server's auth code to LspAuthRequiredError", () => {
		expect(
			requestError({ code: LSP_AUTH_REQUIRED, message: "nope" }),
		).toBeInstanceOf(LspAuthRequiredError);
	});

	it("maps any other rejection to LspUnavailableError with the message", () => {
		expect(requestError({ code: -32603, message: "boom" })).toBeInstanceOf(
			LspUnavailableError,
		);
		const plain = requestError(new Error("connection refused"));
		expect(plain).toBeInstanceOf(LspUnavailableError);
		expect(plain.message).toBe("connection refused");
		expect(requestError("weird").message).toBe("weird");
	});
});

describe("sendGafferRequest", () => {
	beforeEach(() => {
		mockClient = undefined;
	});

	it("throws LspUnavailableError when the language server isn't running", async () => {
		mockClient = undefined;
		await expect(
			sendGafferRequest("gaffer/x", {}, Schema),
		).rejects.toBeInstanceOf(LspUnavailableError);
	});

	it("forwards the method and params and returns the validated result", async () => {
		const calls: { method: string; params: unknown }[] = [];
		mockClient = {
			sendRequest: (method, params) => {
				calls.push({ method, params });
				return Promise.resolve({ name: "ok" });
			},
		};
		const res = await sendGafferRequest("gaffer/x", { a: 1 }, Schema);
		expect(calls).toEqual([{ method: "gaffer/x", params: { a: 1 } }]);
		expect(res.name).toBe("ok");
	});

	it("maps the auth error code to LspAuthRequiredError", async () => {
		mockClient = {
			sendRequest: () => Promise.reject({ code: LSP_AUTH_REQUIRED }),
		};
		await expect(
			sendGafferRequest("gaffer/x", {}, Schema),
		).rejects.toBeInstanceOf(LspAuthRequiredError);
	});

	it("maps any other rejection to LspUnavailableError", async () => {
		mockClient = { sendRequest: () => Promise.reject(new Error("dropped")) };
		await expect(
			sendGafferRequest("gaffer/x", {}, Schema),
		).rejects.toBeInstanceOf(LspUnavailableError);
	});

	it("throws LspUnavailableError on a malformed response", async () => {
		mockClient = { sendRequest: () => Promise.resolve({ wrong: true }) };
		await expect(
			sendGafferRequest("gaffer/x", {}, Schema),
		).rejects.toBeInstanceOf(LspUnavailableError);
	});
});
