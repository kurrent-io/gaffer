// Activation-time orchestrator. Wires the config / identity /
// opt-out / envelope / sink primitives into a single object the
// rest of the extension uses as the telemetry API:
//
//   const telemetry = await createTelemetry({...});
//   telemetry.emit(extensionActivatedEvent);
//   ...
//   await telemetry.drain(4500);  // on deactivate
//
// Opt-out is evaluated once at construction from a snapshot of the
// persisted config. A `[Disable]` click in the first-run
// notification after construction applies to the next activation,
// not this one.
//
// Identity persist + disclosure write happen on independent
// activation paths and both touch telemetry.json. Both writers go
// through editConfig (re-read + merge + save) so neither drops the
// other's fields when their saves interleave.
//
// Telemetry init never aborts activation - on any I/O error (config
// read, identity persist) we log and fall back to the no-op handle.

import type { Event } from "@kurrent/gaffer-telemetry";

import { editConfig, loadSafe } from "./config.js";
import { buildEnvelope } from "./envelope.js";
import { ensureIdentity } from "./identity.js";
import { checkOptOut } from "./opt-out.js";
import { createSink, INGEST_URL } from "./sink.js";

export interface TelemetryFacadeOptions {
	/** `context.globalStorageUri.fsPath`. */
	storageDir: string;
	/** Extension semver, stamped on every envelope's `lib_version`. */
	libVersion: string;
	/** Process env. Injected for testability + CI detection. */
	env: NodeJS.ProcessEnv;
	/** VS Code's `telemetry.telemetryLevel`, or undefined when unset. */
	vscodeTelemetryLevel: string | undefined;
	/** Output sink for the debug-mode envelope print. */
	log: (line: string) => void;
	/** Salted project_id when the extension is in a workspace. */
	projectId?: string;
	/** Spawner identity for cross-surface stitching (not used in v0 - no
	 * one spawns the extension; here for symmetry with the CLI). */
	invokerId?: string;
	/** Override sink URL for tests / staging. Defaults to INGEST_URL. */
	sinkUrl?: string;
	/** Override fetch implementation (tests). */
	fetchImpl?: typeof globalThis.fetch;
}

export interface Telemetry {
	/** Fire-and-forget emit. No-op when opt-out is active. */
	emit(event: Event): void;
	/** Wait for in-flight sends to settle, bounded by timeout. */
	drain(timeoutMs: number): Promise<void>;
}

export async function createTelemetry(
	opts: TelemetryFacadeOptions,
): Promise<Telemetry> {
	try {
		return await createTelemetryImpl(opts);
	} catch (err) {
		// Telemetry must never abort activation. Permissions glitches
		// on globalStorageUri (EACCES on read, rename failure on
		// save) are recoverable on the next run; bubbling them out of
		// activate would make telemetry a hard dependency for the
		// whole extension.
		opts.log(
			`telemetry: init failed, falling back to no-op: ${err instanceof Error ? err.message : String(err)}`,
		);
		return noopTelemetry();
	}
}

async function createTelemetryImpl(
	opts: TelemetryFacadeOptions,
): Promise<Telemetry> {
	const config = await loadSafe(opts.storageDir);
	const optOut = checkOptOut({
		config,
		env: opts.env,
		vscodeTelemetryLevel: opts.vscodeTelemetryLevel,
	});
	if (optOut.disabled) {
		return noopTelemetry();
	}

	const identity = await ensureIdentity(config, (patch) =>
		editConfig(opts.storageDir, patch),
	);

	const sink = createSink({
		url: opts.sinkUrl ?? INGEST_URL,
		debug: isDebugEnv(opts.env),
		log: opts.log,
		...(opts.fetchImpl !== undefined && { fetchImpl: opts.fetchImpl }),
	});

	return {
		emit(event: Event): void {
			sink.send(
				buildEnvelope({
					identity,
					libVersion: opts.libVersion,
					events: [event],
					env: opts.env,
					...(opts.projectId !== undefined && { projectId: opts.projectId }),
					...(opts.invokerId !== undefined && { invokerId: opts.invokerId }),
				}),
			);
		},
		drain(timeoutMs: number): Promise<void> {
			return sink.drain(timeoutMs);
		},
	};
}

function noopTelemetry(): Telemetry {
	return {
		emit: () => {},
		drain: async () => {},
	};
}

/** Same env-var contract as the CLI's GAFFER_TELEMETRY_DEBUG. */
function isDebugEnv(env: NodeJS.ProcessEnv): boolean {
	const raw = env.GAFFER_TELEMETRY_DEBUG;
	if (raw === undefined) return false;
	const v = raw.trim().toLowerCase();
	return v === "1" || v === "true" || v === "yes" || v === "on";
}
