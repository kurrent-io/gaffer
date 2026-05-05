import * as os from "node:os";
import * as fs from "node:fs";
import * as path from "node:path";
import * as vscode from "vscode";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import {
	createProjectIndex,
	isWithin,
	normalizePath,
	projectionBlocks,
	scanLines,
} from "./project-index.js";
import { queueFindFiles } from "../../test/testutil/vscode-state.js";

describe("normalizePath", () => {
	it("collapses '..' and '.' segments", () => {
		expect(normalizePath("/a/b/../c")).toBe(path.normalize("/a/c"));
		expect(normalizePath("/a/./b")).toBe(path.normalize("/a/b"));
	});

	it("is idempotent on an already-normal path", () => {
		const p = path.normalize("/a/b/c");
		expect(normalizePath(p)).toBe(p);
	});
});

describe("isWithin", () => {
	it("returns true when child equals parent", () => {
		expect(isWithin("/a/b", "/a/b")).toBe(true);
	});

	it("returns true for a direct descendant", () => {
		expect(isWithin("/a/b/c", "/a/b")).toBe(true);
	});

	it("returns true for an internal '..' that resolves back inside the parent", () => {
		// fixtures/sub/../happy.json -> fixtures/happy.json. Only the
		// final cleaned form matters; transient .. segments shouldn't
		// trip the escape check.
		expect(isWithin("/a/b/sub/../inside.json", "/a/b")).toBe(true);
	});

	it("returns false when child escapes via ..", () => {
		expect(isWithin("/a/b/../../etc/passwd", "/a/b")).toBe(false);
	});

	it("returns false for a sibling that shares a prefix string", () => {
		expect(isWithin("/a/b2", "/a/b")).toBe(false);
	});

	it("returns false for an unrelated path", () => {
		expect(isWithin("/x/y", "/a/b")).toBe(false);
	});
});

describe("projectionBlocks", () => {
	it("returns one entry per [[projection]] block in source order", () => {
		const text = `
[[projection]]
name = "a"
entry = "a.js"

[[projection]]
name = "b"
entry = "b.js"
`;
		expect(projectionBlocks(text)).toEqual([
			{ name: "a", entry: "a.js", fixtures: {} },
			{ name: "b", entry: "b.js", fixtures: {} },
		]);
	});

	it("returns null slots for blocks missing name or entry", () => {
		const text = `
[[projection]]
name = "a"

[[projection]]
name = "b"
entry = "b.js"
`;
		expect(projectionBlocks(text)).toEqual([
			null,
			{ name: "b", entry: "b.js", fixtures: {} },
		]);
	});

	it("extracts fixtures.<name> = path entries alongside the parent", () => {
		const text = `
[[projection]]
name = "a"
entry = "a.js"
fixtures.happy = "fixtures/happy.json"
fixtures.edge = "fixtures/edge.json"
`;
		expect(projectionBlocks(text)).toEqual([
			{
				name: "a",
				entry: "a.js",
				fixtures: {
					happy: "fixtures/happy.json",
					edge: "fixtures/edge.json",
				},
			},
		]);
	});

	it("returns [] for unparseable TOML", () => {
		expect(projectionBlocks("not = valid = toml")).toEqual([]);
	});

	it("returns [] when there are no [[projection]] blocks", () => {
		expect(projectionBlocks(`[foo]\nbar = 1\n`)).toEqual([]);
	});

	it("accepts the [projection.fixtures] table-block form", () => {
		// Third valid TOML way to express fixtures - a regular table
		// attached to the most recent [[projection]] AoT element.
		// Parses to the same shape as dotted keys / inline table.
		const text = `
[[projection]]
name = "a"
entry = "a.js"

[projection.fixtures]
happy = "fixtures/happy.json"
edge = "fixtures/edge.json"
`;
		expect(projectionBlocks(text)).toEqual([
			{
				name: "a",
				entry: "a.js",
				fixtures: {
					happy: "fixtures/happy.json",
					edge: "fixtures/edge.json",
				},
			},
		]);
	});

	it("accepts the inline-table form", () => {
		// fixtures = { happy = "...", edge = "..." } parses to the
		// same shape. Per-fixture line lenses won't fire for it (no
		// per-key source line) but the dropdown still works because
		// the parser produces the same map.
		const text = `
[[projection]]
name = "a"
entry = "a.js"
fixtures = { happy = "fixtures/happy.json", edge = "fixtures/edge.json" }
`;
		expect(projectionBlocks(text)).toEqual([
			{
				name: "a",
				entry: "a.js",
				fixtures: {
					happy: "fixtures/happy.json",
					edge: "fixtures/edge.json",
				},
			},
		]);
	});
});

