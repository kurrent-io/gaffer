import { afterEach, beforeEach, describe, expect, it } from "vitest";
import * as vscode from "vscode";
import {
	makeLoadHistory,
	historyCommand,
	type HistoryCapture,
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

function harness(capture: HistoryCapture) {
	const { view, shown, errors } = fakeView();
	const deps: HistoryOpenDeps = {
		view,
		runHistory: () => Promise.resolve(capture),
	};
	return { shown, errors, load: makeLoadHistory(deps) };
}

const LEDGER = JSON.stringify([
	{ version: 4, kind: "deploy", contentHash: "a1b2c3d", enabled: true },
	{ version: 2, kind: "created", contentHash: "b7a8c9d", enabled: true },
]);

describe("makeLoadHistory", () => {
	it("shows the parsed ledger for the projection", async () => {
		const h = harness({ ok: true, code: 0, stdout: LEDGER });
		await h.load(ctx);
		expect(h.shown).toHaveLength(1);
		expect(h.shown[0]?.entries.map((e) => e.version)).toEqual([4, 2]);
		expect(h.shown[0]?.ctx).toEqual(ctx);
	});

	it("offers sign-in on exit code 4 and shows nothing", async () => {
		const h = harness({ ok: true, code: 4, stdout: "" });
		await h.load(ctx);
		expect(h.shown).toHaveLength(0);
		expect(
			getShownMessages().find((m) => m.kind === "error")?.message,
		).toContain("needs sign-in");
	});

	it("reports an error on unparseable output", async () => {
		const h = harness({ ok: true, code: 0, stdout: "not json" });
		await h.load(ctx);
		expect(h.shown).toHaveLength(0);
		expect(h.errors[0]).toContain("Couldn't read the history");
	});

	it("reports an error when the spawn fails", async () => {
		const h = harness({ ok: false, err: "ENOENT" });
		await h.load(ctx);
		expect(h.shown).toHaveLength(0);
		expect(h.errors[0]).toContain("ENOENT");
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
