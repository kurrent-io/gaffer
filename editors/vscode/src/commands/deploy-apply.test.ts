import { afterEach, beforeEach, describe, expect, it } from "vitest";
import * as vscode from "vscode";
import { deployApply, type DeployApplyDeps } from "./deploy-apply.js";
import type { PlanReport } from "./deploy-plan.js";
import type { DeployPlanMessage } from "../panels/deploy-plan.js";
import type { CliMessage } from "../ipc/schemas.js";
import {
	getState,
	queueInputBox,
	queueMessageResponse,
	resetVscode,
	setTrusted,
} from "../../test/testutil/vscode-state.js";

const tomlUri = vscode.Uri.file("/proj/gaffer.toml");
const ctx = { env: "staging", tomlUri };

function plan(production: boolean | undefined, outcomes: string[]): PlanReport {
	return {
		env: "staging",
		verdict: "deployable",
		changes: outcomes.length,
		production,
		plan: outcomes.map((outcome, i) => ({ name: "p" + i, outcome })),
	};
}

function summary(failed = 0): CliMessage {
	return {
		type: "deploy_summary",
		created: 0,
		updated: 0,
		rebuilt: 0,
		skipped: 0,
		refused: 0,
		invalid: 0,
		failed,
	};
}

// A run that replays fixed NDJSON lines then exits, recording its call.
function fakeRun(lines: CliMessage[], code: number | null) {
	const calls: { env: string; cwd: string; noValidate: boolean }[] = [];
	const run: DeployApplyDeps["run"] = (env, cwd, noValidate, handlers) => {
		calls.push({ env, cwd, noValidate });
		for (const l of lines) handlers.onLine(l);
		handlers.onExit(code);
	};
	return { calls, run };
}

const flush = () => new Promise((r) => setTimeout(r, 0));

beforeEach(() => {
	setTrusted(true);
});
afterEach(() => {
	resetVscode();
});

describe("deployApply", () => {
	it("deploys silently for a non-prod plan with no rebuild, streaming progress", async () => {
		const sent: DeployPlanMessage[] = [];
		const { calls, run } = fakeRun(
			[
				{ type: "deploy_start", name: "p0", index: 1, total: 1 },
				{ type: "deploy_result", name: "p0", outcome: "created" },
				summary(0),
			],
			0,
		);
		await deployApply({ run })(ctx, plan(false, ["created"]), false, (m) =>
			sent.push(m),
		);
		expect(calls).toEqual([
			{ env: "staging", cwd: "/proj", noValidate: false },
		]);
		expect(sent).toEqual([
			{ type: "deploy-started" },
			{ type: "deploy-active", name: "p0" },
			{ type: "deploy-item", name: "p0", outcome: "created" },
			{
				type: "deploy-done",
				summary: {
					created: 0,
					updated: 0,
					rebuilt: 0,
					skipped: 0,
					refused: 0,
					invalid: 0,
					failed: 0,
				},
			},
		]);
	});

	it("threads the bypass flag through to the run", async () => {
		const { calls, run } = fakeRun([summary(0)], 0);
		await deployApply({ run })(ctx, plan(false, ["created"]), true, () => {});
		expect(calls[0]?.noValidate).toBe(true);
	});

	it("passes an item's error through as detail", async () => {
		const sent: DeployPlanMessage[] = [];
		const { run } = fakeRun(
			[{ type: "deploy_result", name: "p0", outcome: "failed", error: "boom" }],
			1,
		);
		await deployApply({ run })(ctx, plan(false, ["created"]), false, (m) =>
			sent.push(m),
		);
		expect(sent).toContainEqual({
			type: "deploy-item",
			name: "p0",
			outcome: "failed",
			detail: "boom",
		});
	});

	it("does not deploy when the confirm modal is dismissed", async () => {
		const { calls, run } = fakeRun([summary(0)], 0);
		// prod, no rebuild -> accept modal; no queued response -> dismissed.
		await deployApply({ run })(ctx, plan(true, ["created"]), false, () => {});
		expect(calls).toHaveLength(0);
	});

	it("deploys after the confirm modal is accepted", async () => {
		queueMessageResponse("Deploy");
		const { calls, run } = fakeRun([summary(0)], 0);
		await deployApply({ run })(ctx, plan(true, ["created"]), false, () => {});
		expect(calls).toHaveLength(1);
	});

	it("requires the env name for a production rebuild, and proceeds when it matches", async () => {
		queueInputBox("staging");
		const { calls, run } = fakeRun([summary(0)], 0);
		await deployApply({ run })(ctx, plan(true, ["rebuilt"]), false, () => {});
		expect(calls).toHaveLength(1);
	});

	it("aborts a production rebuild when the env name isn't typed", async () => {
		const { calls, run } = fakeRun([summary(0)], 0);
		// no queued input box -> dismissed.
		await deployApply({ run })(ctx, plan(true, ["rebuilt"]), false, () => {});
		expect(calls).toHaveLength(0);
	});

	it("reports an error on a non-zero exit with no summary", async () => {
		const sent: DeployPlanMessage[] = [];
		const { run } = fakeRun(
			[{ type: "deploy_start", name: "p0", index: 1, total: 1 }],
			1,
		);
		await deployApply({ run })(ctx, plan(false, ["created"]), false, (m) =>
			sent.push(m),
		);
		expect(sent.some((m) => m.type === "deploy-error")).toBe(true);
	});

	it("offers sign-in on the auth exit code", async () => {
		queueMessageResponse("Sign in");
		const sent: DeployPlanMessage[] = [];
		const { run } = fakeRun([], 4);
		await deployApply({ run })(ctx, plan(false, ["created"]), false, (m) =>
			sent.push(m),
		);
		expect(sent.some((m) => m.type === "deploy-error")).toBe(true);
		await flush();
		expect(
			getState().executeCommandCalls.some((c) => c.name === "gaffer.signIn"),
		).toBe(true);
	});
});
