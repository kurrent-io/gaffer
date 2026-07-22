import { afterEach, beforeEach, describe, expect, it } from "vitest";
import * as vscode from "vscode";
import {
	openHistoryDiff,
	HistoryDiffContentProvider,
	type DiffCapture,
	type HistoryDiffDeps,
} from "./history-diff.js";
import type {
	HistoryContext,
	HistoryDiffRequest,
} from "../panels/history-view.js";
import {
	getShownMessages,
	getState,
	resetVscode,
	setTrusted,
} from "../../test/testutil/vscode-state.js";

const tomlUri = vscode.Uri.parse("file:///p/gaffer.toml");
const ctx: HistoryContext = {
	env: "staging",
	tomlUri,
	name: "orders",
	production: false,
};
const req: HistoryDiffRequest = {
	name: "orders",
	env: "staging",
	left: "1d77f5a",
	right: "local",
	title: "orders: v4 ↔ local",
};

beforeEach(() => setTrusted(true));
afterEach(() => resetVscode());

function harness(capture: DiffCapture) {
	const provider = new HistoryDiffContentProvider();
	const calls: Array<{ left: string; right: string }> = [];
	const deps: HistoryDiffDeps = {
		provider,
		runDiff: (_cwd, _env, _name, left, right) => {
			calls.push({ left, right });
			return Promise.resolve(capture);
		},
	};
	return { provider, calls, run: () => openHistoryDiff(deps)(ctx, req) };
}

function diffCalls() {
	return getState().executeCommandCalls.filter((c) => c.name === "vscode.diff");
}

describe("openHistoryDiff", () => {
	it("opens the native diff with both sources", async () => {
		const h = harness({
			ok: true,
			code: 0,
			stdout: JSON.stringify({
				name: "orders",
				left: { ref: "version", hash: "1d77f5a", source: "OLD" },
				right: { ref: "local", source: "NEW" },
				lines: [],
			}),
		});
		await h.run();
		expect(h.calls).toEqual([{ left: "1d77f5a", right: "local" }]);
		const opened = diffCalls();
		expect(opened).toHaveLength(1);
		const [left, right, title] = opened[0]?.args as [
			vscode.Uri,
			vscode.Uri,
			string,
		];
		expect(h.provider.provideTextDocumentContent(left)).toBe("OLD");
		expect(h.provider.provideTextDocumentContent(right)).toBe("NEW");
		expect(title).toBe("orders: v4 ↔ local");
	});

	it("offers sign-in on exit code 4 and does not open a diff", async () => {
		const h = harness({ ok: true, code: 4, stdout: "" });
		await h.run();
		expect(diffCalls()).toHaveLength(0);
		expect(
			getShownMessages().find((m) => m.kind === "error")?.message,
		).toContain("needs sign-in");
	});

	it("errors on unparseable output", async () => {
		const h = harness({ ok: true, code: 0, stdout: "not json" });
		await h.run();
		expect(diffCalls()).toHaveLength(0);
		expect(
			getShownMessages().find((m) => m.kind === "error")?.message,
		).toContain("Couldn't read the diff");
	});

	it("errors when the spawn fails to run", async () => {
		const h = harness({ ok: false, err: "ENOENT" });
		await h.run();
		expect(diffCalls()).toHaveLength(0);
		expect(
			getShownMessages().find((m) => m.kind === "error")?.message,
		).toContain("ENOENT");
	});

	it("does nothing in an untrusted workspace", async () => {
		setTrusted(false);
		const h = harness({ ok: true, code: 0, stdout: "{}" });
		await h.run();
		expect(h.calls).toHaveLength(0);
		expect(diffCalls()).toHaveLength(0);
	});
});
