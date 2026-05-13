import type { Event } from "@kurrent/gaffer-telemetry";
import { describe, expect, it, vi } from "vitest";

import type { Telemetry } from "./facade.js";
import {
	reportException,
	wrapAsync,
	wrapSync,
	type WrapContext,
} from "./wrap.js";

function makeCtx(overrides: Partial<WrapContext> = {}): {
	ctx: WrapContext;
	emitted: Event[];
} {
	const emitted: Event[] = [];
	const telemetry: Telemetry = {
		emit: (event) => {
			emitted.push(event);
		},
		drain: async () => {},
		refreshOptOut: async () => {},
		invokerId: () => null,
	};
	const ctx: WrapContext = {
		getTelemetry: () => telemetry,
		extensionPath: "/opt/gaffer/extension",
		getWorkspaceFolders: () => [],
		log: () => {},
		...overrides,
	};
	return { ctx, emitted };
}

describe("wrapAsync", () => {
	it("returns the wrapped value on the happy path without emitting", async () => {
		const { ctx, emitted } = makeCtx();
		const fn = wrapAsync(ctx, "event_processing", async (x: number) => x + 1);
		expect(await fn(2)).toBe(3);
		expect(emitted).toEqual([]);
	});

	it("emits an exception envelope and re-throws on rejection", async () => {
		const { ctx, emitted } = makeCtx();
		const fn = wrapAsync(ctx, "event_processing", async () => {
			throw new Error("boom");
		});
		await expect(fn()).rejects.toThrow("boom");
		expect(emitted).toHaveLength(1);
		const event = emitted[0];
		if (event?.name !== "exception")
			throw new Error("expected exception event");
		expect(event.properties.phase).toBe("event_processing");
		expect(event.properties.exceptions[0]?.value).toBe("boom");
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

	it("no-ops the emit when telemetry isn't built yet (getTelemetry returns null)", async () => {
		const ctx: WrapContext = {
			getTelemetry: () => null,
			extensionPath: "/opt/gaffer/extension",
			getWorkspaceFolders: () => [],
			log: () => {},
		};
		const fn = wrapAsync(ctx, "startup", async () => {
			throw new Error("pre-facade");
		});
		await expect(fn()).rejects.toThrow("pre-facade");
		// No telemetry to assert on; this just verifies no throw inside
		// the reporter and that the original error still propagates.
	});

	it("swallows reporter-side throws without replacing the original error", async () => {
		const exploding: Telemetry = {
			emit: () => {
				throw new Error("reporter exploded");
			},
			drain: async () => {},
			refreshOptOut: async () => {},
			invokerId: () => null,
		};
		const ctx: WrapContext = {
			getTelemetry: () => exploding,
			extensionPath: "/opt/gaffer/extension",
			getWorkspaceFolders: () => [],
			log: vi.fn(),
		};
		const fn = wrapAsync(ctx, "event_processing", async () => {
			throw new Error("original");
		});
		await expect(fn()).rejects.toThrow("original");
		expect(ctx.log).toHaveBeenCalledTimes(1);
	});
});

describe("wrapSync", () => {
	it("returns the wrapped value on the happy path without emitting", () => {
		const { ctx, emitted } = makeCtx();
		const fn = wrapSync(ctx, "event_processing", (x: number) => x + 1);
		expect(fn(2)).toBe(3);
		expect(emitted).toEqual([]);
	});

	it("emits and re-throws synchronously on a throw", () => {
		const { ctx, emitted } = makeCtx();
		const fn = wrapSync(ctx, "event_processing", () => {
			throw new Error("sync boom");
		});
		expect(() => fn()).toThrow("sync boom");
		expect(emitted).toHaveLength(1);
	});
});

describe("workspace folders resolution", () => {
	it("calls getWorkspaceFolders on each report (no activation-time snapshot)", () => {
		const folders: string[] = ["/proj/a"];
		const getFolders = vi.fn(() => folders);
		const original = new Error("boom");
		original.stack = "Error: boom\n    at fn (/proj/b/user.js:1:1)";
		const { ctx, emitted } = makeCtx({
			extensionPath: "/opt/gaffer",
			getWorkspaceFolders: getFolders,
		});

		// First report: /proj/b is not yet a workspace folder; frame survives.
		reportException(ctx, "event_processing", original);
		expect(getFolders).toHaveBeenCalledTimes(1);
		const first = emitted[0];
		if (first?.name !== "exception") throw new Error("expected exception");
		expect(first.properties.exceptions[0]?.stacktrace.frames).toHaveLength(1);

		// Mid-session: /proj/b is opened.
		folders.push("/proj/b");

		// Second report: the new folder's frame is now dropped.
		reportException(ctx, "event_processing", original);
		expect(getFolders).toHaveBeenCalledTimes(2);
		const second = emitted[1];
		if (second?.name !== "exception") throw new Error("expected exception");
		expect(second.properties.exceptions[0]?.stacktrace.frames).toEqual([]);
	});
});

describe("reportException (direct call)", () => {
	it("emits without throwing, given a built telemetry handle", () => {
		const { ctx, emitted } = makeCtx();
		reportException(ctx, "startup", new Error("direct"));
		expect(emitted).toHaveLength(1);
		const event = emitted[0];
		if (event?.name !== "exception")
			throw new Error("expected exception event");
		expect(event.properties.exceptions[0]?.value).toBe("direct");
	});

	it("is safe with a null telemetry handle (pre-facade activation)", () => {
		const ctx: WrapContext = {
			getTelemetry: () => null,
			extensionPath: "/opt/gaffer/extension",
			getWorkspaceFolders: () => [],
			log: () => {},
		};
		expect(() =>
			reportException(ctx, "startup", new Error("pre")),
		).not.toThrow();
	});
});
