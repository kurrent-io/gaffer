// Three-signal opt-out cascade. Any one off => off; we don't probe
// later signals once we've seen the first. Order is purely about
// reporting (which signal we name in the disabled reason) - the
// effective decision is an OR-fold.
//
// Signals:
//   - env vars: same set as CLI (GAFFER_TELEMETRY_OPTOUT,
//     KURRENTDB_TELEMETRY_OPTOUT, DO_NOT_TRACK), truthy values
//     1/true/yes/on after trim+lowercase. Match the CLI's isTruthy in
//     cli/internal/telemetry/optout.go so " 1\n" out of a heredoc-set
//     env opts out on both surfaces, not just the CLI. Env wins first
//     so the displayed reason is stable when multiple opt-outs coexist.
//   - VS Code's telemetry.telemetryLevel: anything other than "all"
//     opts out (off / error / crash). Matches the editor-level user
//     intent: "I've already declined telemetry for this session."
//   - The extension's own telemetry_enabled in telemetry.json:
//     explicit false from the first-run notification. `undefined`
//     means "no decision yet"; default permissive (matches CLI).

import type { TelemetryConfig } from "./config.js";

const OPT_OUT_ENV_VARS = [
	"GAFFER_TELEMETRY_OPTOUT",
	"KURRENTDB_TELEMETRY_OPTOUT",
	"DO_NOT_TRACK",
] as const;

const TRUTHY = new Set(["1", "true", "yes", "on"]);

export type OptOutReason =
	| { kind: "env"; envVar: string; value: string }
	| { kind: "vscode"; level: string }
	| { kind: "extension" };

export type OptOutResult =
	| { disabled: false }
	| { disabled: true; reason: OptOutReason };

export interface CheckOptOutArgs {
	config: TelemetryConfig;
	/** Process env (typically `process.env`). Injected so tests are deterministic. */
	env: NodeJS.ProcessEnv;
	/** `telemetry.telemetryLevel` value, or undefined when the setting is unset. */
	vscodeTelemetryLevel: string | undefined;
}

export function checkOptOut(args: CheckOptOutArgs): OptOutResult {
	for (const v of OPT_OUT_ENV_VARS) {
		const raw = args.env[v];
		if (raw !== undefined && TRUTHY.has(raw.trim().toLowerCase())) {
			return { disabled: true, reason: { kind: "env", envVar: v, value: raw } };
		}
	}
	if (
		args.vscodeTelemetryLevel !== undefined &&
		args.vscodeTelemetryLevel !== "all"
	) {
		return {
			disabled: true,
			reason: { kind: "vscode", level: args.vscodeTelemetryLevel },
		};
	}
	if (args.config.telemetry_enabled === false) {
		return { disabled: true, reason: { kind: "extension" } };
	}
	return { disabled: false };
}
