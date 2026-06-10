import { afterEach, describe, expect, it } from "vitest";
import * as vscode from "vscode";
import {
	buildSourceItems,
	debugProjectionPick,
} from "./debug-projection-pick.js";
import type { DebugProjectionArgs } from "../debugging/session-controller.js";
import { getState, queueQuickPick } from "../../test/testutil/vscode-state.js";

afterEach(() => {
	getState().quickPickResolutions.length = 0;
	getState().quickPickCalls.length = 0;
});

const tomlUri = vscode.Uri.parse("file:///p/gaffer.toml");

// Captures the args a picked source would launch with.
function capture() {
	const calls: DebugProjectionArgs[] = [];
	return {
		calls,
		start: (a: DebugProjectionArgs) => {
			calls.push(a);
			return Promise.resolve();
		},
	};
}

describe("buildSourceItems", () => {
	it("lists fixtures first, then envs, tagging the default", () => {
		const items = buildSourceItems(
			["happy", "sad"],
			[
				{ name: "cloud", default: false },
				{ name: "local", default: true },
			],
		);
		expect(items.map((i) => i.label)).toEqual([
			"Fixture: happy",
			"Fixture: sad",
			"Env: cloud",
			"Env: local",
		]);
		expect(items[0]).toMatchObject({ fixture: "happy" });
		expect(items[2]).toMatchObject({ env: "cloud" });
		// Only the default env carries the tag.
		expect(items[2]?.description).toBeUndefined();
		expect(items[3]?.description).toBe("default");
	});

	it("is empty when there are no fixtures or envs", () => {
		expect(buildSourceItems([], [])).toEqual([]);
	});
});

describe("debugProjectionPick", () => {
	it("starts a fixture run when a fixture row is picked", async () => {
		const { calls, start } = capture();
		queueQuickPick({ label: "Fixture: happy", fixture: "happy" });
		await debugProjectionPick({ start })({
			name: "checkout",
			tomlUri,
			fixtureNames: ["happy"],
			envs: [],
		});
		expect(calls).toEqual([{ name: "checkout", tomlUri, fixture: "happy" }]);
	});

	it("starts a live run against the picked env", async () => {
		const { calls, start } = capture();
		queueQuickPick({ label: "Env: cloud", env: "cloud" });
		await debugProjectionPick({ start })({
			name: "checkout",
			tomlUri,
			fixtureNames: [],
			envs: [{ name: "cloud", default: false }],
		});
		expect(calls).toEqual([{ name: "checkout", tomlUri, env: "cloud" }]);
	});

	it("does nothing (no picker) when there are no sources", async () => {
		const { calls, start } = capture();
		await debugProjectionPick({ start })({
			name: "checkout",
			tomlUri,
			fixtureNames: [],
			envs: [],
		});
		expect(calls).toEqual([]);
		expect(getState().quickPickCalls).toHaveLength(0);
	});

	it("does nothing when the pick is dismissed", async () => {
		const { calls, start } = capture();
		queueQuickPick(undefined);
		await debugProjectionPick({ start })({
			name: "checkout",
			tomlUri,
			fixtureNames: ["happy"],
			envs: [],
		});
		expect(calls).toEqual([]);
	});
});
