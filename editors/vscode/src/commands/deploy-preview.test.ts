import { afterEach, beforeEach, describe, expect, it } from "vitest";
import * as vscode from "vscode";
import { deployPreview, type DryRunResult } from "./deploy-preview.js";
import type { PlanReport } from "./deploy-plan.js";
import type { DeployPlanContext } from "../panels/deploy-plan.js";
import {
	getShownMessages,
	getState,
	queueMessageResponse,
	resetVscode,
	setTrusted,
} from "../../test/testutil/vscode-state.js";

const tomlUri = vscode.Uri.file("/proj/gaffer.toml");

const okPlan = JSON.stringify({
	env: "staging",
	verdict: "deployable",
	changes: 1,
	plan: [{ name: "a", outcome: "created" }],
});

function fakeView(): {
	shows: { report: PlanReport; ctx: DeployPlanContext }[];
	view: { show: (report: PlanReport, ctx: DeployPlanContext) => void };
} {
	const shows: { report: PlanReport; ctx: DeployPlanContext }[] = [];
	return {
		shows,
		view: { show: (report, ctx) => shows.push({ report, ctx }) },
	};
}

beforeEach(() => {
	setTrusted(true);
});
afterEach(() => {
	resetVscode();
});

describe("deployPreview", () => {
	it("renders the plan on a successful dry-run", async () => {
		const { shows, view } = fakeView();
		const calls: { env: string; cwd: string }[] = [];
		await deployPreview({
			view,
			runDryRun: (env, cwd) => {
				calls.push({ env, cwd });
				return Promise.resolve({ ok: true, stdout: okPlan, code: 2 });
			},
		})({ env: "staging", tomlUri });

		expect(calls).toEqual([{ env: "staging", cwd: "/proj" }]);
		expect(shows).toHaveLength(1);
		expect(shows[0]?.report.verdict).toBe("deployable");
		expect(shows[0]?.ctx).toEqual({ env: "staging", tomlUri });
	});

	it("scopes the dry-run and the plan context to a named projection", async () => {
		const { shows, view } = fakeView();
		const calls: { env: string; name: string | undefined }[] = [];
		await deployPreview({
			view,
			runDryRun: (env, _cwd, name) => {
				calls.push({ env, name });
				return Promise.resolve({ ok: true, stdout: okPlan, code: 2 });
			},
		})({ env: "staging", tomlUri, name: "a" });

		expect(calls).toEqual([{ env: "staging", name: "a" }]);
		expect(shows[0]?.ctx).toEqual({ env: "staging", tomlUri, name: "a" });
	});

	it("does nothing in an untrusted workspace", async () => {
		setTrusted(false);
		const { shows, view } = fakeView();
		let ran = false;
		await deployPreview({
			view,
			runDryRun: () => {
				ran = true;
				return Promise.resolve({ ok: true, stdout: okPlan, code: 0 });
			},
		})({ env: "staging", tomlUri });
		expect(ran).toBe(false);
		expect(shows).toHaveLength(0);
	});

	it("shows an error toast on a spawn failure", async () => {
		const { shows, view } = fakeView();
		const failed: DryRunResult = { ok: false, err: new Error("boom") };
		await deployPreview({ view, runDryRun: () => Promise.resolve(failed) })({
			env: "staging",
			tomlUri,
		});
		expect(shows).toHaveLength(0);
		expect(
			getShownMessages().some(
				(m) => m.kind === "error" && m.message.includes("boom"),
			),
		).toBe(true);
	});

	it("offers sign-in on the auth exit code and dispatches it when accepted", async () => {
		const { view } = fakeView();
		queueMessageResponse("Sign in");
		await deployPreview({
			view,
			runDryRun: () => Promise.resolve({ ok: true, stdout: "", code: 4 }),
		})({ env: "staging", tomlUri });
		const signIn = getState().executeCommandCalls.find(
			(c) => c.name === "gaffer.signIn",
		);
		expect(signIn?.args[0]).toEqual({ env: "staging", tomlUri });
	});

	it("does not sign in when the auth prompt is dismissed", async () => {
		const { view } = fakeView();
		await deployPreview({
			view,
			runDryRun: () => Promise.resolve({ ok: true, stdout: "", code: 4 }),
		})({ env: "staging", tomlUri });
		expect(
			getState().executeCommandCalls.some((c) => c.name === "gaffer.signIn"),
		).toBe(false);
	});

	it("shows an error when the plan can't be parsed", async () => {
		const { shows, view } = fakeView();
		await deployPreview({
			view,
			runDryRun: () =>
				Promise.resolve({ ok: true, stdout: "garbage", code: 1 }),
		})({ env: "staging", tomlUri });
		expect(shows).toHaveLength(0);
		expect(getShownMessages().some((m) => m.kind === "error")).toBe(true);
	});
});