describe("scanLines", () => {
	it("locates [[projection]] header lines", () => {
		const text = ["[[projection]]", 'name = "a"', "", "[[projection]]"].join(
			"\n",
		);
		const out = scanLines(text);
		expect(out.projectionHeaders.map((h) => h.line)).toEqual([0, 3]);
	});

	it("matches headers with leading whitespace and trailing comments", () => {
		const text = "  [[projection]] # main";
		const out = scanLines(text);
		expect(out.projectionHeaders).toHaveLength(1);
		expect(out.projectionHeaders[0]?.line).toBe(0);
	});

	it("locates fixtures.<name> lines and captures the name", () => {
		const text = [
			"[[projection]]",
			'fixtures.happy = "a.json"',
			'fixtures.edge = "b.json"',
		].join("\n");
		const out = scanLines(text);
		expect(
			out.fixtureLines.map((f) => ({ line: f.line, name: f.name })),
		).toEqual([
			{ line: 1, name: "happy" },
			{ line: 2, name: "edge" },
		]);
	});

	it("tolerates whitespace around the dot in dotted keys", () => {
		// TOML grammar permits whitespace around the dot. Without
		// allowing it, the scanner would miss legal source.
		const text = '  fixtures . happy   = "a.json"';
		const out = scanLines(text);
		expect(out.fixtureLines).toHaveLength(1);
		expect(out.fixtureLines[0]?.name).toBe("happy");
	});

	it("does not match an inline-table form (no per-key line)", () => {
		// fixtures = { happy = "a", edge = "b" } has no line per key;
		// the scanner correctly returns no fixture lines (callers fall
		// back to the projection-level dropdown via the parsed map).
		const text = '[[projection]]\nfixtures = { happy = "a.json" }';
		const out = scanLines(text);
		expect(out.fixtureLines).toEqual([]);
	});

	it("does not match prefixed names like `fixturesNot.x`", () => {
		const text = 'fixturesNot.happy = "a.json"';
		const out = scanLines(text);
		expect(out.fixtureLines).toEqual([]);
	});
});

