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

// Pair-persistence TTL. 30 days from last observation: the row is
// refreshed on use, so an active install never refires the merge, and an
// inactive install's row ages out 30 days after its final event. This
// matches the deletion-runbook commitment in the public notice (a user
// who emails to delete is by definition not emitting events anymore, so
// their row will be gone within 30 days of their last activity).
const MERGED_PAIR_TTL_MS = 30 * 24 * 60 * 60 * 1000;

// Skip the per-event UPDATE that refreshes the pair's expires_at if the
// existing expiry is still comfortably distant. A chatty CLI can emit
// many envelopes carrying the same `invoker_id` per session; refreshing
// expires_at on every one of them is wasted writes against D1's free-
// tier limits and doesn't meaningfully change behaviour.
const REFRESH_FLOOR_MS = 20 * 24 * 60 * 60 * 1000;

export interface MergeFirer {
	(emitterId: string, invokerId: string): Promise<boolean>;
}

export async function fireMergeDangerously(
	host: string,
	apiKey: string,
	emitterId: string,
	invokerId: string,
): Promise<boolean> {
	try {
		const res = await fetch(`${host}/batch`, {
			method: "POST",
			headers: { "content-type": "application/json" },
			body: JSON.stringify({
				api_key: apiKey,
				batch: [
					{
						event: "$merge_dangerously",
						distinct_id: emitterId,
						properties: {
							alias: invokerId,
							// Suppress PostHog GeoIP enrichment (same as
							// translateEnvelope; see translation.ts).
							$ip: null,
							$geoip_disable: true,
						},
					},
				],
			}),
			// Cap the outbound request so a hung PostHog doesn't sit on the
			// isolate's waitUntil budget.
			signal: AbortSignal.timeout(5000),
		});
		if (!res.ok) {
			console.error("fireMergeDangerously non-2xx:", res.status);
		}
		return res.ok;
	} catch (err) {
		console.error("fireMergeDangerously fetch failed:", err);
		return false;
	}
}

export async function maybeFireMerge(
	emitterId: string,
	invokerId: string,
	db: D1Database,
	fire: MergeFirer,
): Promise<void> {
	const now = Date.now();
	const expiry = now + MERGED_PAIR_TTL_MS;

	const seen = (await db
		.prepare(
			`SELECT expires_at FROM merged_pairs
			 WHERE emitter_id = ?1 AND invoker_id = ?2 AND expires_at > ?3
			 LIMIT 1`,
		)
		.bind(emitterId, invokerId, now)
		.first()) as { expires_at: number } | null;

	if (seen) {
		// Already merged. Refresh expires_at only if it's getting close to
		// running out; skip the write otherwise.
		if (seen.expires_at < now + REFRESH_FLOOR_MS) {
			await db
				.prepare(
					`UPDATE merged_pairs SET expires_at = ?3
					 WHERE emitter_id = ?1 AND invoker_id = ?2`,
				)
				.bind(emitterId, invokerId, expiry)
				.run();
		}
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
