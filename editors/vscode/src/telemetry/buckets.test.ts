import { describe, expect, it } from "vitest";

import { bucketCliVersion, bucketDuration } from "./buckets.js";

describe("bucketDuration", () => {
	it("rounds down to the nearest bucket boundary", () => {
		expect(bucketDuration(0)).toBe(0);
		expect(bucketDuration(9)).toBe(0);
		expect(bucketDuration(10)).toBe(10);
		expect(bucketDuration(99)).toBe(10);
		expect(bucketDuration(100)).toBe(100);
		expect(bucketDuration(999)).toBe(100);
		expect(bucketDuration(1000)).toBe(1000);
		expect(bucketDuration(10_000)).toBe(10000);
		expect(bucketDuration(60_000)).toBe(60000);
		expect(bucketDuration(600_000)).toBe(600000);
	});

	it("caps at the largest bucket for extreme values", () => {
		expect(bucketDuration(99_999_999)).toBe(600000);
	});

	it("collapses negative and non-finite values to 0", () => {
		expect(bucketDuration(-1)).toBe(0);
		expect(bucketDuration(Number.NaN)).toBe(0);
		expect(bucketDuration(Number.POSITIVE_INFINITY)).toBe(0);
		expect(bucketDuration(Number.NEGATIVE_INFINITY)).toBe(0);
	});
});

describe("bucketCliVersion", () => {
	it("extracts major.minor from a semver string", () => {
		expect(bucketCliVersion("0.4.2")).toBe("0.4");
		expect(bucketCliVersion("26.1.0")).toBe("26.1");
		expect(bucketCliVersion("100.999.0")).toBe("100.999");
	});

	it("strips a leading v prefix", () => {
		expect(bucketCliVersion("v0.4.2")).toBe("0.4");
		expect(bucketCliVersion("v26.1.0-rc.1")).toBe("26.1");
	});

	it("tolerates trailing pre-release / build metadata", () => {
		expect(bucketCliVersion("0.4.2-rc.1")).toBe("0.4");
		expect(bucketCliVersion("0.4.2+build.7")).toBe("0.4");
	});

	it("trims surrounding whitespace", () => {
		expect(bucketCliVersion("  0.4.2  ")).toBe("0.4");
	});

	it("returns 'unknown' for unparseable input", () => {
		expect(bucketCliVersion("")).toBe("unknown");
		expect(bucketCliVersion("nightly")).toBe("unknown");
		expect(bucketCliVersion("v")).toBe("unknown");
	});
});
