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
	it("returns null (live) when details is null", async () => {
		// LSP didn't answer / rejected -> graceful degradation to live
		// (matches the pre-picker single-step UX).
		expect(await pickRunMode("p", null)).toBeNull();
	});

	it("returns null (live) when projection has neither connection nor fixtures", async () => {
		// CLI will surface the resulting error itself; we don't gate
		// the run with an extra confirmation toast.
		expect(
			await pickRunMode("p", { connection: null, fixtures: [] }),
		).toBeNull();
	});

	it("returns null (live) when projection has connection but no fixtures", async () => {
		// Only one possible run mode -> skip the second pick.
		expect(
			await pickRunMode("p", {
				connection: "esdb://localhost:2113",
				fixtures: [],
			}),
		).toBeNull();
		// No QuickPick should have been shown.
		expect(getState().quickPickCalls).toEqual([]);
	});

	it("shows a fixture-only QuickPick when projection has fixtures but no connection", async () => {
		queueQuickPick({ label: "fixture: happy", mode: "fixture", name: "happy" });
		const result = await pickRunMode("checkout", {
			connection: null,
			fixtures: ["happy", "sad"],
		});
		expect(result).toBe("happy");
		const lastCall = getState().quickPickCalls.at(-1);
		const labels = (lastCall?.items as ReadonlyArray<{ label: string }>).map(
			(i) => i.label,
		);
		expect(labels).toEqual(["fixture: happy", "fixture: sad"]);
	});

	it("shows live + fixtures when projection has both", async () => {
		queueQuickPick({
			label: "connection: esdb://localhost:2113",
			mode: "live",
		});
		const result = await pickRunMode("checkout", {
			connection: "esdb://localhost:2113",
			fixtures: ["happy"],
		});
		expect(result).toBeNull();
		const lastCall = getState().quickPickCalls.at(-1);
		const labels = (lastCall?.items as ReadonlyArray<{ label: string }>).map(
			(i) => i.label,
		);
		expect(labels).toEqual([
			"connection: esdb://localhost:2113",
			"fixture: happy",
		]);
	});

	it("returns the fixture name when a fixture item is picked", async () => {
		queueQuickPick({ label: "fixture: sad", mode: "fixture", name: "sad" });
		const result = await pickRunMode("checkout", {
			connection: "esdb://localhost:2113",
			fixtures: ["happy", "sad"],
		});
		expect(result).toBe("sad");
	});

	it("returns undefined when the user dismisses the picker", async () => {
		// Picker dismissal vs. live default are distinct: dismissal
		// must propagate as undefined so runProjection aborts the run
		// rather than launching live.
		queueQuickPick(undefined);
		const result = await pickRunMode("checkout", {
			connection: "esdb://localhost:2113",
			fixtures: ["happy"],
		});
		expect(result).toBeUndefined();
	});
});