describe("createProjectIndex", () => {
	let tmpRoot: string;

	beforeEach(() => {
		tmpRoot = fs.mkdtempSync(path.join(os.tmpdir(), "gaffer-pi-"));
	});

	afterEach(() => {
		fs.rmSync(tmpRoot, { recursive: true, force: true });
	});

	function writeToml(rel: string, body: string): string {
		const full = path.join(tmpRoot, rel);
		fs.mkdirSync(path.dirname(full), { recursive: true });
		fs.writeFileSync(full, body);
		return full;
	}

	it("indexes all entries from a single gaffer.toml", async () => {
		const tomlPath = writeToml(
			"app/gaffer.toml",
			`
[[projection]]
name = "a"
entry = "a.js"

[[projection]]
name = "b"
entry = "b.js"
`,
		);
		queueFindFiles([vscode.Uri.file(tomlPath)]);

		const index = await createProjectIndex();
		expect(index.size).toBe(2);
		expect(index.projections.map((p) => p.name).sort()).toEqual(["a", "b"]);
		expect(index.projectRoot).toBe(path.join(tmpRoot, "app"));
		const aEntry = path.join(tmpRoot, "app/a.js");
		expect(index.lookup(aEntry)).toEqual({
			name: "a",
			tomlDir: path.join(tmpRoot, "app"),
			fixtures: [],
			invalidFixtures: [],
		});
	});

	it("attaches fixtures + flags path-escape as invalid", async () => {
		// Duplicate names are impossible in the new dotted-key form -
		// smol-toml rejects duplicate keys at parse time. Empty path
		// and path escape remain validated here.
		const tomlPath = writeToml(
			"app/gaffer.toml",
			`
[[projection]]
name = "a"
entry = "a.js"
fixtures.happy = "fixtures/happy.json"
fixtures.evil = "../escape.json"
fixtures.empty = ""
`,
		);
		queueFindFiles([vscode.Uri.file(tomlPath)]);

		const index = await createProjectIndex();
		const proj = index.projections[0];
		expect(proj?.fixtures).toEqual([
			{ name: "happy", path: "fixtures/happy.json" },
		]);
		// Sorted alphabetically: empty, evil, happy.
		expect(proj?.invalidFixtures).toEqual([
			{ name: "empty", reason: "empty path" },
			{
				name: "evil",
				path: "../escape.json",
				reason: "path escapes project root",
			},
		]);
	});

	it("rejects entry paths that escape the toml directory via ..", async () => {
		const tomlPath = writeToml(
			"app/gaffer.toml",
			`
[[projection]]
name = "evil"
entry = "../etc/passwd"

[[projection]]
name = "ok"
entry = "ok.js"
`,
		);
		queueFindFiles([vscode.Uri.file(tomlPath)]);

		const index = await createProjectIndex();
		expect(index.size).toBe(1);
		expect(index.projections.map((p) => p.name)).toEqual(["ok"]);
	});

	it("rejects projections with an empty entry", async () => {
		const tomlPath = writeToml(
			"app/gaffer.toml",
			`
[[projection]]
name = "empty"
entry = ""

[[projection]]
name = "ok"
entry = "ok.js"
`,
		);
		queueFindFiles([vscode.Uri.file(tomlPath)]);

		const index = await createProjectIndex();
		expect(index.size).toBe(1);
		expect(index.projections.map((p) => p.name)).toEqual(["ok"]);
	});

	it("returns an empty index when no toml files are found", async () => {
		queueFindFiles([]);
		const index = await createProjectIndex();
		expect(index.size).toBe(0);
		expect(index.projections).toEqual([]);
		expect(index.entryPaths).toEqual([]);
		expect(index.projectRoot).toBeUndefined();
	});

	it("allows the same fixture name across different projections", async () => {
		// Two projections each declaring `fixtures.happy = "..."` is
		// fine - fixture names are scoped per projection. Pin so a
		// future global-uniqueness rule doesn't silently regress.
		const tomlPath = writeToml(
			"app/gaffer.toml",
			`
[[projection]]
name = "a"
entry = "a.js"
fixtures.happy = "fixtures/a-happy.json"

[[projection]]
name = "b"
entry = "b.js"
fixtures.happy = "fixtures/b-happy.json"
`,
		);
		queueFindFiles([vscode.Uri.file(tomlPath)]);

		const index = await createProjectIndex();
		expect(index.size).toBe(2);
		const a = index.projections.find((p) => p.name === "a");
		const b = index.projections.find((p) => p.name === "b");
		expect(a?.fixtures).toEqual([
			{ name: "happy", path: "fixtures/a-happy.json" },
		]);
		expect(b?.fixtures).toEqual([
			{ name: "happy", path: "fixtures/b-happy.json" },
		]);
	});

	it("returns null on lookup for an unknown path", async () => {
		queueFindFiles([]);
		const index = await createProjectIndex();
		expect(index.lookup("/does/not/exist.js")).toBeNull();
	});

	it("indexes entries across multiple toml files", async () => {
		const t1 = writeToml(
			"a/gaffer.toml",
			`[[projection]]\nname = "a"\nentry = "a.js"\n`,
		);
		const t2 = writeToml(
			"b/gaffer.toml",
			`[[projection]]\nname = "b"\nentry = "b.js"\n`,
		);
		queueFindFiles([vscode.Uri.file(t1), vscode.Uri.file(t2)]);

		const index = await createProjectIndex();
		expect(index.size).toBe(2);
		expect(index.projections.map((p) => p.name).sort()).toEqual(["a", "b"]);
	});
});
