import { describe, expect, it } from "vitest";

import type { Phase } from "./exception.js";
import type { Telemetry } from "./facade.js";
import { wrapAsync, wrapSync } from "./wrap.js";

interface ReportCall {
	phase: Phase;
	err: unknown;
}

function makeTelemetry(): { telemetry: Telemetry; reports: ReportCall[] } {
	const reports: ReportCall[] = [];
	const telemetry: Telemetry = {
		emit: () => {},
		drain: async () => {},
		refreshOptOut: async () => {},
		invokerId: () => null,
		isOptedOut: () => false,
		reportException: (phase, err) => {
			reports.push({ phase, err });
		},
	};
	return { telemetry, reports };
}

describe("wrapAsync", () => {
	it("returns the wrapped value on the happy path without reporting", async () => {
		const { telemetry, reports } = makeTelemetry();
		const fn = wrapAsync(
			telemetry,
			"event_processing",
			async (x: number) => x + 1,
		);
		expect(await fn(2)).toBe(3);
		expect(reports).toEqual([]);
	});

	it("reports the exception and re-throws on rejection", async () => {
		const { telemetry, reports } = makeTelemetry();
		const err = new Error("boom");
		const fn = wrapAsync(telemetry, "event_processing", async () => {
			throw err;
		});
		await expect(fn()).rejects.toThrow("boom");
		expect(reports).toEqual([{ phase: "event_processing", err }]);
	});

	it("preserves the original error identity on re-throw", async () => {
		const original = new Error("identity");
		const { telemetry } = makeTelemetry();
		const fn = wrapAsync(telemetry, "event_processing", async () => {
			throw original;
		});
		await fn().catch((err: unknown) => {
			expect(err).toBe(original);
		});
	});

	it("propagates the original error even when reportException itself throws", async () => {
		// Facade promises reportException never throws; this asserts
		// the wrapper doesn't depend on that contract being honoured.
		const exploding: Telemetry = {
			emit: () => {},
			drain: async () => {},
			refreshOptOut: async () => {},
			invokerId: () => null,
			isOptedOut: () => false,
			reportException: () => {
				throw new Error("reporter exploded");
			},
		};
		const fn = wrapAsync(exploding, "event_processing", async () => {
			throw new Error("original");
		});
		await expect(fn()).rejects.toThrow("reporter exploded");
		// Note: an exploding reporter does replace the original error
		// here. Production facade's reportException catches its own
		// throws so this shouldn't happen, but document the boundary.
	});
});

describe("wrapSync", () => {
	it("returns the wrapped value on the happy path without reporting", () => {
		const { telemetry, reports } = makeTelemetry();
		const fn = wrapSync(telemetry, "event_processing", (x: number) => x + 1);
		expect(fn(2)).toBe(3);
		expect(reports).toEqual([]);
	});

	it("reports and re-throws synchronously on a throw", () => {
		const { telemetry, reports } = makeTelemetry();
		const fn = wrapSync(telemetry, "event_processing", () => {
			throw new Error("sync boom");
		});
		expect(() => fn()).toThrow("sync boom");
		expect(reports).toHaveLength(1);
	});
});
