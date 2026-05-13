import { describe, expect, it } from "vitest";

import { fromConfig, mint, projectId } from "./identity.js";

const UUID_V4 =
	/^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i;

describe("identity.mint", () => {
	it("produces three UUID v4 strings", () => {
		const id = mint();
		expect(id.telemetryId).toMatch(UUID_V4);
		expect(id.salt).toMatch(UUID_V4);
		expect(id.runId).toMatch(UUID_V4);
	});

	it("produces independent values across calls", () => {
		const a = mint();
		const b = mint();
		expect(a.telemetryId).not.toBe(b.telemetryId);
		expect(a.salt).not.toBe(b.salt);
		expect(a.runId).not.toBe(b.runId);
	});
});

describe("identity.fromConfig", () => {
	it("returns null when telemetry_id is missing", () => {
		expect(fromConfig({ salt: "abc" })).toBeNull();
	});

	it("returns null when salt is missing", () => {
		expect(fromConfig({ telemetry_id: "abc" })).toBeNull();
	});

	it("treats empty-string fields as missing (next save re-mints)", () => {
		expect(fromConfig({ telemetry_id: "", salt: "abc" })).toBeNull();
		expect(fromConfig({ telemetry_id: "abc", salt: "" })).toBeNull();
	});

	it("adopts the persisted halves and mints a fresh runId", () => {
		const id = fromConfig({ telemetry_id: "tel", salt: "salty" });
		if (id === null) throw new Error("expected non-null identity");
		expect(id.telemetryId).toBe("tel");
		expect(id.salt).toBe("salty");
		expect(id.runId).toMatch(UUID_V4);
	});

	it("returns a different runId on each call (per-process)", () => {
		const a = fromConfig({ telemetry_id: "tel", salt: "salty" });
		const b = fromConfig({ telemetry_id: "tel", salt: "salty" });
		if (a === null || b === null)
			throw new Error("expected non-null identities");
		expect(a.runId).not.toBe(b.runId);
	});
});

describe("identity.projectId", () => {
	it("returns 16 lowercase hex chars", () => {
		const id = projectId("salt", "/abs/path");
		expect(id).toMatch(/^[0-9a-f]{16}$/);
	});

	it("is deterministic across calls with the same salt and path", () => {
		expect(projectId("salt", "/abs/path")).toBe(projectId("salt", "/abs/path"));
	});

	it("differs when the salt changes", () => {
		expect(projectId("salt-a", "/abs/path")).not.toBe(
			projectId("salt-b", "/abs/path"),
		);
	});

	it("differs when the path changes", () => {
		expect(projectId("salt", "/a")).not.toBe(projectId("salt", "/b"));
	});
});
