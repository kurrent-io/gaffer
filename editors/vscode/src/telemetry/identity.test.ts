import { describe, expect, it } from "vitest";

import { fromConfig, mint } from "./identity.js";

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
