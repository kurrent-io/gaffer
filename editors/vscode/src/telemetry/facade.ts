// Activation-time orchestrator. Wires the config / identity /
// opt-out / envelope / sink primitives into a single object the
// rest of the extension uses as the telemetry API:
//
//   const telemetry = await createTelemetry({...});
//   telemetry.emit(extensionActivatedEvent);
//   ...
//   void disclosurePromise.then(() => telemetry.refreshOptOut());
//   ...
//   await telemetry.drain(4500);  // on deactivate
//
// Opt-out is snapshotted at construction. The caller calls
// refreshOptOut() after any persisted state change (notably the
// first-run notification's write) so a late-firing exception emit
// honours a mid-session `[Disable]` click.
//
// Identity persist + disclosure write happen on independent
// activation paths and both touch telemetry.json. Both writers go
// through editConfig (re-read + merge + save) so neither drops the
// other's fields when their saves interleave.
//
// Telemetry init never aborts activation - on any I/O error (config
// read, identity persist) we log and fall back to the no-op handle.

import type { Event } from "@kurrent/gaffer-telemetry";

import { editConfig, loadSafe, type TelemetryConfig } from "./config.js";
import { buildEnvelope } from "./envelope.js";
import { buildException, type Phase } from "./exception.js";
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
	/** Spawner identity for cross-surface stitching (not used in v0 - no
	 * one spawns the extension; here for symmetry with the CLI). */
	invokerId?: string;
	/** Resolves workspace folder paths at report-time so exception
	 * scrubbing reflects mid-session workspace changes. */
	getWorkspaceFolders?: () => readonly string[];
	/** Absolute path of the extension bundle root, for exception
	 * frame in_app classification. */
	extensionPath?: string;
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
	/** Re-read the persisted config and update the opt-out snapshot.
	 * Call after the first-run notification's write so a `[Disable]`
	 * click silences emits that haven't fired yet. */
	refreshOptOut(): Promise<void>;
	/** Per-install `emitter_id` to pass to spawned CLI processes as
	 * `--invoker-id`. Returns `null` when opt-out is active (we have
	 * no identity to share) or when the facade fell back to no-op. */
	invokerId(): string | null;
	/** Build + emit an exception envelope for `err`. No-op when opt-out
	 * is active; never throws (any failure inside the report path is
	 * swallowed so the caller's original error always propagates). */
	reportException(phase: Phase, err: unknown): void;
}

export async function createTelemetry(
	opts: TelemetryFacadeOptions,
): Promise<Telemetry> {
	try {
		return await createTelemetryImpl(opts);
	} catch (err) {
		opts.log(
			`telemetry: init failed, falling back to no-op: ${err instanceof Error ? err.message : String(err)}`,
		);
		return noopTelemetry();
	}
}

async function createTelemetryImpl(
	opts: TelemetryFacadeOptions,
): Promise<Telemetry> {
	const initialConfig = await loadSafe(opts.storageDir);
	if (currentOptOut(opts, initialConfig).disabled) {
		return noopTelemetry();
	}

	const identity = await ensureIdentity(initialConfig, (patch) =>
		editConfig(opts.storageDir, patch),
	);

	const sink = createSink({
		url: opts.sinkUrl ?? INGEST_URL,
		debug: isDebugEnv(opts.env),
		log: opts.log,
		...(opts.fetchImpl !== undefined && { fetchImpl: opts.fetchImpl }),
	});

	// One-way latch: once disabled mid-session, stays disabled. The
	// only realistic mid-session transition is permissive -> disabled
	// (user clicks `[Disable]` in the first-run notification);
	// transitions in the other direction need a fresh activation
	// anyway because env-var and VS Code-level signals are read at
	// construction. The latch only affects future spawns; CLI
	// children already running carry the linkage flags they were
	// started with until they exit naturally.
	let disabled = false;

	const emit = (event: Event): void => {
		if (disabled) return;
		sink.send(
			buildEnvelope({
				identity,
				libVersion: opts.libVersion,
				events: [event],
				env: opts.env,
				...(opts.invokerId !== undefined && { invokerId: opts.invokerId }),
			}),
		);
	};

	return {
		emit,
		drain(timeoutMs: number): Promise<void> {
			return sink.drain(timeoutMs);
		},
		async refreshOptOut(): Promise<void> {
			if (disabled) return;
			const fresh = await loadSafe(opts.storageDir);
			if (currentOptOut(opts, fresh).disabled) disabled = true;
		},
		invokerId(): string | null {
			return disabled ? null : identity.telemetryId;
		},
		reportException(phase: Phase, err: unknown): void {
			if (disabled) return;
			try {
				emit(
					buildException({
						err,
						phase,
						extensionPath: opts.extensionPath ?? "",
						workspaceFolders: opts.getWorkspaceFolders?.() ?? [],
					}),
				);
			} catch (reportErr) {
				opts.log(
					`telemetry: exception report failed: ${reportErr instanceof Error ? reportErr.message : String(reportErr)}`,
				);
			}
		},
	};
}

function noopTelemetry(): Telemetry {
	return {
		emit: () => {},
		drain: async () => {},
		refreshOptOut: async () => {},
		invokerId: () => null,
		reportException: () => {},
	};
}

function currentOptOut(
	opts: TelemetryFacadeOptions,
	config: TelemetryConfig,
): { disabled: boolean } {
	return checkOptOut({
		config,
		env: opts.env,
		vscodeTelemetryLevel: opts.vscodeTelemetryLevel,
	});
}

/** Same env-var contract as the CLI's GAFFER_TELEMETRY_DEBUG. */
function isDebugEnv(env: NodeJS.ProcessEnv): boolean {
	const raw = env.GAFFER_TELEMETRY_DEBUG;
	if (raw === undefined) return false;
	const v = raw.trim().toLowerCase();
	return v === "1" || v === "true" || v === "yes" || v === "on";
}
