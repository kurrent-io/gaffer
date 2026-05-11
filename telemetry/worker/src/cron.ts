// Daily prune of expired rows from the session-stitching and identity-merge
// tables.
//
// All read paths filter on `expires_at > now`, so functional correctness
// holds even if a row stays past its expiry; this is purely to stop the
// tables growing unbounded. Scheduled via a Cloudflare cron trigger
// (see `wrangler.jsonc`).

export async function prune(db: D1Database, now: number = Date.now()): Promise<void> {
	await db.batch([
		db.prepare(`DELETE FROM session_by_user WHERE expires_at < ?1`).bind(now),
		db.prepare(`DELETE FROM session_by_run WHERE expires_at < ?1`).bind(now),
		db.prepare(`DELETE FROM merged_pairs WHERE expires_at < ?1`).bind(now),
	]);
}
