// Identity merge.
//
// When an envelope carries `invoker_id` in its context (set by extension-
// spawned CLI processes via the GAFFER_INVOKER env var), the worker
// associates the spawning extension's PostHog person with the spawned
// CLI's via `$merge_dangerously`. The decision of whether to fire is
// gated by a small D1 table so we don't refire on every event.
//
// Order: PostHog request FIRST, D1 write SECOND. Only a confirmed 2xx
// causes the pair to be persisted. If the fire fails, the row stays
// absent and the next event for the same pair retries. This makes the
// state machine "PostHog has been told" -> "we've recorded that we told
// it"; the other order would let us end up claiming we'd merged when
// PostHog never received the merge.
//
// `$merge_dangerously` is idempotent for the same `(distinct_id, alias)`
// pair, so a re-fire is safe.

// Pair-persistence TTL. 90 days is long enough that a normal user won't
// hit a refire from natural use; expired rows get pruned by the same
// daily cron that cleans the session tables.
const MERGED_PAIR_TTL_MS = 90 * 24 * 60 * 60 * 1000;

export interface MergeFirer {
	(emitterId: string, invokerId: string): Promise<boolean>;
}

export async function maybeFireMerge(
	emitterId: string,
	invokerId: string,
	db: D1Database,
	fire: MergeFirer,
): Promise<void> {
	const now = Date.now();
	const expiry = now + MERGED_PAIR_TTL_MS;

	const seen = await db
		.prepare(
			`SELECT 1 FROM merged_pairs
			 WHERE emitter_id = ?1 AND invoker_id = ?2 AND expires_at > ?3
			 LIMIT 1`,
		)
		.bind(emitterId, invokerId, now)
		.first();

	if (seen) {
		// Already merged once; just refresh the TTL.
		await db
			.prepare(
				`UPDATE merged_pairs SET expires_at = ?3
				 WHERE emitter_id = ?1 AND invoker_id = ?2`,
			)
			.bind(emitterId, invokerId, expiry)
			.run();
		return;
	}

	const ok = await fire(emitterId, invokerId);
	if (!ok) {
		// PostHog didn't confirm. Leave the table untouched; the next event
		// for this pair will retry.
		return;
	}

	await db
		.prepare(
			`INSERT INTO merged_pairs (emitter_id, invoker_id, expires_at)
			 VALUES (?1, ?2, ?3)
			 ON CONFLICT (emitter_id, invoker_id) DO UPDATE SET expires_at = excluded.expires_at`,
		)
		.bind(emitterId, invokerId, expiry)
		.run();
}
