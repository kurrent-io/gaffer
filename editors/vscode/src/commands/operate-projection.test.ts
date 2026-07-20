import { afterEach, beforeEach, describe, expect, it } from "vitest";
import * as vscode from "vscode";
import {
	operateProjection,
	type OperateProjectionDeps,
} from "./operate-projection.js";
import { LspAuthRequiredError, LspUnavailableError } from "../lsp/request.js";
import type { OperateResult, OperateVerb } from "../lsp/operate.js";
import {
	getShownMessages,
	getState,
	queueInputBox,
	queueMessageResponse,
	resetVscode,
	setTrusted,
} from "../../test/testutil/vscode-state.js";

const tomlUri = vscode.Uri.parse("file:///p/sub/gaffer.toml");

beforeEach(() => {
	setTrusted(true);
});
afterEach(() => {
	resetVscode();
});

function fakeRequest(
	outcome: { resolve: OperateResult } | { reject: unknown },
): {
	calls: Array<{ verb: OperateVerb; deleteEmitted: boolean }>;
	deps: OperateProjectionDeps;
} {
	const calls: Array<{ verb: OperateVerb; deleteEmitted: boolean }> = [];
	return {
		calls,
		deps: {
			request: (a) => {
				calls.push({ verb: a.verb, deleteEmitted: a.deleteEmitted ?? false });
				return "reject" in outcome
					? Promise.reject(outcome.reject)
					: Promise.resolve(outcome.resolve);
			},
		},
	};
}

function warnings(): string[] {
	return getShownMessages()
		.filter((m) => m.kind === "warning")
		.map((m) => m.message);
}

describe("operateProjection confirm tiers", () => {
	it("runs silently for a non-prod reversible verb, then toasts", async () => {
		const { calls, deps } = fakeRequest({
			resolve: { name: "checkout", outcome: "paused", target: "local-cluster" },
		});
		await operateProjection(deps)({
			name: "checkout",
			tomlUri,
			env: "local",
			verb: "pause",
			production: false,
		});
		expect(calls).toEqual([{ verb: "pause", deleteEmitted: false }]);
		expect(warnings()).toHaveLength(0);
		const msgs = getShownMessages();
		expect(msgs).toHaveLength(1);
		expect(msgs[0]?.kind).toBe("info");
		expect(msgs[0]?.message).toBe("Paused checkout on local-cluster.");
	});

	it("modal-confirms a reversible verb on production and proceeds on accept", async () => {
		const { calls, deps } = fakeRequest({
			resolve: { name: "checkout", outcome: "paused", target: "prod" },
		});
		queueMessageResponse("Pause"); // the modal accept button
		await operateProjection(deps)({
			name: "checkout",
			tomlUri,
			env: "prod",
			verb: "pause",
			production: true,
		});
		expect(calls).toHaveLength(1);
		expect(warnings().some((m) => m.includes("PRODUCTION [prod]"))).toBe(true);
	});

	it("does not run when the modal confirm is cancelled", async () => {
		const { calls, deps } = fakeRequest({
			resolve: { name: "checkout", outcome: "paused", target: "prod" },
		});
		// No queued response -> showWarningMessage resolves undefined (cancelled).
		await operateProjection(deps)({
			name: "checkout",
			tomlUri,
			env: "prod",
			verb: "pause",
			production: true,
		});
		expect(calls).toEqual([]);
	});

	it("modal-confirms a no-undo verb off production (delete, non-prod)", async () => {
		const { calls, deps } = fakeRequest({
			resolve: { name: "checkout", outcome: "deleted", target: "local" },
		});
		queueMessageResponse("Delete");
		await operateProjection(deps)({
			name: "checkout",
			tomlUri,
			env: "local",
			verb: "delete",
			production: false,
		});
		expect(calls).toHaveLength(1);
	});

	it("requires typing the name for a no-undo verb on production", async () => {
		const { calls, deps } = fakeRequest({
			resolve: { name: "checkout", outcome: "deleted", target: "prod" },
		});
		queueInputBox("checkout"); // correct name typed
		await operateProjection(deps)({
			name: "checkout",
			tomlUri,
			env: "prod",
			verb: "delete",
			production: true,
		});
		expect(calls).toEqual([{ verb: "delete", deleteEmitted: false }]);
		expect(getState().inputBoxCalls).toHaveLength(1);
	});

	it("does not run when the typed name doesn't match", async () => {
		const { calls, deps } = fakeRequest({
			resolve: { name: "checkout", outcome: "deleted", target: "prod" },
		});
		queueInputBox("wrong");
		await operateProjection(deps)({
			name: "checkout",
			tomlUri,
			env: "prod",
			verb: "delete",
			production: true,
		});
		expect(calls).toEqual([]);
	});

	it("does not run a reversible verb silently when production is unknown", async () => {
		const { calls, deps } = fakeRequest({
			resolve: { name: "checkout", outcome: "paused", target: "stg" },
		});
		// production undefined -> must confirm; no queued response -> cancelled.
		await operateProjection(deps)({
			name: "checkout",
			tomlUri,
			env: "stg",
			verb: "pause",
			production: undefined,
		});
		expect(calls).toEqual([]);
	});

	it("confirms (accept modal) a reversible verb when production is unknown", async () => {
		const { calls, deps } = fakeRequest({
			resolve: { name: "checkout", outcome: "paused", target: "stg" },
		});
		queueMessageResponse("Pause");
		await operateProjection(deps)({
			name: "checkout",
			tomlUri,
			env: "stg",
			verb: "pause",
			production: undefined,
		});
		expect(calls).toHaveLength(1);
		expect(warnings()).toHaveLength(1);
	});

	it("surfaces the delete-emitted consequence in the confirm", async () => {
		const { deps } = fakeRequest({
			resolve: { name: "checkout", outcome: "deleted", target: "local" },
		});
		queueMessageResponse("Delete");
		await operateProjection(deps)({
			name: "checkout",
			tomlUri,
			env: "local",
			verb: "delete",
			production: false,
			deleteEmitted: true,
		});
		expect(warnings().some((m) => m.includes("streams it emitted"))).toBe(true);
	});
});

describe("operateProjection failures", () => {
	it("offers sign-in on an auth failure and routes the choice", async () => {
		const { deps } = fakeRequest({ reject: new LspAuthRequiredError() });
		queueMessageResponse("Sign in");
		await operateProjection(deps)({
			name: "checkout",
			tomlUri,
			env: "prod",
			verb: "pause",
			production: false,
		});
		const signIn = getState().executeCommandCalls.filter(
			(c) => c.name === "gaffer.signIn",
		);
		expect(signIn).toHaveLength(1);
		expect(signIn[0]?.args[0]).toMatchObject({ env: "prod" });
	});

	it("shows the error detail on a non-auth failure", async () => {
		const { deps } = fakeRequest({
			reject: new LspUnavailableError("connection refused"),
		});
		await operateProjection(deps)({
			name: "checkout",
			tomlUri,
			env: "local",
			verb: "pause",
			production: false,
		});
		const errs = getShownMessages().filter((m) => m.kind === "error");
		expect(errs.some((m) => m.message.includes("connection refused"))).toBe(
			true,
		);
	});

	it("does nothing in an untrusted workspace", async () => {
		setTrusted(false);
		const { calls, deps } = fakeRequest({
			resolve: { name: "checkout", outcome: "paused", target: "local" },
		});
		await operateProjection(deps)({
			name: "checkout",
			tomlUri,
			env: "local",
			verb: "pause",
			production: false,
		});
		expect(calls).toEqual([]);
	});
});
