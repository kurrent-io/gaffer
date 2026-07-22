import { afterEach, beforeEach, describe, expect, it } from "vitest";
import * as vscode from "vscode";
import {
	DeployPlanView,
	type DeployPlanContext,
	type DeployPlanHandlers,
} from "./deploy-plan.js";
import type { PlanReport } from "../commands/deploy-plan.js";
import { getState, resetVscode } from "../../test/testutil/vscode-state.js";
import type { FakeWebviewPanel } from "../../test/__mocks__/vscode.js";

const tomlUri = vscode.Uri.parse("file:///p/gaffer.toml");
const report: PlanReport = {
	env: "staging",
	verdict: "deployable",
	changes: 1,
	plan: [{ name: "a", outcome: "created" }],
};

beforeEach(() => {
	resetVscode();
});
afterEach(() => {
	resetVscode();
});

function makeView(handlers: Partial<DeployPlanHandlers> = {}): DeployPlanView {
	return new DeployPlanView({
		onDiff: handlers.onDiff ?? (() => {}),
		onDeploy: handlers.onDeploy ?? (() => {}),
	});
}

function panels(): readonly FakeWebviewPanel[] {
	return getState().webviewPanels;
}

function onlyPanel(): FakeWebviewPanel {
	const ps = panels();
	expect(ps).toHaveLength(1);
	const p = ps[0];
	if (!p) throw new Error("expected a panel");
	return p;
}

// The token from the panel's most recent "plan" message, echoed by a Deploy
// click so the host can drop a stale one.
function planToken(): number {
	const posted = onlyPanel().webview.postedMessages;
	for (let i = posted.length - 1; i >= 0; i--) {
		const m = posted[i] as { type?: unknown; token?: unknown };
		if (m && m.type === "plan" && typeof m.token === "number") return m.token;
	}
	return -1;
}

describe("DeployPlanView", () => {
	it("creates one panel and posts the plan", () => {
		makeView().show(report, { env: "staging", tomlUri });
		const p = onlyPanel();
		expect(p.title).toBe("Deploy plan: staging");
		expect(p.webview.html).toContain("acquireVsCodeApi");
		expect(p.webview.postedMessages).toContainEqual({
			type: "plan",
			report,
			token: 1,
		});
	});

	it("titles the tab by the projection name when the plan is scoped", () => {
		makeView().show(report, { env: "staging", tomlUri, name: "orders" });
		expect(onlyPanel().title).toBe("Deploy plan: orders");
	});

	it("reuses the panel on a re-preview and re-renders in place", () => {
		const view = makeView();
		view.show(report, { env: "staging", tomlUri });
		view.show({ ...report, env: "prod" }, { env: "prod", tomlUri });
		const p = onlyPanel();
		expect(p.title).toBe("Deploy plan: prod");
		expect(p.revealCount).toBe(2);
		expect(p.webview.postedMessages).toHaveLength(2);
	});

	it("dispatches a diff request from a row's Diff button", () => {
		const diffed: { ctx: DeployPlanContext; name: string }[] = [];
		makeView({ onDiff: (ctx, name) => diffed.push({ ctx, name }) }).show(
			report,
			{
				env: "staging",
				tomlUri,
			},
		);
		onlyPanel().webview.emitMessage({ command: "diff", name: "a" });
		expect(diffed).toEqual([{ ctx: { env: "staging", tomlUri }, name: "a" }]);
	});

	it("dispatches deploy with the report and bypass flag, and streams back", () => {
		const calls: {
			ctx: DeployPlanContext;
			rep: PlanReport;
			noValidate: boolean;
		}[] = [];
		makeView({
			onDeploy: (ctx, rep, noValidate, send) => {
				calls.push({ ctx, rep, noValidate });
				send({ type: "deploy-started" });
			},
		}).show(report, { env: "staging", tomlUri });
		onlyPanel().webview.emitMessage({
			command: "deploy",
			noValidate: true,
			token: planToken(),
		});
		expect(calls).toEqual([
			{ ctx: { env: "staging", tomlUri }, rep: report, noValidate: true },
		]);
		// The send callback posts progress back to this panel's webview.
		expect(onlyPanel().webview.postedMessages).toContainEqual({
			type: "deploy-started",
		});
	});

	it("drops a second deploy while one is in flight, then allows one after it settles", () => {
		let sendFn: ((m: unknown) => void) | undefined;
		let calls = 0;
		makeView({
			onDeploy: (_ctx, _rep, _nv, send) => {
				calls++;
				sendFn = send as (m: unknown) => void;
			},
		}).show(report, { env: "staging", tomlUri });
		const wv = onlyPanel().webview;
		const deploy = { command: "deploy", noValidate: false, token: planToken() };
		wv.emitMessage(deploy);
		wv.emitMessage(deploy); // second, mid-flight -> ignored
		expect(calls).toBe(1);
		// Settle, then a fresh deploy is accepted.
		sendFn?.({ type: "deploy-done", summary: {} });
		wv.emitMessage(deploy);
		expect(calls).toBe(2);
	});

	it("drops a deploy whose plan token is stale", () => {
		let calls = 0;
		makeView({ onDeploy: () => calls++ }).show(report, {
			env: "staging",
			tomlUri,
		});
		onlyPanel().webview.emitMessage({
			command: "deploy",
			noValidate: false,
			token: planToken() + 1, // stale: the panel has re-rendered since
		});
		expect(calls).toBe(0);
	});

	it("closes the panel on cancel", () => {
		makeView().show(report, { env: "staging", tomlUri });
		const p = onlyPanel();
		p.webview.emitMessage({ command: "cancel" });
		expect(p.disposed).toBe(true);
	});

	it("ignores malformed webview messages", () => {
		const seen: unknown[] = [];
		makeView({
			onDiff: (ctx, name) => seen.push({ ctx, name }),
			onDeploy: (ctx) => seen.push(ctx),
		}).show(report, { env: "staging", tomlUri });
		const wv = onlyPanel().webview;
		wv.emitMessage({ command: "diff" }); // no name
		wv.emitMessage({ command: "other", name: "a" });
		wv.emitMessage(null);
		expect(seen).toHaveLength(0);
	});

	it("recreates the panel after the user closes it", () => {
		const view = makeView();
		view.show(report, { env: "staging", tomlUri });
		onlyPanel().emitDispose();
		view.show(report, { env: "staging", tomlUri });
		expect(panels()).toHaveLength(2);
	});
});
