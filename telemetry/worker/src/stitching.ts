// Worker-side session stitching.
//
// Clients send `run_id` per process but never `session_id`. The worker
// computes a `session_id` from `(emitter_id, run_id)` so a single user's
// activity across separate processes (terminal CLI, extension, MCP,
// extension-spawned CLI) groups into one journey.
//
// Decision rules (in priority order):
//  1. Resurrection: if `run_id` already maps to a session that hasn't
//     expired and started < 24h ago, use it. Lets long-idle processes
//     whose final event arrives hours later land in the right session.
//  2. Active continuation: if `emitter_id` has a session whose `last_seen`
//     is within 30 min and that started < 24h ago, use it.
//  3. Fresh: mint a new session_id.
//
// The decision is folded into a single SQL statement so there's no JS-side
// gap during which concurrent batches for the same emitter could race.

const INACTIVITY_WINDOW_MS = 30 * 60 * 1000;
const ABSOLUTE_CAP_MS = 24 * 60 * 60 * 1000;
// 24h cap + 1h slack so resurrection rows outlive the cap by enough to
// catch the late-arriving event that defined the cap.
const RUN_TTL_MS = 25 * 60 * 60 * 1000;

export async function stitchSession(emitterId: string, runId: string, db: D1Database): Promise<string> {
	const now = Date.now();
	const capThreshold = now - ABSOLUTE_CAP_MS;
	const userExpiry = now + INACTIVITY_WINDOW_MS;
	const runExpiry = now + RUN_TTL_MS;
	const proposedSessionId = crypto.randomUUID();

	// D1's `batch()` is internally transactional: statement 2 sees
	// statement 1's write, and SQLite's database-level write lock
	// serialises concurrent batches.
	//
	// We don't use `withSession("first-primary")` to force primary reads.
	// Replica lag could in theory split sessions for one user across colos,
	// but that requires their events to originate from different machines -
	// `emitter_id` is per-install on one host, and same-machine activity
	// shares a colo. If real traffic ever shows splits, withSession is the
	// fix at the cost of one primary round-trip per ingest.
	const result = await db.batch([
		// 1. Pick the winning candidate (resurrection / active / fresh) and
		//    upsert session_by_user. The candidate row supplies both
		//    `session_id` and `session_started_at` together so they can't
		//    drift apart.
		//
		//    Priority is encoded as a literal `pri` column: 1 = resurrection,
		//    2 = active continuation, 3 = fresh mint. `ORDER BY pri LIMIT 1`
		//    picks the lowest. Arm 3 always emits so the union is never empty.
		//
		//    A WITH-clause CTE version reads more linearly but workerd's D1
		//    SQLite rejects `WITH ... INSERT ... SELECT FROM cte ... ON
		//    CONFLICT DO UPDATE`. The derived-table form sidesteps it.
		//
		//    The `WHERE true` between the FROM and the UPSERT is the
		//    documented SQLite workaround for the parser ambiguity between
		//    `ON CONFLICT` (UPSERT) and `ON ...` (JOIN). See
		//    https://www.sqlite.org/lang_upsert.html#parsing_ambiguity.
		db
			.prepare(
				`
INSERT INTO session_by_user (emitter_id, session_id, session_started_at, last_seen_at, expires_at)
SELECT
  ?4 AS emitter_id,
  candidate.session_id,
  candidate.session_started_at,
  ?2 AS last_seen_at,
  ?6 AS expires_at
FROM (
  SELECT session_id, session_started_at, 1 AS pri FROM session_by_run
    WHERE run_id = ?1 AND expires_at > ?2 AND session_started_at > ?3
  UNION ALL
  SELECT session_id, session_started_at, 2 AS pri FROM session_by_user
    WHERE emitter_id = ?4 AND expires_at > ?2 AND session_started_at > ?3
  UNION ALL
  SELECT ?5, ?2, 3
  ORDER BY pri LIMIT 1
) AS candidate
WHERE true
ON CONFLICT (emitter_id) DO UPDATE SET
  session_id         = excluded.session_id,
  session_started_at = excluded.session_started_at,
  last_seen_at       = excluded.last_seen_at,
  expires_at         = excluded.expires_at
RETURNING session_id;
`,
			)
			.bind(runId, now, capThreshold, emitterId, proposedSessionId, userExpiry),

		// 2. Mirror the resolved session into session_by_run. Reads the
		//    just-written session_by_user row (same transaction, same view
		//    of data). Resurrection works because future stitches hit
		//    session_by_run first via run_id.
		db
			.prepare(
				`
INSERT INTO session_by_run (run_id, session_id, session_started_at, expires_at)
SELECT ?1, session_id, session_started_at, ?2
FROM session_by_user
WHERE emitter_id = ?3
ON CONFLICT (run_id) DO UPDATE SET
  session_id         = excluded.session_id,
  session_started_at = excluded.session_started_at,
  expires_at         = excluded.expires_at;
`,
			)
			.bind(runId, runExpiry, emitterId),
	]);

	const row = result[0]?.results?.[0] as { session_id: string } | undefined;
	if (!row) {
		// Should be unreachable: an INSERT ... ON CONFLICT DO UPDATE with a
		// RETURNING clause always yields exactly one row. Throwing here is
		// preferable to falling back to `proposedSessionId` - statement 1
		// has already committed *some* session_id into D1, and returning a
		// different one would leave this envelope orphaned from every
		// future event on the same emitter.
		throw new Error("stitchSession: RETURNING produced no row");
	}
	return row.session_id;
}
