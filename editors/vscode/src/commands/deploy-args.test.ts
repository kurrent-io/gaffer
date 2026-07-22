import { describe, expect, it } from "vitest";
import { deployApplyArgs, deployPreviewArgs } from "./deploy-args.js";

describe("deployPreviewArgs", () => {
	it("plans the whole project when unscoped, with no positional separator", () => {
		expect(deployPreviewArgs("staging", undefined)).toEqual([
			"deploy",
			"--dry-run",
			"--json",
			"--env",
			"staging",
		]);
	});

	it("scopes to a projection with the name last, after --", () => {
		expect(deployPreviewArgs("staging", "orders")).toEqual([
			"deploy",
			"--dry-run",
			"--json",
			"--env",
			"staging",
			"--",
			"orders",
		]);
	});

	it("keeps a dash-leading projection name a positional, not a flag", () => {
		// `--` before the name stops the CLI reading `--no-validate` as the flag.
		const args = deployPreviewArgs("staging", "--no-validate");
		expect(args.at(-2)).toBe("--");
		expect(args.at(-1)).toBe("--no-validate");
	});
});

describe("deployApplyArgs", () => {
	it("applies the whole project when unscoped, with no positional separator", () => {
		expect(deployApplyArgs("staging", undefined, false)).toEqual([
			"deploy",
			"--yes",
			"--json",
			"--stream",
			"--env",
			"staging",
		]);
	});

	it("scopes to a projection with the name last, after --", () => {
		expect(deployApplyArgs("staging", "orders", false)).toEqual([
			"deploy",
			"--yes",
			"--json",
			"--stream",
			"--env",
			"staging",
			"--",
			"orders",
		]);
	});

	it("places --no-validate before the -- separator and the name", () => {
		expect(deployApplyArgs("staging", "orders", true)).toEqual([
			"deploy",
			"--yes",
			"--json",
			"--stream",
			"--env",
			"staging",
			"--no-validate",
			"--",
			"orders",
		]);
	});

	it("keeps a dash-leading projection name a positional, not a flag", () => {
		const args = deployApplyArgs("staging", "--env", true);
		expect(args.at(-2)).toBe("--");
		expect(args.at(-1)).toBe("--env");
	});
});
