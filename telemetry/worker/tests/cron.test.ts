import { env } from "cloudflare:test";
import { beforeAll, beforeEach, describe, expect, it } from "vitest";
import { prune } from "../src/cron";
import { applyMigrations, resetTables } from "./migrations";

beforeAll(async () => {
	await applyMigrations(env.DB);
});

beforeEach(async () => {
	await resetTables(env.DB);
});

async function seedRow(table: string, columns: Record<string, string | number>) {
	const cols = Object.keys(columns);
	const placeholders = cols.map((_, i) => `?${i + 1}`).join(", ");
	const values = Object.values(columns);
	await env.DB.prepare(`INSERT INTO ${table} (${cols.join(", ")}) VALUES (${placeholders})`)
		.bind(...values)
		.run();
}

describe("prune", () => {
	it("deletes expired rows from all three tables", async () => {
		const now = Date.now();
		const past = now - 1000;
		const future = now + 60 * 60 * 1000;

		await seedRow("session_by_user", {
			emitter_id: "expired",
			session_id: "s1",
			session_started_at: past,
			last_seen_at: past,
			expires_at: past,
		});
		await seedRow("session_by_user", {
			emitter_id: "fresh",
			session_id: "s2",
			session_started_at: now,
			last_seen_at: now,
			expires_at: future,
		});
		await seedRow("session_by_run", {
			run_id: "expired-run",
			session_id: "s1",
			session_started_at: past,
			expires_at: past,
		});
		await seedRow("session_by_run", {
			run_id: "fresh-run",
			session_id: "s2",
			session_started_at: now,
			expires_at: future,
		});
		await seedRow("merged_pairs", {
			emitter_id: "expired",
			invoker_id: "i1",
			expires_at: past,
		});
		await seedRow("merged_pairs", {
			emitter_id: "fresh",
			invoker_id: "i2",
			expires_at: future,
		});

		await prune(env.DB, now);

		const users = await env.DB.prepare(`SELECT emitter_id FROM session_by_user`).all();
		expect(users.results.map((r) => (r as { emitter_id: string }).emitter_id)).toEqual(["fresh"]);

		const runs = await env.DB.prepare(`SELECT run_id FROM session_by_run`).all();
		expect(runs.results.map((r) => (r as { run_id: string }).run_id)).toEqual(["fresh-run"]);

		const pairs = await env.DB.prepare(`SELECT emitter_id FROM merged_pairs`).all();
		expect(pairs.results.map((r) => (r as { emitter_id: string }).emitter_id)).toEqual(["fresh"]);
	});

	it("is a no-op when no rows have expired", async () => {
		const now = Date.now();
		const future = now + 60 * 60 * 1000;

		await seedRow("session_by_user", {
			emitter_id: "fresh",
			session_id: "s1",
			session_started_at: now,
			last_seen_at: now,
			expires_at: future,
		});

		await prune(env.DB, now);

		const users = await env.DB.prepare(`SELECT count(*) AS n FROM session_by_user`).first();
		expect((users as { n: number }).n).toBe(1);
	});
});
