import { describe, expect, it } from "vitest";
import { diffVersionArgs, historyArgs, rollbackArgs } from "./history-args.js";

describe("historyArgs", () => {
	it("reads the timeline as JSON with the name last, after --", () => {
		expect(historyArgs("staging", "orders")).toEqual([
			"history",
			"--json",
			"--env",
			"staging",
			"--",
			"orders",
		]);
	});

	it("keeps a dash-leading projection name a positional, not a flag", () => {
		const args = historyArgs("staging", "--env");
		expect(args.at(-2)).toBe("--");
		expect(args.at(-1)).toBe("--env");
	});
});

describe("diffVersionArgs", () => {
	it("diffs two refs with the name last, after --", () => {
		expect(diffVersionArgs("staging", "orders", "9f2a1c", "local")).toEqual([
			"diff",
			"--json",
			"--env",
			"staging",
			"--left",
			"9f2a1c",
			"--right",
			"local",
			"--",
			"orders",
		]);
	});

	it("keeps a dash-leading projection name a positional, not a flag", () => {
		const args = diffVersionArgs("staging", "--left", "a1b2c3", "d4e5f6");
		expect(args.at(-2)).toBe("--");
		expect(args.at(-1)).toBe("--left");
	});
});

describe("rollbackArgs", () => {
	it("rolls back to a hash, name and hash last after --, with --yes", () => {
		expect(rollbackArgs("prod", "orders", "3f9a1c2")).toEqual([
			"rollback",
			"--json",
			"--yes",
			"--env",
			"prod",
			"--",
			"orders",
			"3f9a1c2",
		]);
	});

	it("keeps a dash-leading projection name ahead of the hash, both positional", () => {
		const args = rollbackArgs("prod", "--env", "3f9a1c2");
		expect(args.slice(-3)).toEqual(["--", "--env", "3f9a1c2"]);
	});
});
