import * as fs from "node:fs";
import * as os from "node:os";
import * as path from "node:path";
import { afterEach, beforeEach, describe, expect, it } from "vitest";

import { load, loadSafe, save, type TelemetryConfig } from "./config.js";

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

describe("config.loadSafe", () => {
	let dir: string;

	beforeEach(() => {
		dir = fs.mkdtempSync(path.join(os.tmpdir(), "gaffer-telemetry-config-"));
	});
	afterEach(() => {
		fs.rmSync(dir, { recursive: true, force: true });
	});

	it("returns empty for a fresh install (no file)", async () => {
		expect(await loadSafe(dir)).toEqual({});
	});

	it("returns the parsed config when the file is well-formed", async () => {
		const expected: TelemetryConfig = { telemetry_id: "abc", disclosed: true };
		await save(dir, expected);
		expect(await loadSafe(dir)).toEqual(expected);
	});

	it("quarantines a malformed file and returns empty", async () => {
		fs.writeFileSync(path.join(dir, "telemetry.json"), "{not json");
		const result = await loadSafe(dir);
		expect(result).toEqual({});

		// Original is gone; a sibling .corrupt-<ts> survives with the
		// original bytes for forensics.
		expect(fs.existsSync(path.join(dir, "telemetry.json"))).toBe(false);
		const quarantined = fs
			.readdirSync(dir)
			.filter((f) => f.startsWith("telemetry.json.corrupt-"));
		expect(quarantined.length).toBe(1);
		const first = quarantined[0];
		if (first === undefined) throw new Error("expected a quarantined file");
		expect(fs.readFileSync(path.join(dir, first), "utf8")).toBe("{not json");
	});

	it("quarantines a non-object top level (validation error from load)", async () => {
		fs.writeFileSync(
			path.join(dir, "telemetry.json"),
			JSON.stringify(["nope"]),
		);
		expect(await loadSafe(dir)).toEqual({});
		expect(
			fs.readdirSync(dir).filter((f) => f.startsWith("telemetry.json.corrupt-"))
				.length,
		).toBe(1);
	});

	it.skipIf(
		process.platform === "win32" ||
			process.getuid === undefined ||
			process.getuid() === 0,
	)(
		"propagates I/O errors instead of quarantining (don't destroy a valid identity)",
		async () => {
			// EACCES on read should NOT trigger quarantine - a transient
			// permission glitch with a real identity on disk would
			// otherwise rename the file aside and re-mint. Force EACCES
			// via chmod(0o000); the skip-if above keeps this off Windows
			// (no POSIX permission bits) and off root (which bypasses
			// the check).
			const file = path.join(dir, "telemetry.json");
			fs.writeFileSync(file, JSON.stringify({ telemetry_id: "important" }));
			fs.chmodSync(file, 0o000);
			try {
				await expect(loadSafe(dir)).rejects.toThrow();
				// File is still there - not quarantined.
				expect(fs.existsSync(file)).toBe(true);
			} finally {
				fs.chmodSync(file, 0o600);
			}
		},
	);
});
