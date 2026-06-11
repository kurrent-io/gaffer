import { describe, it, expect, afterEach } from "vitest";
import { findDevLibPath } from "../src/native.js";

// The dev fallback walks up ancestor directories and loads the first matching
// native library it finds. It is gated behind GAFFER_RUNTIME_DEV so a
// production install can't be tricked into loading a library planted in a
// writable ancestor directory.
describe("findDevLibPath gating", () => {
	const saved = process.env.GAFFER_RUNTIME_DEV;

	afterEach(() => {
		if (saved === undefined) {
			delete process.env.GAFFER_RUNTIME_DEV;
		} else {
			process.env.GAFFER_RUNTIME_DEV = saved;
		}
	});

	it("refuses the ancestor walk-up without the opt-in", () => {
		delete process.env.GAFFER_RUNTIME_DEV;
		expect(findDevLibPath()).toBeNull();
	});

	it("treats falsy opt-in values as off", () => {
		process.env.GAFFER_RUNTIME_DEV = "0";
		expect(findDevLibPath()).toBeNull();
		process.env.GAFFER_RUNTIME_DEV = "false";
		expect(findDevLibPath()).toBeNull();
	});

	it("resolves the source-tree library when opted in", () => {
		process.env.GAFFER_RUNTIME_DEV = "1";
		expect(findDevLibPath()).toContain("Gaffer.Runtime");
	});
});
