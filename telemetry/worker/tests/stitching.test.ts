import { env } from "cloudflare:test";
import { beforeAll, beforeEach, describe, expect, it } from "vitest";
import { stitchSession } from "../src/stitching";
import { applyMigrations, resetTables } from "./migrations";

const emitterA = "00000000-0000-0000-0000-0000000000aa";
const emitterB = "00000000-0000-0000-0000-0000000000bb";
const runA = "00000000-0000-0000-0000-00000000aaaa";
const runB = "00000000-0000-0000-0000-00000000bbbb";

beforeAll(async () => {
	await applyMigrations(env.DB);
});

beforeEach(async () => {
	await resetTables(env.DB);
});

describe("stitchSession", () => {
	it("mints a new session_id when there's nothing to resurrect or continue", async () => {
		const sessionId = await stitchSession(emitterA, runA, env.DB);
		expect(sessionId).toMatch(/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/);
	});

	it("returns the same session_id for a second event from the same run", async () => {
		const first = await stitchSession(emitterA, runA, env.DB);
		const second = await stitchSession(emitterA, runA, env.DB);
		expect(second).toBe(first);
	});

	it("continues an active session for a different run on the same emitter", async () => {
		// Two runs on the same emitter, back-to-back, should share a session.
		const first = await stitchSession(emitterA, runA, env.DB);
		const second = await stitchSession(emitterA, runB, env.DB);
		expect(second).toBe(first);
	});

	it("gives different emitters different sessions", async () => {
		const a = await stitchSession(emitterA, runA, env.DB);
		const b = await stitchSession(emitterB, runB, env.DB);
		expect(a).not.toBe(b);
	});

	it("resurrects a run whose session_by_user row has been overwritten", async () => {
		// emitterA starts in session X via runA, several hours ago.
		const original = await stitchSession(emitterA, runA, env.DB);

		// Age both rows: push session_by_run AND session_by_user back 12h.
		// session_by_user gets a further bump to put `last_seen_at` outside
		// the 30 min inactivity window so the next emitter event mints fresh.
		const twelveHoursAgo = Date.now() - 12 * 60 * 60 * 1000;
		await env.DB.prepare(
			`UPDATE session_by_user
			 SET last_seen_at = ?1, session_started_at = ?1
			 WHERE emitter_id = ?2`,
		)
			.bind(twelveHoursAgo, emitterA)
			.run();
		await env.DB.prepare(
			`UPDATE session_by_run
			 SET session_started_at = ?1
			 WHERE run_id = ?2`,
		)
			.bind(twelveHoursAgo, runA)
			.run();

		// A different run on the same emitter mints a fresh session.
		const after = await stitchSession(emitterA, runB, env.DB);
		expect(after).not.toBe(original);

		// Now a late event from the original run arrives - 12h after the
		// run was last seen, still within the 24h cap. session_by_run
		// resurrects it into the original session rather than adopting the
		// new one minted by runB.
		const resurrected = await stitchSession(emitterA, runA, env.DB);
		expect(resurrected).toBe(original);
	});

	it("mints a fresh session when both run and user records are too old (24h cap)", async () => {
		const first = await stitchSession(emitterA, runA, env.DB);

		// Push both rows past the 24h cap.
		const ancient = Date.now() - 25 * 60 * 60 * 1000;
		await env.DB.prepare(
			`UPDATE session_by_user
			 SET session_started_at = ?1, last_seen_at = ?1
			 WHERE emitter_id = ?2`,
		)
			.bind(ancient, emitterA)
			.run();
		await env.DB.prepare(
			`UPDATE session_by_run
			 SET session_started_at = ?1
			 WHERE run_id = ?2`,
		)
			.bind(ancient, runA)
			.run();

		const next = await stitchSession(emitterA, runA, env.DB);
		expect(next).not.toBe(first);
	});

	it("doesn't resurrect from a session_by_run row that's already expired", async () => {
		const first = await stitchSession(emitterA, runA, env.DB);

		// Push session_by_run past expires_at AND clear session_by_user (so
		// the only candidate is the resurrection row, which should be
		// rejected on the expires_at predicate).
		await env.DB.prepare(`UPDATE session_by_run SET expires_at = ?1 WHERE run_id = ?2`)
			.bind(Date.now() - 1000, runA)
			.run();
		await env.DB.prepare(`DELETE FROM session_by_user WHERE emitter_id = ?1`).bind(emitterA).run();

		const next = await stitchSession(emitterA, runA, env.DB);
		expect(next).not.toBe(first);
	});

	it("serialises concurrent stitches for the same emitter", async () => {
		// Three concurrent stitches with the same emitter but distinct runs.
		// SQLite's row-level write lock should serialise them at step 1, so
		// all three land in the same session.
		//
		// Caveat: in a single isolate `Promise.all` may interleave at most
		// at the await boundary - the underlying D1 calls happen serially in
		// the test pool. This test confirms the same-session outcome rather
		// than directly proving the row-lock claim under wire-level
		// contention. A real concurrency stress test would need multiple
		// isolates.
		const [a, b, c] = await Promise.all([
			stitchSession(emitterA, runA, env.DB),
			stitchSession(emitterA, runB, env.DB),
			stitchSession(emitterA, "00000000-0000-0000-0000-00000000cccc", env.DB),
		]);
		expect(b).toBe(a);
		expect(c).toBe(a);
	});
});
