import { env } from "cloudflare:test";
import { beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
import { maybeFireMerge } from "../src/merging";
import { applyMigrations, resetTables } from "./migrations";

const emitterA = "00000000-0000-0000-0000-0000000000aa";
const invokerA = "00000000-0000-0000-0000-00000000ffff";
const emitterB = "00000000-0000-0000-0000-0000000000bb";

beforeAll(async () => {
	await applyMigrations(env.DB);
});

beforeEach(async () => {
	await resetTables(env.DB);
});

describe("maybeFireMerge", () => {
	it("fires the merge and persists the pair on success", async () => {
		const fire = vi.fn(async () => true);
		await maybeFireMerge(emitterA, invokerA, env.DB, fire);

		expect(fire).toHaveBeenCalledTimes(1);
		expect(fire).toHaveBeenCalledWith(emitterA, invokerA);

		const row = await env.DB.prepare(`SELECT 1 FROM merged_pairs WHERE emitter_id = ?1 AND invoker_id = ?2`)
			.bind(emitterA, invokerA)
			.first();
		expect(row).not.toBeNull();
	});

	it("does not refire for the same pair", async () => {
		const fire = vi.fn(async () => true);
		await maybeFireMerge(emitterA, invokerA, env.DB, fire);
		await maybeFireMerge(emitterA, invokerA, env.DB, fire);
		await maybeFireMerge(emitterA, invokerA, env.DB, fire);

		expect(fire).toHaveBeenCalledTimes(1);
	});

	it("skips the refresh write when expires_at is still comfortably distant", async () => {
		const fire = vi.fn(async () => true);
		await maybeFireMerge(emitterA, invokerA, env.DB, fire);

		const before = (await env.DB.prepare(
			`SELECT expires_at FROM merged_pairs WHERE emitter_id = ?1 AND invoker_id = ?2`,
		)
			.bind(emitterA, invokerA)
			.first()) as { expires_at: number };

		// Fresh insert is a full 90d in the future; the refresh-floor is
		// 60d, so a repeat right now shouldn't issue an UPDATE.
		await maybeFireMerge(emitterA, invokerA, env.DB, fire);

		const after = (await env.DB.prepare(`SELECT expires_at FROM merged_pairs WHERE emitter_id = ?1 AND invoker_id = ?2`)
			.bind(emitterA, invokerA)
			.first()) as { expires_at: number };

		expect(after.expires_at).toBe(before.expires_at);
	});

	it("refreshes TTL when expires_at is within the refresh floor", async () => {
		const fire = vi.fn(async () => true);
		await maybeFireMerge(emitterA, invokerA, env.DB, fire);

		// Force the existing expiry to a near-term value so the next call
		// has to refresh it.
		const nearTerm = Date.now() + 30 * 24 * 60 * 60 * 1000;
		await env.DB.prepare(
			`UPDATE merged_pairs SET expires_at = ?1
			 WHERE emitter_id = ?2 AND invoker_id = ?3`,
		)
			.bind(nearTerm, emitterA, invokerA)
			.run();

		await maybeFireMerge(emitterA, invokerA, env.DB, fire);

		const after = (await env.DB.prepare(`SELECT expires_at FROM merged_pairs WHERE emitter_id = ?1 AND invoker_id = ?2`)
			.bind(emitterA, invokerA)
			.first()) as { expires_at: number };

		expect(after.expires_at).toBeGreaterThan(nearTerm);
	});

	it("does not persist when the fire returns false", async () => {
		const fire = vi.fn(async () => false);
		await maybeFireMerge(emitterA, invokerA, env.DB, fire);

		expect(fire).toHaveBeenCalledTimes(1);

		const row = await env.DB.prepare(`SELECT 1 FROM merged_pairs WHERE emitter_id = ?1 AND invoker_id = ?2`)
			.bind(emitterA, invokerA)
			.first();
		expect(row).toBeNull();
	});

	it("retries on the next call if the previous fire failed", async () => {
		const fire = vi.fn().mockResolvedValueOnce(false).mockResolvedValueOnce(true);
		await maybeFireMerge(emitterA, invokerA, env.DB, fire);
		await maybeFireMerge(emitterA, invokerA, env.DB, fire);

		expect(fire).toHaveBeenCalledTimes(2);

		const row = await env.DB.prepare(`SELECT 1 FROM merged_pairs WHERE emitter_id = ?1 AND invoker_id = ?2`)
			.bind(emitterA, invokerA)
			.first();
		expect(row).not.toBeNull();
	});

	it("fires once per distinct (emitter, invoker) pair", async () => {
		const fire = vi.fn(async () => true);
		await maybeFireMerge(emitterA, invokerA, env.DB, fire);
		await maybeFireMerge(emitterB, invokerA, env.DB, fire);

		expect(fire).toHaveBeenCalledTimes(2);
	});

	it("refires after the pair record has expired", async () => {
		const fire = vi.fn(async () => true);
		await maybeFireMerge(emitterA, invokerA, env.DB, fire);

		// Force the row's expires_at to the past.
		await env.DB.prepare(
			`UPDATE merged_pairs SET expires_at = ?1
			 WHERE emitter_id = ?2 AND invoker_id = ?3`,
		)
			.bind(Date.now() - 1000, emitterA, invokerA)
			.run();

		await maybeFireMerge(emitterA, invokerA, env.DB, fire);
		expect(fire).toHaveBeenCalledTimes(2);
	});
});
