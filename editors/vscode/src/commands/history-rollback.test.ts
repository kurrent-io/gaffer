import { afterEach, beforeEach, describe, expect, it } from "vitest";
import * as vscode from "vscode";
import {
	rollbackFromHistory,
	type HistoryRollbackDeps,
	type RollbackOutcome,
} from "./history-rollback.js";
import type { HistoryContext, HistoryMessage } from "../panels/history-view.js";
import {
	getShownMessages,
	queueMessageResponse,
	resetVscode,
	setTrusted,
} from "../../test/testutil/vscode-state.js";

const tomlUri = vscode.Uri.parse("file:///p/gaffer.toml");

beforeEach(() => setTrusted(true));
afterEach(() => resetVscode());

function ctx(production: boolean | undefined): HistoryContext {
	return { env: "prod", tomlUri, name: "orders", production };
}

function harness(outcome: RollbackOutcome) {
	const runCalls: Array<{ hash: string }> = [];
	const reloads: HistoryContext[] = [];
	const sent: HistoryMessage[] = [];
	const deps: HistoryRollbackDeps = {
		runRollback: (_cwd, _env, _name, hash) => {
			runCalls.push({ hash });
			return Promise.resolve(outcome);
		},
		reload: (c) => {
			reloads.push(c);
			return Promise.resolve();
		},
	};
	const run = (c: HistoryContext) =>
		rollbackFromHistory(deps)(c, { version: 5, hash: "deadbeefcafe" }, (m) =>
			sent.push(m),
		);
	return { run, runCalls, reloads, sent };
}

describe("rollbackFromHistory confirm tiers", () => {
	it("rolls back silently off-prod, then toasts and reloads", async () => {
		const h = harness({
			ok: true,
			stdout: '{"name":"orders","outcome":"rolled-back","hash":"deadbeefcafe"}',
		});
		await h.run(ctx(false));
		expect(h.runCalls).toHaveLength(1);
		expect(h.reloads).toHaveLength(1);
		expect(h.sent.map((m) => m.type)).toEqual([
			"rollback-active",
			"rollback-done",
		]);
	});

	it("modal-confirms on production and proceeds on accept", async () => {
		queueMessageResponse("Roll back");
		const h = harness({
			ok: true,
			stdout: '{"name":"orders","outcome":"rolled-back","hash":"deadbeefcafe"}',
		});
		await h.run(ctx(true));
		expect(h.runCalls).toHaveLength(1);
		const warn = getShownMessages().find((m) => m.kind === "warning");
		expect(warn?.message).toContain("PRODUCTION [prod]");
	});

	it("cancelling the modal runs nothing and releases the guard", async () => {
		queueMessageResponse(undefined); // dismissed
		const h = harness({ ok: true, stdout: "{}" });
		await h.run(ctx(true));
		expect(h.runCalls).toHaveLength(0);
		expect(h.reloads).toHaveLength(0);
		expect(h.sent).toEqual([
			{ type: "rollback-error", version: 5, message: "" },
		]);
	});

	it("modal-confirms when production is unknown (fails safe)", async () => {
		queueMessageResponse(undefined);
		const h = harness({ ok: true, stdout: "{}" });
		await h.run(ctx(undefined));
		expect(h.runCalls).toHaveLength(0); // the modal was shown and dismissed
		const warn = getShownMessages().find((m) => m.kind === "warning");
		expect(warn?.message).toContain("prod");
		expect(warn?.message).not.toContain("PRODUCTION");
	});
});

describe("rollbackFromHistory outcomes", () => {
	it("reports an unchanged rollback without claiming a move", async () => {
		const h = harness({
			ok: true,
			stdout: '{"name":"orders","outcome":"unchanged","hash":"deadbeefcafe"}',
		});
		await h.run(ctx(false));
		const info = getShownMessages().find((m) => m.kind === "info");
		expect(info?.message).toContain("already at that version");
		expect(h.reloads).toHaveLength(1);
	});

	it("surfaces a refusal reason and does not reload", async () => {
		const h = harness({
			ok: false,
			auth: false,
			reason: "can't change engine version in place; use recreate",
		});
		await h.run(ctx(false));
		const err = getShownMessages().find((m) => m.kind === "error");
		expect(err?.message).toContain("use recreate");
		expect(h.reloads).toHaveLength(0);
		expect(h.sent.at(-1)).toMatchObject({ type: "rollback-error" });
	});

	it("offers sign-in on an auth failure", async () => {
		const h = harness({ ok: false, auth: true, reason: "auth required" });
		await h.run(ctx(false));
		const err = getShownMessages().find((m) => m.kind === "error");
		expect(err?.message).toContain("needs sign-in");
		expect(h.sent.at(-1)).toMatchObject({ type: "rollback-error" });
	});

	it("errors on an unparseable rollback result", async () => {
		const h = harness({ ok: true, stdout: "not json" });
		await h.run(ctx(false));
		const err = getShownMessages().find((m) => m.kind === "error");
		expect(err?.message).toContain("Couldn't read the rollback result");
		expect(h.reloads).toHaveLength(0);
	});

	it("does nothing in an untrusted workspace", async () => {
		setTrusted(false);
		const h = harness({ ok: true, stdout: "{}" });
		await h.run(ctx(false));
		expect(h.runCalls).toHaveLength(0);
	});
});
