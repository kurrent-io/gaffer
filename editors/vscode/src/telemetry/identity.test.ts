import { describe, expect, it, vi } from "vitest";

import type { TelemetryConfig } from "./config.js";
import { ensureIdentity, fromConfig, mint } from "./identity.js";

const UUID_V4 =
	/^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i;

describe("identity.mint", () => {
	it("produces UUID v4 strings", () => {
		const id = mint();
		expect(id.telemetryId).toMatch(UUID_V4);
		expect(id.runId).toMatch(UUID_V4);
	});

	it("produces independent values across calls", () => {
		const a = mint();
		const b = mint();
		expect(a.telemetryId).not.toBe(b.telemetryId);
		expect(a.runId).not.toBe(b.runId);
	});
});

describe("identity.fromConfig", () => {
	it("returns null when telemetry_id is missing", () => {
		expect(fromConfig({})).toBeNull();
	});

	it("treats an empty-string telemetry_id as missing (next save re-mints)", () => {
		expect(fromConfig({ telemetry_id: "" })).toBeNull();
	});

	it("adopts the persisted id and mints a fresh runId", () => {
		const id = fromConfig({ telemetry_id: "tel" });
		if (id === null) throw new Error("expected non-null identity");
		expect(id.telemetryId).toBe("tel");
		expect(id.runId).toMatch(UUID_V4);
	});

	it("returns a different runId on each call (per-process)", () => {
		const a = fromConfig({ telemetry_id: "tel" });
		const b = fromConfig({ telemetry_id: "tel" });
		if (a === null || b === null)
			throw new Error("expected non-null identities");
		expect(a.runId).not.toBe(b.runId);
	});
});

describe("identity.ensureIdentity", () => {
	it("adopts a persisted id without calling persistMint", async () => {
		const persist = vi.fn(async () => {});
		const id = await ensureIdentity({ telemetry_id: "tel" }, persist);
		expect(id.telemetryId).toBe("tel");
		expect(persist).not.toHaveBeenCalled();
	});

	it("mints and persists on cold install", async () => {
		const persist = vi.fn(async () => {});
		const id = await ensureIdentity({}, persist);
		expect(id.telemetryId).toMatch(UUID_V4);
		expect(persist).toHaveBeenCalledWith({ telemetry_id: id.telemetryId });
	});

	it("two concurrent first-mints last-writer-wins on the persist callback", async () => {
		// Simulates two extension hosts racing on a fresh install. Each
		// host loads {} (no id yet) before either has written. Both
		// mint independently; both call persist. The losing process
		// emits this session under its own id; the next activation
		// reads the winner.
		const store: TelemetryConfig = {};
		const persist = async (patch: Partial<TelemetryConfig>): Promise<void> => {
			Object.assign(store, patch);
		};
		const snapshot: TelemetryConfig = {};
		const [a, b] = await Promise.all([
			ensureIdentity(snapshot, persist),
			ensureIdentity(snapshot, persist),
		]);
		expect(a.telemetryId).toMatch(UUID_V4);
		expect(b.telemetryId).toMatch(UUID_V4);
		expect(a.telemetryId).not.toBe(b.telemetryId);
		// The store retains one of the two ids - last writer wins.
		expect([a.telemetryId, b.telemetryId]).toContain(store.telemetry_id);
	});
});
