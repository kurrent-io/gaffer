import * as fs from "node:fs";
import * as os from "node:os";
import * as path from "node:path";
import { afterEach, beforeEach, describe, expect, it } from "vitest";

import { load, save, type TelemetryConfig } from "./config.js";

describe("config.load", () => {
	let dir: string;

	beforeEach(() => {
		dir = fs.mkdtempSync(path.join(os.tmpdir(), "gaffer-telemetry-config-"));
	});
	afterEach(() => {
		fs.rmSync(dir, { recursive: true, force: true });
	});

	it("returns an empty object when the file does not exist", async () => {
		expect(await load(dir)).toEqual({});
	});

	it("round-trips a fully-populated config", async () => {
		const original: TelemetryConfig = {
			telemetry_id: "8f2b1a4c-9e7d-4a3e-b5f2-7c8a9d4e1f02",
			salt: "11111111-2222-3333-4444-555555555555",
			telemetry_enabled: true,
			disclosed: true,
		};
		await save(dir, original);
		expect(await load(dir)).toEqual(original);
	});

	it("ignores extraneous fields (forward-compat with future writers)", async () => {
		fs.writeFileSync(
			path.join(dir, "telemetry.json"),
			JSON.stringify({ telemetry_id: "abc", future_field: "ignore me" }),
		);
		expect(await load(dir)).toEqual({ telemetry_id: "abc" });
	});

	it("drops fields with wrong types (defensive against hand-edits)", async () => {
		fs.writeFileSync(
			path.join(dir, "telemetry.json"),
			JSON.stringify({
				telemetry_id: "abc",
				salt: 42, // wrong type
				telemetry_enabled: "yes", // wrong type
				disclosed: true,
			}),
		);
		expect(await load(dir)).toEqual({ telemetry_id: "abc", disclosed: true });
	});

	it("throws on a non-object top level", async () => {
		fs.writeFileSync(
			path.join(dir, "telemetry.json"),
			JSON.stringify(["nope"]),
		);
		await expect(load(dir)).rejects.toThrow(/expected a JSON object/);
	});

	it("throws on malformed JSON", async () => {
		fs.writeFileSync(path.join(dir, "telemetry.json"), "{not json");
		await expect(load(dir)).rejects.toThrow();
	});
});

describe("config.save", () => {
	let dir: string;

	beforeEach(() => {
		dir = fs.mkdtempSync(path.join(os.tmpdir(), "gaffer-telemetry-config-"));
	});
	afterEach(() => {
		fs.rmSync(dir, { recursive: true, force: true });
	});

	it("creates the storage directory if missing", async () => {
		const nested = path.join(dir, "nested", "globalStorage");
		await save(nested, { telemetry_id: "abc" });
		expect(fs.existsSync(path.join(nested, "telemetry.json"))).toBe(true);
	});

	it("writes the file atomically (no .tmp left behind on success)", async () => {
		await save(dir, { telemetry_id: "abc" });
		const stragglers = fs.readdirSync(dir).filter((f) => f.endsWith(".tmp"));
		expect(stragglers).toEqual([]);
	});

	it("overwrites an existing file", async () => {
		await save(dir, { telemetry_id: "first" });
		await save(dir, { telemetry_id: "second" });
		const loaded = await load(dir);
		expect(loaded.telemetry_id).toBe("second");
	});

	it("uses 0o600 mode on the written file", async () => {
		await save(dir, { telemetry_id: "abc" });
		const stat = fs.statSync(path.join(dir, "telemetry.json"));
		// Mask off file-type bits; compare just permissions.
		expect(stat.mode & 0o777).toBe(0o600);
	});

	it("removes the .tmp file when rename fails (no identity leak)", async () => {
		// Force the rename to fail by making the final path a directory:
		// rename(file, existing-dir) fails with EISDIR/EPERM, and the
		// .tmp would otherwise survive containing telemetry_id + salt.
		fs.mkdirSync(path.join(dir, "telemetry.json"));
		await expect(
			save(dir, { telemetry_id: "leaked", salt: "leaked" }),
		).rejects.toThrow();
		const stragglers = fs.readdirSync(dir).filter((f) => f.endsWith(".tmp"));
		expect(stragglers).toEqual([]);
	});
});
