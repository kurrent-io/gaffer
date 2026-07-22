import { describe, expect, it } from "vitest";
import {
	parseDiffReport,
	parseHistoryReport,
	parseRollbackResult,
} from "./history-schema.js";

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

describe("parseDiffReport", () => {
	it("parses sides and lines with intraline spans", () => {
		const report = parseDiffReport(
			JSON.stringify({
				name: "orders",
				left: { ref: "version", hash: "1d77f5a", source: "old\n" },
				right: { ref: "local", source: "new\n" },
				lines: [
					{ kind: "equal", oldN: 1, newN: 1, text: "const x = 1" },
					{
						kind: "removed",
						oldN: 2,
						newN: 0,
						text: "let y = 2",
						emphFrom: 4,
						emphTo: 5,
					},
					{
						kind: "added",
						oldN: 0,
						newN: 2,
						text: "let z = 2",
						emphFrom: 4,
						emphTo: 5,
					},
				],
			}),
		);
		expect(report?.name).toBe("orders");
		expect(report?.left).toMatchObject({ ref: "version", hash: "1d77f5a" });
		expect(report?.right).toMatchObject({ ref: "local", source: "new\n" });
		expect(report?.lines).toHaveLength(3);
		expect(report?.lines[1]).toMatchObject({
			kind: "removed",
			emphFrom: 4,
			emphTo: 5,
		});
	});

	it("rejects an unknown line kind", () => {
		const report = parseDiffReport(
			JSON.stringify({
				name: "orders",
				left: { ref: "local" },
				right: { ref: "local" },
				lines: [{ kind: "modified", oldN: 1, newN: 1 }],
			}),
		);
		expect(report).toBeNull();
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
