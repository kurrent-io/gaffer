import { afterEach, beforeEach, describe, expect, it } from "vitest";
import * as vscode from "vscode";
import {
	diffProjection,
	GafferDiffContentProvider,
	GAFFER_DIFF_SCHEME,
	type DiffProjectionDeps,
} from "./diff-projection.js";
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

// A `gaffer diff --json` payload with distinct sources and a drift verdict.
function diffedJson(): string {
	return JSON.stringify({
		name: "checkout",
		left: { ref: "deployed", hash: "aaa", source: "OLD" },
		right: { ref: "local", hash: "bbb", source: "NEW" },
		verdict: { drift: "drifted" },
	});
}

function fakeRun(result: Awaited<ReturnType<DiffProjectionDeps["run"]>>): {
	calls: { args: string[]; cwd: string }[];
	run: DiffProjectionDeps["run"];
} {
	const calls: { args: string[]; cwd: string }[] = [];
	return {
		calls,
		run: (args, cwd) => {
			calls.push({ args, cwd });
			return Promise.resolve(result);
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
		const { calls, run } = fakeRun({ ok: true, stdout: diffedJson() });
		await diffProjection({ provider, run })({
			name: "checkout",
			tomlUri,
			env: "prod",
		});
		// Spawned with the right argv, in the config's directory. Compute the
		// expected cwd the same way so the assertion adapts to the mock's Uri
		// normalization (the real Uri.parse normalizes leading slashes; the mock
		// doesn't).
		const expectedCwd = vscode.Uri.joinPath(tomlUri, "..").fsPath;
		expect(calls).toEqual([
			{
				args: ["diff", "--env", "prod", "--json", "--", "checkout"],
				cwd: expectedCwd,
			},
		]);
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
		const { run } = fakeRun({
			ok: true,
			stdout: JSON.stringify({
				name: "checkout",
				left: { ref: "deployed", source: "" },
				right: { ref: "local", source: "NEW" },
				verdict: { drift: "not-deployed" },
			}),
		});
		await diffProjection({ provider, run })({
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
		const { run } = fakeRun({
			ok: false,
			err: {
				code: 4,
				cause: {
					stderr: 'env "prod" requires sign-in: run `gaffer auth --env prod`',
				},
			},
		});
		queueMessageResponse("Sign in");
		await diffProjection({ provider, run })({
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
		const { run } = fakeRun({
			ok: false,
			err: { code: 4 },
		});
		// No queued response → showErrorMessage resolves undefined (dismissed).
		await diffProjection({ provider, run })({
			name: "checkout",
			tomlUri,
			env: "prod",
		});
		expect(
			getState().executeCommandCalls.filter((c) => c.name === "gaffer.signIn"),
		).toHaveLength(0);
	});

	it("shows the CLI error on a non-auth failure", async () => {
		const provider = new GafferDiffContentProvider();
		const { run } = fakeRun({
			ok: false,
			err: { cause: { stderr: "connection refused" } },
		});
		await diffProjection({ provider, run })({
			name: "checkout",
			tomlUri,
			env: "prod",
		});
		expect(diffCommandCalls()).toHaveLength(0);
		const msgs = getShownMessages();
		expect(msgs[0]?.kind).toBe("error");
		expect(msgs[0]?.message).toContain("connection refused");
	});

	it("does not offer sign-in on stderr text alone without the auth exit code", async () => {
		const provider = new GafferDiffContentProvider();
		// Message text mentions sign-in, but the exit code isn't the auth code:
		// detection is the code, not the text, so this is a plain error.
		const { run } = fakeRun({
			ok: false,
			err: { code: 1, cause: { stderr: "boom requires sign-in maybe" } },
		});
		await diffProjection({ provider, run })({
			name: "checkout",
			tomlUri,
			env: "prod",
		});
		const msgs = getShownMessages();
		expect(msgs[0]?.items ?? []).not.toContain("Sign in");
		expect(
			getState().executeCommandCalls.filter((c) => c.name === "gaffer.signIn"),
		).toHaveLength(0);
	});

	it("reports an unparseable --json payload", async () => {
		const provider = new GafferDiffContentProvider();
		const { run } = fakeRun({ ok: true, stdout: "not json" });
		await diffProjection({ provider, run })({
			name: "checkout",
			tomlUri,
			env: "prod",
		});
		expect(diffCommandCalls()).toHaveLength(0);
		expect(getShownMessages()[0]?.kind).toBe("error");
	});

	it("does nothing in an untrusted workspace", async () => {
		setTrusted(false);
		const provider = new GafferDiffContentProvider();
		const { calls, run } = fakeRun({ ok: true, stdout: diffedJson() });
		await diffProjection({ provider, run })({
			name: "checkout",
			tomlUri,
			env: "prod",
		});
		expect(calls).toEqual([]);
		expect(diffCommandCalls()).toHaveLength(0);
	});
});
