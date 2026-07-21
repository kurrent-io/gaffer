import { afterEach, beforeEach, describe, expect, it } from "vitest";
import * as vscode from "vscode";
import { DeployPlanView, type DeployPlanContext } from "./deploy-plan.js";
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

function panels(): readonly FakeWebviewPanel[] {
	return getState().webviewPanels;
}

// Assert exactly one panel exists and return it, non-undefined for the assertions.
function onlyPanel(): FakeWebviewPanel {
	const ps = panels();
	expect(ps).toHaveLength(1);
	const p = ps[0];
	if (!p) throw new Error("expected a panel");
	return p;
}

describe("DeployPlanView", () => {
	it("creates one panel and posts the plan", () => {
		new DeployPlanView(() => {}).show(report, { env: "staging", tomlUri });
		const p = onlyPanel();
		expect(p.title).toBe("Deploy plan: staging");
		expect(p.webview.html).toContain("acquireVsCodeApi");
		expect(p.webview.postedMessages).toContainEqual({ type: "plan", report });
	});

	it("reuses the panel on a re-preview and re-renders in place", () => {
		const view = new DeployPlanView(() => {});
		view.show(report, { env: "staging", tomlUri });
		view.show({ ...report, env: "prod" }, { env: "prod", tomlUri });
		const p = onlyPanel();
		expect(p.title).toBe("Deploy plan: prod");
		expect(p.revealCount).toBe(2);
		expect(p.webview.postedMessages).toHaveLength(2);
	});

	it("dispatches a diff request from a row click", () => {
		const diffed: { ctx: DeployPlanContext; name: string }[] = [];
		new DeployPlanView((ctx, name) => diffed.push({ ctx, name })).show(report, {
			env: "staging",
			tomlUri,
		});
		onlyPanel().webview.emitMessage({ command: "diff", name: "a" });
		expect(diffed).toEqual([{ ctx: { env: "staging", tomlUri }, name: "a" }]);
	});

	it("ignores malformed webview messages", () => {
		const diffed: unknown[] = [];
		new DeployPlanView((ctx, name) => diffed.push({ ctx, name })).show(report, {
			env: "staging",
			tomlUri,
		});
		const wv = onlyPanel().webview;
		wv.emitMessage({ command: "diff" }); // no name
		wv.emitMessage({ command: "other", name: "a" });
		wv.emitMessage(null);
		expect(diffed).toHaveLength(0);
	});

	it("recreates the panel after the user closes it", () => {
		const view = new DeployPlanView(() => {});
		view.show(report, { env: "staging", tomlUri });
		onlyPanel().emitDispose();
		view.show(report, { env: "staging", tomlUri });
		expect(panels()).toHaveLength(2);
	});
});
