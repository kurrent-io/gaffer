// Build the wire envelope. Pure constructor over the generated types
// from @kurrent/gaffer-telemetry - no fetch, no sink interaction; the
// sink wraps a built envelope into a POST.
//
// OS/arch/runtime_environment are detected from Node globals rather
// than passed in; they don't change within a process and threading
// them through every emit site is noise. process.env is injected
// (CI detection peeks it) so tests stay deterministic.

import type {
	Arch,
	Context,
	Envelope,
	Event,
	OS,
	RuntimeEnvironment,
} from "@kurrent/gaffer-telemetry";

import type { Identity } from "./identity.js";

/** Inputs the envelope builder takes from the caller per emit. */
export interface EnvelopeInput {
	identity: Identity;
	libVersion: string;
	events: Event[];
	env: NodeJS.ProcessEnv;
	invokerId?: string;
}

export function buildEnvelope(input: EnvelopeInput): Envelope {
	const context: Context = {
		emitter: "vscode",
		lib_version: input.libVersion,
		os: detectOS(),
		arch: detectArch(),
		runtime_environment: detectRuntimeEnvironment(input.env),
	};
	if (input.invokerId !== undefined) context.invoker_id = input.invokerId;
	return {
		schema_version: "1",
		emitter_id: input.identity.telemetryId,
		run_id: input.identity.runId,
		context,
		events: input.events,
	};
}

/**
 * Map Node's `process.platform` to the schema's OS enum. Unknown
 * values pass through cast - the worker drops envelopes outside the
 * enum, so an exotic platform loses its telemetry rather than
 * crashing extension activation. Matches `mapGoOS` on the CLI side.
 */
function detectOS(): OS {
	switch (process.platform) {
		case "linux":
			return "linux";
		case "darwin":
			return "darwin";
		case "win32":
			return "windows";
		default:
			return process.platform as OS;
	}
}

function detectArch(): Arch {
	switch (process.arch) {
		case "x64":
			return "x64";
		case "arm64":
			return "arm64";
		case "ia32":
			return "ia32";
		default:
			return process.arch as Arch;
	}
}

/**
 * "ci" if any of the canonical CI env vars is set, else "local".
 * Matches `cli/internal/telemetry/emit.go:detectRuntimeEnv`; widening
 * this list is a shared contract change.
 */
function detectRuntimeEnvironment(env: NodeJS.ProcessEnv): RuntimeEnvironment {
	for (const k of ["CI", "TEAMCITY_VERSION", "JENKINS_URL"] as const) {
		if (env[k] !== undefined && env[k] !== "") return "ci";
	}
	return "local";
}
