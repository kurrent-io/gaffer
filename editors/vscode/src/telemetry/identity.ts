// Per-install identity and per-process run id. Mirrors the CLI's
// telemetry.Identity (UUID v4) so dashboards pivot on `emitter_id`
// the same way regardless of surface.

import { randomUUID } from "node:crypto";

import type { TelemetryConfig } from "./config.js";

export interface Identity {
	/** Per-install; persisted; sent as envelope `emitter_id`. */
	telemetryId: string;
	/** Per-process; in-memory only; sent as envelope `run_id`. */
	runId: string;
}

/** Mint a fresh identity. UUID v4 (Node's `crypto.randomUUID()`),
 * matching the CLI's `uuid.NewRandom()`.
 */
export function mint(): Identity {
	return {
		telemetryId: randomUUID(),
		runId: randomUUID(),
	};
}

/**
 * Adopt a persisted identity, pairing it with a fresh per-process
 * runId. Returns null when no persisted id exists - the caller
 * decides whether to mint.
 */
export function fromConfig(config: TelemetryConfig): Identity | null {
	if (!config.telemetry_id) return null;
	return {
		telemetryId: config.telemetry_id,
		runId: randomUUID(),
	};
}

/**
 * Resolve an identity for the current process: adopt the persisted
 * one if present, otherwise mint a fresh id and persist it via the
 * `persistMint` callback. The callback merges the new field into the
 * on-disk config (see `editConfig` in config.ts) rather than
 * overwriting, so a concurrent disclosure-runner write doesn't drop
 * the freshly-minted id.
 */
export async function ensureIdentity(
	config: TelemetryConfig,
	persistMint: (patch: Partial<TelemetryConfig>) => Promise<void>,
): Promise<Identity> {
	const existing = fromConfig(config);
	if (existing !== null) return existing;
	const fresh = mint();
	await persistMint({ telemetry_id: fresh.telemetryId });
	return fresh;
}
