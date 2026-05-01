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
			{ name: "a", entry: "a.js" },
			{ name: "b", entry: "b.js" },
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
			{ name: "b", entry: "b.js" },
		]);
	});

	it("returns [] for unparseable TOML", () => {
		expect(projectionBlocks("not = valid = toml")).toEqual([]);
	});

	it("returns [] when there are no [[projection]] blocks", () => {
		expect(projectionBlocks(`[foo]\nbar = 1\n`)).toEqual([]);
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
		});
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
