import { afterEach, describe, expect, it } from "vitest";
import { pickRunMode } from "./run-projection.js";
import { getState, queueQuickPick } from "../../test/testutil/vscode-state.js";

afterEach(() => {
	// Drain any queued quickPick responses + recorded calls so a
	// test's leftover state doesn't leak into the next.
	getState().quickPickResolutions.length = 0;
	getState().quickPickCalls.length = 0;
});

describe("pickRunMode", () => {
	it("returns an empty (live default) source when details is null", async () => {
		// LSP didn't answer; degrade to a live default run, no picker.
		expect(await pickRunMode("p", null)).toEqual({});
		expect(getState().quickPickCalls).toEqual([]);
	});

	it("returns empty (live default) when there are no fixtures or envs", async () => {
		expect(
			await pickRunMode("p", {
				connection: null,
				fixtures: [],
				environments: [],
			}),
		).toEqual({});
		expect(getState().quickPickCalls).toEqual([]);
	});

	it("auto-selects a sole environment without prompting", async () => {
		const result = await pickRunMode("checkout", {
			connection: "esdb://prod:2113",
			fixtures: [],
			environments: [{ name: "prod", default: true }],
		});
		expect(result).toEqual({ env: "prod" });
		expect(getState().quickPickCalls).toEqual([]);
	});

	it("auto-selects a sole fixture without prompting", async () => {
		const result = await pickRunMode("checkout", {
			connection: null,
			fixtures: ["happy"],
			environments: [],
		});
		expect(result).toEqual({ fixture: "happy" });
		expect(getState().quickPickCalls).toEqual([]);
	});

	it("prompts with fixtures then envs when there's more than one source", async () => {
		queueQuickPick({ label: "Env: prod", env: "prod" });
		const result = await pickRunMode("checkout", {
			connection: "esdb://local:2113",
			fixtures: ["happy"],
			environments: [
				{ name: "local", default: true },
				{ name: "prod", default: false },
			],
		});
		expect(result).toEqual({ env: "prod" });
		const labels = (
			getState().quickPickCalls.at(-1)?.items as ReadonlyArray<{
				label: string;
			}>
		).map((i) => i.label);
		expect(labels).toEqual(["Fixture: happy", "Env: local", "Env: prod"]);
	});

	it("returns the fixture when a fixture row is picked", async () => {
		queueQuickPick({ label: "Fixture: sad", fixture: "sad" });
		const result = await pickRunMode("checkout", {
			connection: null,
			fixtures: ["happy", "sad"],
			environments: [],
		});
		expect(result).toEqual({ fixture: "sad" });
	});

	it("returns undefined when the user dismisses the picker", async () => {
		// Dismissal must stay distinct from a live-default source so
		// runProjection aborts rather than launching.
		queueQuickPick(undefined);
		const result = await pickRunMode("checkout", {
			connection: null,
			fixtures: ["happy", "sad"],
			environments: [],
		});
		expect(result).toBeUndefined();
	});
});
