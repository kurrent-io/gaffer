import { describe, expect, it } from "vitest";
import { parsePlanReport } from "./deploy-plan.js";

describe("parsePlanReport", () => {
	it("parses a full envelope with per-item flags and drift", () => {
		const report = parsePlanReport(
			JSON.stringify({
				env: "staging",
				target: "cluster",
				production: false,
				verdict: "deployable",
				changes: 1,
				plan: [
					{ name: "a", outcome: "created" },
					{
						name: "b",
						outcome: "refused",
						recreate: true,
						reason: "engine version",
					},
				],
				configDrift: [{ knob: "x", server: 1, local: 2 }],
			}),
		);
		expect(report?.verdict).toBe("deployable");
		expect(report?.plan).toHaveLength(2);
		expect(report?.plan[1]?.recreate).toBe(true);
		expect(report?.plan[1]?.reason).toBe("engine version");
		expect(report?.configDrift?.[0]).toEqual({
			knob: "x",
			server: 1,
			local: 2,
		});
	});

	it("parses a minimal in-sync envelope", () => {
		const report = parsePlanReport(
			JSON.stringify({ verdict: "in-sync", changes: 0, plan: [] }),
		);
		expect(report?.verdict).toBe("in-sync");
		expect(report?.plan).toEqual([]);
	});

	it("keeps a configDriftError when the check couldn't run", () => {
		const report = parsePlanReport(
			JSON.stringify({
				verdict: "deployable",
				changes: 1,
				plan: [{ name: "a", outcome: "created" }],
				configDriftError: "no HTTP surface",
			}),
		);
		expect(report?.configDriftError).toBe("no HTTP surface");
	});

	it("returns null on non-JSON or empty stdout", () => {
		expect(parsePlanReport("not json")).toBeNull();
		expect(parsePlanReport("")).toBeNull();
	});

	it("returns null when the shape is wrong (missing verdict)", () => {
		expect(
			parsePlanReport(JSON.stringify({ changes: 0, plan: [] })),
		).toBeNull();
	});
});
