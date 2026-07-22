import { describe, expect, it } from "vitest";
import { parseHistoryReport, parseRollbackResult } from "./history-schema.js";

describe("parseHistoryReport", () => {
	it("parses a ledger, defaulting the omitted false/empty fields", () => {
		const report = parseHistoryReport(
			JSON.stringify([
				{
					version: 7,
					time: "2026-07-22T10:00:00Z",
					contentHash: "3f9a1c2deadbeef",
					kind: "deploy",
					enabled: true,
					actor: "alice",
					operation: "deploy",
					configChanges: [
						{ knob: "checkpointAfterMs", from: "1000", to: "2000" },
					],
				},
				{
					version: 2,
					kind: "disabled",
					stateChange: true,
					contentHash: "8b2e04a",
				},
			]),
		);
		expect(report).not.toBeNull();
		expect(report).toHaveLength(2);
		expect(report?.[0]).toMatchObject({
			version: 7,
			kind: "deploy",
			enabled: true,
			external: false,
			stateChange: false,
			deleted: false,
			actor: "alice",
		});
		expect(report?.[0]?.configChanges).toEqual([
			{ knob: "checkpointAfterMs", from: "1000", to: "2000" },
		]);
		expect(report?.[1]).toMatchObject({ kind: "disabled", stateChange: true });
	});

	it("returns null on non-JSON and on a shape mismatch", () => {
		expect(parseHistoryReport("")).toBeNull();
		expect(parseHistoryReport("not json")).toBeNull();
		expect(
			parseHistoryReport(
				JSON.stringify([{ version: "seven", kind: "deploy" }]),
			),
		).toBeNull();
		expect(
			parseHistoryReport(JSON.stringify({ version: 1, kind: "deploy" })),
		).toBeNull();
	});
});

describe("parseRollbackResult", () => {
	it("parses a rolled-back and an unchanged outcome", () => {
		expect(
			parseRollbackResult(
				JSON.stringify({
					name: "orders",
					outcome: "rolled-back",
					hash: "1d77f5a",
				}),
			),
		).toEqual({
			name: "orders",
			outcome: "rolled-back",
			hash: "1d77f5a",
		});
		expect(
			parseRollbackResult(
				JSON.stringify({
					name: "orders",
					outcome: "unchanged",
					hash: "1d77f5a",
				}),
			)?.outcome,
		).toBe("unchanged");
	});

	it("rejects an unknown outcome", () => {
		expect(
			parseRollbackResult(
				JSON.stringify({ name: "orders", outcome: "refused", hash: "x" }),
			),
		).toBeNull();
	});
});
