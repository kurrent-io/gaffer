import { afterEach, describe, expect, it } from "vitest";
import * as vscode from "vscode";
import {
	buildActionItems,
	projectionActions,
	type ProjectionActionsDeps,
	type ProjectionActionsEnv,
} from "./projection-actions.js";
import { getState, queueQuickPick } from "../../test/testutil/vscode-state.js";

afterEach(() => {
	getState().quickPickResolutions.length = 0;
	getState().quickPickCalls.length = 0;
});

const tomlUri = vscode.Uri.parse("file:///p/gaffer.toml");

type DiffArgs = Parameters<ProjectionActionsDeps["diff"]>[0];
type OperateArgs = Parameters<ProjectionActionsDeps["operate"]>[0];

function capture(): {
	diffCalls: DiffArgs[];
	operateCalls: OperateArgs[];
	deps: ProjectionActionsDeps;
} {
	const diffCalls: DiffArgs[] = [];
	const operateCalls: OperateArgs[] = [];
	return {
		diffCalls,
		operateCalls,
		deps: {
			diff: (a) => {
				diffCalls.push(a);
				return Promise.resolve();
			},
			operate: (a) => {
				operateCalls.push(a);
				return Promise.resolve();
			},
		},
	};
}

function actionLabels(items: ReturnType<typeof buildActionItems>): string[] {
	return items.filter((i) => i.pick).map((i) => i.label);
}

describe("buildActionItems", () => {
	it("groups by env with a separator per env, default env first", () => {
		const envs: ProjectionActionsEnv[] = [
			{ name: "local", default: false },
			{ name: "prod", default: true },
		];
		const separators = buildActionItems(envs)
			.filter((i) => i.kind === vscode.QuickPickItemKind.Separator)
			.map((i) => i.label);
		expect(separators).toEqual(["prod (default)", "local"]);
	});

	it("offers diff + pause/resume (unknown state) + delete variants", () => {
		const items = buildActionItems([{ name: "prod", default: true }]);
		expect(actionLabels(items)).toEqual([
			"$(diff-single) Diff against deployed",
			"$(debug-pause) Pause",
			"$(debug-start) Resume",
			"$(trash) Delete",
			"$(trash) Delete (and emitted streams)",
		]);
	});

	it("offers pause + abort (not resume) when running", () => {
		const items = buildActionItems([
			{ name: "prod", default: true, state: "running" },
		]);
		expect(actionLabels(items)).toEqual([
			"$(diff-single) Diff against deployed",
			"$(debug-pause) Pause",
			"$(debug-stop) Abort",
			"$(trash) Delete",
			"$(trash) Delete (and emitted streams)",
		]);
	});

	it("offers resume only (not pause/abort) when stopped", () => {
		const items = buildActionItems([
			{ name: "prod", default: true, state: "stopped" },
		]);
		expect(actionLabels(items)).toEqual([
			"$(diff-single) Diff against deployed",
			"$(debug-start) Resume",
			"$(trash) Delete",
			"$(trash) Delete (and emitted streams)",
		]);
	});

	it("carries production and deleteEmitted on the operate picks", () => {
		const items = buildActionItems([
			{ name: "prod", default: true, state: "running", production: true },
		]);
		expect(items.find((i) => i.label === "$(debug-pause) Pause")?.pick).toEqual(
			{ env: "prod", action: "pause", production: true },
		);
		expect(
			items.find((i) => i.label === "$(trash) Delete (and emitted streams)")
				?.pick,
		).toEqual({
			env: "prod",
			action: "delete",
			production: true,
			deleteEmitted: true,
		});
	});
});

describe("projectionActions", () => {
	it("runs the diff for the picked env", async () => {
		const { diffCalls, deps } = capture();
		queueQuickPick({ pick: { env: "prod", action: "diff" } });
		await projectionActions(deps)({
			name: "checkout",
			tomlUri,
			envs: [{ name: "prod", default: true }],
		});
		expect(diffCalls).toEqual([{ name: "checkout", tomlUri, env: "prod" }]);
	});

	it("routes an operate verb with production + deleteEmitted", async () => {
		const { operateCalls, deps } = capture();
		queueQuickPick({
			pick: {
				env: "prod",
				action: "delete",
				production: true,
				deleteEmitted: true,
			},
		});
		await projectionActions(deps)({
			name: "checkout",
			tomlUri,
			envs: [{ name: "prod", default: true }],
		});
		expect(operateCalls).toEqual([
			{
				name: "checkout",
				tomlUri,
				env: "prod",
				verb: "delete",
				production: true,
				deleteEmitted: true,
			},
		]);
	});

	it("defaults production to false when the pick omits it", async () => {
		const { operateCalls, deps } = capture();
		queueQuickPick({ pick: { env: "prod", action: "pause" } });
		await projectionActions(deps)({
			name: "checkout",
			tomlUri,
			envs: [{ name: "prod", default: true }],
		});
		expect(operateCalls[0]).toMatchObject({ verb: "pause", production: false });
	});

	it("does nothing (no picker) when there are no envs", async () => {
		const { diffCalls, operateCalls, deps } = capture();
		await projectionActions(deps)({ name: "checkout", tomlUri, envs: [] });
		expect(diffCalls).toEqual([]);
		expect(operateCalls).toEqual([]);
		expect(getState().quickPickCalls).toHaveLength(0);
	});

	it("does nothing when the pick is dismissed", async () => {
		const { diffCalls, operateCalls, deps } = capture();
		queueQuickPick(undefined);
		await projectionActions(deps)({
			name: "checkout",
			tomlUri,
			envs: [{ name: "prod", default: true }],
		});
		expect(diffCalls).toEqual([]);
		expect(operateCalls).toEqual([]);
	});

	it("ignores a picked separator row (no pick payload)", async () => {
		const { diffCalls, operateCalls, deps } = capture();
		queueQuickPick({ label: "prod", kind: vscode.QuickPickItemKind.Separator });
		await projectionActions(deps)({
			name: "checkout",
			tomlUri,
			envs: [{ name: "prod", default: true }],
		});
		expect(diffCalls).toEqual([]);
		expect(operateCalls).toEqual([]);
	});
});
