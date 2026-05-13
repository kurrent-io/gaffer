// Per-install identity and per-process run id. Mirrors the CLI's
// telemetry.Identity (UUID v4 for all three fields) so dashboards
// pivot on `emitter_id` the same way regardless of surface.

import { createHmac, randomUUID } from "node:crypto";

import type { TelemetryConfig } from "./config.js";

export interface Identity {
	/** Per-install; persisted; sent as envelope `emitter_id`. */
	telemetryId: string;
	/** Per-install; persisted; HMAC key for project_id derivation. Never sent. */
	salt: string;
	/** Per-process; in-memory only; sent as envelope `run_id`. */
	runId: string;
}

/** Mint a fresh identity. UUID v4 for all three fields (Node's
 * `crypto.randomUUID()`), matching the CLI's `uuid.NewRandom()` so the
 * wire format is identical across surfaces.
 */
export function mint(): Identity {
	return {
		telemetryId: randomUUID(),
		salt: randomUUID(),
		runId: randomUUID(),
	};
}

/**
 * Adopt a persisted identity, pairing it with a fresh per-process
 * runId. Returns null when either persistent half is missing - the
 * caller decides whether to mint.
 */
export function fromConfig(config: TelemetryConfig): Identity | null {
	if (!config.telemetry_id || !config.salt) return null;
	return {
		telemetryId: config.telemetry_id,
		salt: config.salt,
		runId: randomUUID(),
	};
}

/**
 * Resolve an identity for the current process: adopt the persisted
 * one if present, otherwise mint a fresh pair and persist it via
 * the `persistMint` callback. The callback is expected to merge the
 * new fields into the on-disk config (see `editConfig` in config.ts)
 * rather than overwrite it, so a concurrent disclosure-runner write
 * doesn't drop the freshly-minted id/salt.
 *
 * Concurrent first-mints from two extension hosts on the same
 * install still last-writer-wins on disk. The losing process emits
 * one session's worth of envelopes under its own id; next activation
 * reads the persisted winner and converges.
 */
export async function ensureIdentity(
	config: TelemetryConfig,
	persistMint: (patch: Partial<TelemetryConfig>) => Promise<void>,
): Promise<Identity> {
	const existing = fromConfig(config);
	if (existing !== null) return existing;
	const fresh = mint();
	await persistMint({
		telemetry_id: fresh.telemetryId,
		salt: fresh.salt,
	});
	return fresh;
}

/**
 * Derive the wire-format `project_id` for the given absolute project
 * root. HMAC-SHA256(salt, absRoot), first 8 bytes hex - matches the
 * CLI's `deriveID` in `cli/internal/telemetry/identity.go`. Extension
 * and CLI hash to different ids for the same project by design
 * (independent salts); the algorithm is the shared contract.
 *
 * Callers MUST pass a cleaned absolute path so the same project hashes
 * consistently across runs.
 */
export function projectId(salt: string, absProjectRoot: string): string {
	return createHmac("sha256", salt)
		.update(absProjectRoot)
		.digest("hex")
		.slice(0, 16);
}
