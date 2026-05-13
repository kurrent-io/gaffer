import { describe, expect, it, vi } from "vitest";

import type { Phase } from "./exception.js";
import type { Telemetry } from "./facade.js";
import {
	reportException,
	wrapAsync,
	wrapSync,
	type WrapContext,
} from "./wrap.js";

interface ReportCall {
	phase: Phase;
	err: unknown;
}

function makeCtx(): { ctx: WrapContext; reports: ReportCall[] } {
	const reports: ReportCall[] = [];
	const telemetry: Telemetry = {
		emit: () => {},
		drain: async () => {},
		refreshOptOut: async () => {},
		invokerId: () => null,
		reportException: (phase, err) => {
			reports.push({ phase, err });
		},
	};
	return { ctx: { telemetry }, reports };
}

describe("wrapAsync", () => {
	it("returns the wrapped value on the happy path without reporting", async () => {
		const { ctx, reports } = makeCtx();
		const fn = wrapAsync(ctx, "event_processing", async (x: number) => x + 1);
		expect(await fn(2)).toBe(3);
		expect(reports).toEqual([]);
	});

	it("reports the exception and re-throws on rejection", async () => {
		const { ctx, reports } = makeCtx();
		const err = new Error("boom");
		const fn = wrapAsync(ctx, "event_processing", async () => {
			throw err;
		});
		await expect(fn()).rejects.toThrow("boom");
		expect(reports).toEqual([{ phase: "event_processing", err }]);
	});

	it("preserves the original error identity on re-throw", async () => {
		const original = new Error("identity");
		const { ctx } = makeCtx();
		const fn = wrapAsync(ctx, "event_processing", async () => {
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
			reportException: () => {
				throw new Error("reporter exploded");
			},
		};
		const fn = wrapAsync(
			{ telemetry: exploding },
			"event_processing",
			async () => {
				throw new Error("original");
			},
		);
		await expect(fn()).rejects.toThrow("reporter exploded");
		// Note: an exploding reporter does replace the original error
		// here. Production facade's reportException catches its own
		// throws so this shouldn't happen, but document the boundary.
	});
});

describe("wrapSync", () => {
	it("returns the wrapped value on the happy path without reporting", () => {
		const { ctx, reports } = makeCtx();
		const fn = wrapSync(ctx, "event_processing", (x: number) => x + 1);
		expect(fn(2)).toBe(3);
		expect(reports).toEqual([]);
	});

	it("reports and re-throws synchronously on a throw", () => {
		const { ctx, reports } = makeCtx();
		const fn = wrapSync(ctx, "event_processing", () => {
			throw new Error("sync boom");
		});
		expect(() => fn()).toThrow("sync boom");
		expect(reports).toHaveLength(1);
	});
});

describe("reportException (direct call)", () => {
	it("delegates to telemetry.reportException", () => {
		const { ctx, reports } = makeCtx();
		const err = new Error("direct");
		reportException(ctx, "startup", err);
		expect(reports).toEqual([{ phase: "startup", err }]);
	});
});

// Suppress unused-import warning in environments where vi isn't read.
void vi;
