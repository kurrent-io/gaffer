import { describe, expect, it } from "vitest";

import { checkOptOut } from "./opt-out.js";

const permissive = {
	config: {},
	env: {} as NodeJS.ProcessEnv,
	vscodeTelemetryLevel: "all",
};

describe("checkOptOut", () => {
	it("returns disabled=false when no signal is set", () => {
		expect(checkOptOut(permissive)).toEqual({ disabled: false });
	});

	it("disables on GAFFER_TELEMETRY_OPTOUT=1", () => {
		expect(
			checkOptOut({ ...permissive, env: { GAFFER_TELEMETRY_OPTOUT: "1" } }),
		).toEqual({
			disabled: true,
			reason: { kind: "env", envVar: "GAFFER_TELEMETRY_OPTOUT", value: "1" },
		});
	});

	it("disables on KURRENTDB_TELEMETRY_OPTOUT=true", () => {
		expect(
			checkOptOut({
				...permissive,
				env: { KURRENTDB_TELEMETRY_OPTOUT: "true" },
			}),
		).toMatchObject({
			disabled: true,
			reason: { kind: "env", envVar: "KURRENTDB_TELEMETRY_OPTOUT" },
		});
	});

	it("disables on DO_NOT_TRACK=yes", () => {
		expect(
			checkOptOut({ ...permissive, env: { DO_NOT_TRACK: "yes" } }),
		).toMatchObject({
			disabled: true,
			reason: { kind: "env", envVar: "DO_NOT_TRACK" },
		});
	});

	it("ignores falsy env values", () => {
		for (const v of ["0", "false", "no", "off", ""]) {
			expect(checkOptOut({ ...permissive, env: { DO_NOT_TRACK: v } })).toEqual({
				disabled: false,
			});
		}
	});

	it("accepts truthy env values case-insensitively", () => {
		for (const v of ["1", "TRUE", "Yes", "ON"]) {
			expect(
				checkOptOut({ ...permissive, env: { DO_NOT_TRACK: v } }),
			).toMatchObject({ disabled: true });
		}
	});

	it("trims whitespace before checking truthiness (heredoc-set env hygiene)", () => {
		for (const v of [" 1", "1\n", "\ttrue", "  yes  "]) {
			expect(
				checkOptOut({ ...permissive, env: { DO_NOT_TRACK: v } }),
			).toMatchObject({ disabled: true });
		}
	});

	it("disables when telemetry.telemetryLevel is anything other than all", () => {
		for (const level of ["off", "crash", "error"]) {
			expect(
				checkOptOut({ ...permissive, vscodeTelemetryLevel: level }),
			).toEqual({
				disabled: true,
				reason: { kind: "vscode", level },
			});
		}
	});

	it("ignores an unset vscodeTelemetryLevel (default permissive)", () => {
		expect(
			checkOptOut({ ...permissive, vscodeTelemetryLevel: undefined }),
		).toEqual({ disabled: false });
	});

	it("disables when extension config has telemetry_enabled=false", () => {
		expect(
			checkOptOut({ ...permissive, config: { telemetry_enabled: false } }),
		).toEqual({ disabled: true, reason: { kind: "extension" } });
	});

	it("treats telemetry_enabled=true as permissive", () => {
		expect(
			checkOptOut({ ...permissive, config: { telemetry_enabled: true } }),
		).toEqual({ disabled: false });
	});

	it("reports env first when multiple signals are off (canonical reason ordering)", () => {
		expect(
			checkOptOut({
				config: { telemetry_enabled: false },
				env: { DO_NOT_TRACK: "1" },
				vscodeTelemetryLevel: "off",
			}),
		).toMatchObject({
			disabled: true,
			reason: { kind: "env", envVar: "DO_NOT_TRACK" },
		});
	});

	it("reports vscode before extension when env is permissive", () => {
		expect(
			checkOptOut({
				config: { telemetry_enabled: false },
				env: {} as NodeJS.ProcessEnv,
				vscodeTelemetryLevel: "off",
			}),
		).toMatchObject({
			disabled: true,
			reason: { kind: "vscode", level: "off" },
		});
	});

	it("reports the first env var found across the canonical order", () => {
		// GAFFER_TELEMETRY_OPTOUT comes before DO_NOT_TRACK in the list,
		// so when both are set we report the gaffer-specific one - it's
		// the more actionable signal for a gaffer user.
		expect(
			checkOptOut({
				...permissive,
				env: {
					GAFFER_TELEMETRY_OPTOUT: "1",
					DO_NOT_TRACK: "1",
				},
			}),
		).toMatchObject({
			disabled: true,
			reason: { kind: "env", envVar: "GAFFER_TELEMETRY_OPTOUT" },
		});
	});
});
