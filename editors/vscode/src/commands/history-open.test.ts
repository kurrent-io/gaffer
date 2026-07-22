import { afterEach, beforeEach, describe, expect, it } from "vitest";
import * as vscode from "vscode";
import {
	makeLoadHistory,
	historyCommand,
	type HistoryOutcome,
	type HistoryOpenDeps,
} from "./history-open.js";
import type { HistoryContext, HistoryView } from "../panels/history-view.js";
import type { HistoryEntry } from "./history-schema.js";
import {
	getShownMessages,
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

beforeEach(() => setTrusted(true));
afterEach(() => resetVscode());

function fakeView() {
	const shown: Array<{ entries: HistoryEntry[]; ctx: HistoryContext }> = [];
	const errors: string[] = [];
	const view = {
		show: (entries: HistoryEntry[], c: HistoryContext) =>
			shown.push({ entries, ctx: c }),
		reportError: (m: string) => errors.push(m),
	} as unknown as HistoryView;
	return { view, shown, errors };
}

function harness(outcome: HistoryOutcome) {
	const { view, shown, errors } = fakeView();
	const deps: HistoryOpenDeps = {
		view,
		runHistory: () => Promise.resolve(outcome),
	};
	return { shown, errors, load: makeLoadHistory(deps) };
}

const LEDGER = JSON.stringify([
	{ version: 4, kind: "deploy", contentHash: "a1b2c3d", enabled: true },
	{ version: 2, kind: "created", contentHash: "b7a8c9d", enabled: true },
]);

describe("makeLoadHistory", () => {
	it("shows the parsed ledger for the projection", async () => {
		const h = harness({ ok: true, stdout: LEDGER });
		await h.load(ctx);
		expect(h.shown).toHaveLength(1);
		expect(h.shown[0]?.entries.map((e) => e.version)).toEqual([4, 2]);
		expect(h.shown[0]?.ctx).toEqual(ctx);
	});

	it("offers sign-in on an auth failure and clears the panel", async () => {
		const h = harness({ ok: false, auth: true, reason: "sign-in required" });
		await h.load(ctx);
		expect(h.shown).toHaveLength(0);
		// The panel is cleared (reportError) so a stale timeline doesn't linger,
		// and a sign-in is offered.
		expect(h.errors[0]).toContain("needs sign-in");
		expect(
			getShownMessages().find((m) => m.kind === "error")?.message,
		).toContain("needs sign-in");
	});

	it("surfaces the real reason on a non-auth failure", async () => {
		const h = harness({
			ok: false,
			auth: false,
			reason: `"orders" is not deployed`,
		});
		await h.load(ctx);
		expect(h.shown).toHaveLength(0);
		expect(h.errors[0]).toContain("is not deployed");
	});

	it("reports an error on unparseable output", async () => {
		const h = harness({ ok: true, stdout: "not json" });
		await h.load(ctx);
		expect(h.shown).toHaveLength(0);
		expect(h.errors[0]).toContain("Couldn't read the history");
	});
});

describe("historyCommand", () => {
	it("builds the context from the command args and loads", async () => {
		const loaded: HistoryContext[] = [];
		await historyCommand((c) => {
			loaded.push(c);
			return Promise.resolve();
		})({ name: "orders", tomlUri, env: "staging", production: true });
		expect(loaded).toEqual([
			{ name: "orders", tomlUri, env: "staging", production: true },
		]);
	});

	it("does nothing in an untrusted workspace", async () => {
		setTrusted(false);
		const loaded: HistoryContext[] = [];
		await historyCommand((c) => {
			loaded.push(c);
			return Promise.resolve();
		})({ name: "orders", tomlUri, env: "staging" });
		expect(loaded).toHaveLength(0);
	});
});
