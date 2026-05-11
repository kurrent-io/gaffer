// Daily prune of expired rows from the session-stitching and identity-merge
// tables.
//
// All read paths filter on `expires_at > now`, so functional correctness
// holds even if a row stays past its expiry; this is purely to stop the
// tables growing unbounded. Scheduled via a Cloudflare cron trigger
// (see `wrangler.jsonc`).
//
// DELETEs are bounded by a LIMIT and run in a loop until they stop
// returning rows. An unbounded `DELETE WHERE expires_at < now` would
// time out under cron's CPU budget if the cron has been failing for a
// while and a backlog has built up; the backlog would then never drain
// because every retry hits the same wall.

const DELETE_CHUNK_SIZE = 10000;
const MAX_CHUNKS_PER_RUN = 100;

const TABLES = ["session_by_user", "session_by_run", "merged_pairs"] as const;

export async function prune(db: D1Database, now: number = Date.now()): Promise<void> {
	for (const table of TABLES) {
		await pruneTable(db, table, now);
	}
}

async function pruneTable(db: D1Database, table: string, now: number): Promise<void> {
	const stmt = db.prepare(
		`DELETE FROM ${table}
		 WHERE rowid IN (SELECT rowid FROM ${table} WHERE expires_at < ?1 LIMIT ${DELETE_CHUNK_SIZE})`,
	);
	for (let i = 0; i < MAX_CHUNKS_PER_RUN; i++) {
		const result = await stmt.bind(now).run();
		// `meta.changes` is the row count affected by the last write, capped
		// at the LIMIT. A short batch (< LIMIT rows deleted) means there's
		// nothing more to drain. This saves the extra "returned 0" round-trip
		// we'd otherwise spend on the common case where the table has fewer
		// than CHUNK_SIZE expired rows.
		const changes = result.meta?.changes ?? 0;
		if (changes < DELETE_CHUNK_SIZE) {
			return;
		}
	}
}
