import { afterEach, beforeEach, describe, expect, it } from "vitest";
import * as vscode from "vscode";
import {
	diffProjection,
	GafferDiffContentProvider,
	GAFFER_DIFF_SCHEME,
	type DiffProjectionDeps,
} from "./diff-projection.js";
import { type ProjectionDiff } from "../lsp/diff.js";
import { LspAuthRequiredError, LspUnavailableError } from "../lsp/request.js";
import {
	getShownMessages,
	getState,
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

// A drifted diff with distinct sources.
function diffedResult(): ProjectionDiff {
	return {
		name: "checkout",
		left: { ref: "deployed", hash: "aaa", source: "OLD" },
		right: { ref: "local", hash: "bbb", source: "NEW" },
		verdict: { drift: "drifted" },
	};
}

// fakeRequest records its calls and resolves or rejects with the given outcome.
function fakeRequest(
	outcome: { resolve: ProjectionDiff } | { reject: unknown },
): {
	calls: { name: string; env: string }[];
	requestDiff: DiffProjectionDeps["requestDiff"];
} {
	const calls: { name: string; env: string }[] = [];
	return {
		calls,
		requestDiff: (name, _tomlUri, env) => {
			calls.push({ name, env });
			return "reject" in outcome
				? Promise.reject(outcome.reject)
				: Promise.resolve(outcome.resolve);
		},
	};
}

function diffCommandCalls() {
	return getState().executeCommandCalls.filter((c) => c.name === "vscode.diff");
}

describe("GafferDiffContentProvider", () => {
	it("serves stored sources and fires change for both sides", () => {
		const p = new GafferDiffContentProvider();
		const fired: string[] = [];
		p.onDidChange((u) => fired.push(u.toString()));
		const { left, right } = p.setSides("checkout", "prod", "OLD", "NEW");
		expect(left.scheme).toBe(GAFFER_DIFF_SCHEME);
		expect(p.provideTextDocumentContent(left)).toBe("OLD");
		expect(p.provideTextDocumentContent(right)).toBe("NEW");
		expect(fired).toEqual([left.toString(), right.toString()]);
		// Unknown URI serves empty rather than undefined.
		expect(
			p.provideTextDocumentContent(vscode.Uri.parse("gaffer-diff:/x/y")),
		).toBe("");
	});

	it("keeps the two envs' documents distinct", () => {
		const p = new GafferDiffContentProvider();
		const prod = p.setSides("checkout", "prod", "P", "L");
		const stg = p.setSides("checkout", "staging", "S", "L");
		expect(prod.left.toString()).not.toBe(stg.left.toString());
		expect(p.provideTextDocumentContent(prod.left)).toBe("P");
		expect(p.provideTextDocumentContent(stg.left)).toBe("S");
	});
});

describe("diffProjection", () => {
	it("opens the native diff with both sources", async () => {
		const provider = new GafferDiffContentProvider();
		const { calls, requestDiff } = fakeRequest({ resolve: diffedResult() });
		await diffProjection({ provider, requestDiff })({
			name: "checkout",
			tomlUri,
			env: "prod",
		});
		expect(calls).toEqual([{ name: "checkout", env: "prod" }]);
		const diffs = diffCommandCalls();
		expect(diffs).toHaveLength(1);
		const [left, right, title] = diffs[0]?.args as [
			vscode.Uri,
			vscode.Uri,
			string,
		];
		expect(provider.provideTextDocumentContent(left)).toBe("OLD");
		expect(provider.provideTextDocumentContent(right)).toBe("NEW");
		expect(title).toBe("checkout: deployed ↔ local (prod)");
	});

	it("shows a message and opens no diff when the projection isn't deployed", async () => {
		const provider = new GafferDiffContentProvider();
		const { requestDiff } = fakeRequest({
			resolve: {
				name: "checkout",
				left: { ref: "deployed", source: "" },
				right: { ref: "local", source: "NEW" },
				verdict: { drift: "not-deployed" },
			},
		});
		await diffProjection({ provider, requestDiff })({
			name: "checkout",
			tomlUri,
			env: "prod",
		});
		expect(diffCommandCalls()).toHaveLength(0);
		const msgs = getShownMessages();
		expect(msgs).toHaveLength(1);
		expect(msgs[0]?.kind).toBe("info");
		expect(msgs[0]?.message).toContain("isn't deployed to prod");
	});

	it("offers sign-in on an auth failure and routes the choice", async () => {
		const provider = new GafferDiffContentProvider();
		const { requestDiff } = fakeRequest({
			reject: new LspAuthRequiredError(),
		});
		queueMessageResponse("Sign in");
		await diffProjection({ provider, requestDiff })({
			name: "checkout",
			tomlUri,
			env: "prod",
		});
		expect(diffCommandCalls()).toHaveLength(0);
		const msgs = getShownMessages();
		expect(msgs[0]?.kind).toBe("error");
		expect(msgs[0]?.items).toContain("Sign in");
		const signInCalls = getState().executeCommandCalls.filter(
			(c) => c.name === "gaffer.signIn",
		);
		expect(signInCalls).toHaveLength(1);
		expect(signInCalls[0]?.args[0]).toMatchObject({ env: "prod" });
	});

	it("does not route sign-in when the error toast is dismissed", async () => {
		const provider = new GafferDiffContentProvider();
		const { requestDiff } = fakeRequest({
			reject: new LspAuthRequiredError(),
		});
		// No queued response → showErrorMessage resolves undefined (dismissed).
		await diffProjection({ provider, requestDiff })({
			name: "checkout",
			tomlUri,
			env: "prod",
		});
		expect(
			getState().executeCommandCalls.filter((c) => c.name === "gaffer.signIn"),
		).toHaveLength(0);
	});

	it("shows the error detail on a non-auth failure", async () => {
		const provider = new GafferDiffContentProvider();
		const { requestDiff } = fakeRequest({
			reject: new LspUnavailableError("connection refused"),
		});
		await diffProjection({ provider, requestDiff })({
			name: "checkout",
			tomlUri,
			env: "prod",
		});
		expect(diffCommandCalls()).toHaveLength(0);
		const msgs = getShownMessages();
		expect(msgs[0]?.kind).toBe("error");
		expect(msgs[0]?.message).toContain("connection refused");
	});

	it("does nothing in an untrusted workspace", async () => {
		setTrusted(false);
		const provider = new GafferDiffContentProvider();
		const { calls, requestDiff } = fakeRequest({ resolve: diffedResult() });
		await diffProjection({ provider, requestDiff })({
			name: "checkout",
			tomlUri,
			env: "prod",
		});
		expect(calls).toEqual([]);
		expect(diffCommandCalls()).toHaveLength(0);
	});
});
