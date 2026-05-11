-- Session stitching tables.
--
-- session_by_user: keyed by the per-install identifier. Holds the active
-- session for a given install plus the inactivity-window expiry.
--
-- session_by_run: keyed by the per-process identifier. Lets long-idle
-- processes whose final event arrives hours after their last activity
-- "resurrect" into their original session instead of starting a new one.
--
-- expires_at columns replace KV's auto-TTL. A daily cron prunes
-- already-expired rows; reads always filter on expires_at > now so they
-- stay invisible even before the cron runs.

CREATE TABLE session_by_user (
	emitter_id          TEXT PRIMARY KEY,
	session_id          TEXT NOT NULL,
	session_started_at  INTEGER NOT NULL,
	last_seen_at        INTEGER NOT NULL,
	expires_at          INTEGER NOT NULL
);
CREATE INDEX idx_session_by_user_expires ON session_by_user(expires_at);

CREATE TABLE session_by_run (
	run_id              TEXT PRIMARY KEY,
	session_id          TEXT NOT NULL,
	session_started_at  INTEGER NOT NULL,
	expires_at          INTEGER NOT NULL
);
CREATE INDEX idx_session_by_run_expires ON session_by_run(expires_at);

-- Identity merge persistence. One row per (emitter_id, invoker_id) pair
-- we've fired $merge_dangerously for in PostHog. The row only lands on a
-- confirmed 2xx response - if PostHog returns non-2xx (or the request
-- never completes) the row stays absent and the next event retries.
-- PostHog's $merge_dangerously is idempotent for the same pair, so a
-- re-fire is safe.

CREATE TABLE merged_pairs (
	emitter_id  TEXT NOT NULL,
	invoker_id  TEXT NOT NULL,
	expires_at  INTEGER NOT NULL,
	PRIMARY KEY (emitter_id, invoker_id)
);
CREATE INDEX idx_merged_pairs_expires ON merged_pairs(expires_at);
