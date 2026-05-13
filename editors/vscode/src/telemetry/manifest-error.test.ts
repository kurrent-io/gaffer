import { describe, expect, it } from "vitest";

import { classifyManifestError } from "./manifest-error.js";

describe("classifyManifestError", () => {
	it("maps ENOENT to binary_not_found", () => {
		expect(classifyManifestError({ code: "ENOENT" })).toBe("binary_not_found");
	});

	it("maps killed=true with no code to timeout (execFile timeout signature)", () => {
		expect(classifyManifestError({ killed: true })).toBe("timeout");
	});

	it("maps other E-prefixed codes to binary_spawn_failed", () => {
		expect(classifyManifestError({ code: "EACCES" })).toBe(
			"binary_spawn_failed",
		);
		expect(classifyManifestError({ code: "EISDIR" })).toBe(
			"binary_spawn_failed",
		);
		expect(classifyManifestError({ code: "EMFILE" })).toBe(
			"binary_spawn_failed",
		);
	});

	it("returns unknown_error for plain Error / malformed-manifest paths", () => {
		expect(classifyManifestError(new Error("malformed manifest"))).toBe(
			"unknown_error",
		);
		expect(classifyManifestError({})).toBe("unknown_error");
	});

	it("returns unknown_error for non-object inputs", () => {
		expect(classifyManifestError(null)).toBe("unknown_error");
		expect(classifyManifestError(undefined)).toBe("unknown_error");
		expect(classifyManifestError("ENOENT")).toBe("unknown_error");
		expect(classifyManifestError(42)).toBe("unknown_error");
	});

	it("preserves the kill-via-timeout signal when code is unset", () => {
		// Real execFile timeout: err has killed=true, signal='SIGTERM',
		// no err.code. Make sure we don't fall through to
		// binary_spawn_failed for that shape.
		expect(classifyManifestError({ killed: true, signal: "SIGTERM" })).toBe(
			"timeout",
		);
	});
});
