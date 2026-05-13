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
