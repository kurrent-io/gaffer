import { afterEach, beforeEach, describe, expect, it } from "vitest";
import * as vscode from "vscode";
import {
	openHistoryDiff,
	HistoryDiffContentProvider,
	type HistoryDiffDeps,
} from "./history-diff.js";
import type { ProjectionDiff } from "../lsp/diff.js";
import { LspAuthRequiredError, LspUnavailableError } from "../lsp/request.js";
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

function harness(outcome: { resolve: ProjectionDiff } | { reject: unknown }) {
	const provider = new HistoryDiffContentProvider();
	const calls: Array<{ left: string; right: string }> = [];
	const deps: HistoryDiffDeps = {
		provider,
		requestDiff: (_name, _uri, _env, left, right) => {
			calls.push({ left, right });
			return "reject" in outcome
				? Promise.reject(outcome.reject)
				: Promise.resolve(outcome.resolve);
		},
	};
	return { provider, calls, run: () => openHistoryDiff(deps)(ctx, req) };
}

function diffCalls() {
	return getState().executeCommandCalls.filter((c) => c.name === "vscode.diff");
}

describe("openHistoryDiff", () => {
	it("opens the native diff with both sources over the LSP", async () => {
		const h = harness({
			resolve: {
				name: "orders",
				left: { ref: "version", hash: "1d77f5a", source: "OLD" },
				right: { ref: "local", source: "NEW" },
			},
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

	it("offers sign-in on an auth error and does not open a diff", async () => {
		const h = harness({ reject: new LspAuthRequiredError() });
		await h.run();
		expect(diffCalls()).toHaveLength(0);
		expect(
			getShownMessages().find((m) => m.kind === "error")?.message,
		).toContain("needs sign-in");
	});

	it("reports a generic failure on any other error", async () => {
		const h = harness({ reject: new LspUnavailableError("boom") });
		await h.run();
		expect(diffCalls()).toHaveLength(0);
		expect(
			getShownMessages().find((m) => m.kind === "error")?.message,
		).toContain("Couldn't diff");
	});

	it("does nothing in an untrusted workspace", async () => {
		setTrusted(false);
		const h = harness({
			resolve: {
				name: "orders",
				left: { ref: "version", source: "" },
				right: { ref: "local", source: "" },
			},
		});
		await h.run();
		expect(h.calls).toHaveLength(0);
		expect(diffCalls()).toHaveLength(0);
	});
});

describe("HistoryDiffContentProvider", () => {
	it("bounds the cache, evicting the oldest comparison", () => {
		const provider = new HistoryDiffContentProvider();
		const first = provider.setSides("e", "p", "h0a", "L0", "h0b", "R0");
		// 60 more comparisons (61 total = 122 sides) tips past the ~120-entry cap,
		// so the oldest-inserted comparison is evicted.
		for (let i = 1; i < 61; i++) {
			provider.setSides("e", "p", `h${i}a`, `L${i}`, `h${i}b`, `R${i}`);
		}
		expect(provider.provideTextDocumentContent(first.left)).toBe("");
		expect(provider.provideTextDocumentContent(first.right)).toBe("");
		// A recent comparison is still served.
		const recent = provider.setSides("e", "p", "hZa", "LZ", "hZb", "RZ");
		expect(provider.provideTextDocumentContent(recent.left)).toBe("LZ");
	});
});
