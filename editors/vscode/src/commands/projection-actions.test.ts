import { afterEach, describe, expect, it } from "vitest";
import * as vscode from "vscode";
import {
	buildActionItems,
	projectionActions,
	type ProjectionActionsEnv,
} from "./projection-actions.js";
import { getState, queueQuickPick } from "../../test/testutil/vscode-state.js";

afterEach(() => {
	getState().quickPickResolutions.length = 0;
	getState().quickPickCalls.length = 0;
});

const tomlUri = vscode.Uri.parse("file:///p/gaffer.toml");

function capture() {
	const calls: { name: string; tomlUri: vscode.Uri; env: string }[] = [];
	return {
		calls,
		diff: (a: { name: string; tomlUri: vscode.Uri; env: string }) => {
			calls.push(a);
			return Promise.resolve();
		},
	};
}

describe("buildActionItems", () => {
	it("groups by env with a separator per env, default env first", () => {
		const envs: ProjectionActionsEnv[] = [
			{ name: "local", default: false },
			{ name: "prod", default: true },
		];
		const items = buildActionItems(envs);
		// prod (default) leads: its separator + action, then local's.
		expect(items.map((i) => [i.label, i.kind])).toEqual([
			["prod (default)", vscode.QuickPickItemKind.Separator],
			["$(diff-single) Diff against deployed", undefined],
			["local", vscode.QuickPickItemKind.Separator],
			["$(diff-single) Diff against deployed", undefined],
		]);
		expect(items.filter((i) => i.pick).map((i) => i.pick)).toEqual([
			{ env: "prod", action: "diff" },
			{ env: "local", action: "diff" },
		]);
	});

	it("groups a single env under a separator too", () => {
		const items = buildActionItems([{ name: "prod", default: true }]);
		expect(items.map((i) => [i.label, i.kind])).toEqual([
			["prod (default)", vscode.QuickPickItemKind.Separator],
			["$(diff-single) Diff against deployed", undefined],
		]);
		expect(items[1]?.pick).toEqual({ env: "prod", action: "diff" });
	});
});

describe("projectionActions", () => {
	it("runs the diff for the picked env", async () => {
		const { calls, diff } = capture();
		queueQuickPick({
			label: "$(diff-single) Diff against deployed",
			pick: { env: "prod", action: "diff" },
		});
		await projectionActions({ diff })({
			name: "checkout",
			tomlUri,
			envs: [
				{ name: "prod", default: true },
				{ name: "local", default: false },
			],
		});
		expect(calls).toEqual([{ name: "checkout", tomlUri, env: "prod" }]);
	});

	it("does nothing (no picker) when there are no envs", async () => {
		const { calls, diff } = capture();
		await projectionActions({ diff })({ name: "checkout", tomlUri, envs: [] });
		expect(calls).toEqual([]);
		expect(getState().quickPickCalls).toHaveLength(0);
	});

	it("does nothing when the pick is dismissed", async () => {
		const { calls, diff } = capture();
		queueQuickPick(undefined);
		await projectionActions({ diff })({
			name: "checkout",
			tomlUri,
			envs: [{ name: "prod", default: true }],
		});
		expect(calls).toEqual([]);
	});

	it("ignores a picked separator row (no pick payload)", async () => {
		const { calls, diff } = capture();
		queueQuickPick({
			label: "prod",
			kind: vscode.QuickPickItemKind.Separator,
		});
		await projectionActions({ diff })({
			name: "checkout",
			tomlUri,
			envs: [
				{ name: "prod", default: true },
				{ name: "local", default: false },
			],
		});
		expect(calls).toEqual([]);
	});
});
