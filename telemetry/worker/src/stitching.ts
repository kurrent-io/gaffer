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
// The whole decision is folded into a single SQL statement (CTE-based
// upsert against session_by_user) so there's no JS-side gap during which
// concurrent batches for the same emitter could race. A second statement
// in the same D1 batch mirrors the resolved session into session_by_run
// for resurrection lookups.

const INACTIVITY_WINDOW_MS = 30 * 60 * 1000;
const ABSOLUTE_CAP_MS = 24 * 60 * 60 * 1000;
// 24h cap + 1h slack so resurrection rows outlive the cap by enough to
// catch the late-arriving event that defined the cap.
const RUN_TTL_MS = 25 * 60 * 60 * 1000;

export async function stitchSession(emitterId: string, runId: string, db: D1Database): Promise<string> {
	const now = Date.now();
	const inactivityThreshold = now - INACTIVITY_WINDOW_MS;
	const capThreshold = now - ABSOLUTE_CAP_MS;
	const userExpiry = now + INACTIVITY_WINDOW_MS;
	const runExpiry = now + RUN_TTL_MS;
	const proposedSessionId = crypto.randomUUID();

	// D1's `batch()` runs the array as a single implicit transaction:
	// statement 2's read sees statement 1's write, and SQLite's row-level
	// write lock serialises concurrent batches for the same emitter_id.
	// That's what makes the stitch atomic; we don't need the Sessions API
	// here because we never thread a bookmark across multiple round-trips.
	const result = await db.batch([
		// 1. Resolve session_id and session_started_at via nested COALESCEs,
		//    upsert session_by_user, RETURN it. Atomic within a single
		//    statement; SQLite's row-level write lock serialises concurrent
		//    batches for the same emitter_id.
		//
		//    A WITH-clause CTE version reads more linearly but workerd's
		//    D1 SQLite rejects `WITH ... INSERT ... SELECT FROM cte ... ON
		//    CONFLICT DO UPDATE`. Inlining the candidate lookups as scalar
		//    subqueries in the INSERT SELECT works around it.
		db
			.prepare(
				`
INSERT INTO session_by_user (emitter_id, session_id, session_started_at, last_seen_at, expires_at)
SELECT
  ?4 AS emitter_id,
  COALESCE(
    (SELECT session_id FROM session_by_run
       WHERE run_id = ?1 AND expires_at > ?2 AND session_started_at > ?3),
    (SELECT session_id FROM session_by_user
       WHERE emitter_id = ?4 AND last_seen_at > ?5 AND session_started_at > ?3),
    ?6
  ) AS session_id,
  COALESCE(
    (SELECT session_started_at FROM session_by_run
       WHERE run_id = ?1 AND expires_at > ?2 AND session_started_at > ?3),
    (SELECT session_started_at FROM session_by_user
       WHERE emitter_id = ?4 AND last_seen_at > ?5 AND session_started_at > ?3),
    ?2
  ) AS session_started_at,
  ?2 AS last_seen_at,
  ?7 AS expires_at
ON CONFLICT (emitter_id) DO UPDATE SET
  session_id         = excluded.session_id,
  session_started_at = excluded.session_started_at,
  last_seen_at       = excluded.last_seen_at,
  expires_at         = excluded.expires_at
RETURNING session_id;
`,
			)
			.bind(runId, now, capThreshold, emitterId, inactivityThreshold, proposedSessionId, userExpiry),

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
