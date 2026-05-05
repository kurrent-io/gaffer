import * as vscode from "vscode";
import path from "node:path";
import fs from "node:fs";
import { parse as parseToml } from "smol-toml";
import { log } from "../output.js";
import type {
	InvalidProjectFixture,
	ProjectEntry,
	ProjectFixture,
} from "../types.js";

export interface ProjectIndex {
	lookup(filePath: string): ProjectEntry | null;
	readonly size: number;
	readonly entryPaths: ReadonlyArray<string>;
	readonly projections: ReadonlyArray<{
		name: string;
		tomlUri: vscode.Uri;
		fixtures: ReadonlyArray<ProjectFixture>;
		invalidFixtures: ReadonlyArray<InvalidProjectFixture>;
	}>;
	readonly projectRoot: string | undefined;
}

export async function createProjectIndex(): Promise<ProjectIndex> {
	const entries = new Map<string, ProjectEntry>();
	const uris = await vscode.workspace.findFiles(
		"**/gaffer.toml",
		"**/node_modules/**",
	);
	for (const uri of uris) {
		const tomlDir = path.dirname(uri.fsPath);
		for (const proj of parseProjections(uri.fsPath)) {
			if (proj.entry === "") {
				log(`Rejecting projection ${proj.name}: empty entry path`);
				continue;
			}
			const absEntry = path.resolve(tomlDir, proj.entry);
			// Reject entry paths that escape the toml's directory. A
			// hostile or malformed gaffer.toml could otherwise point the
			// extension at arbitrary files outside the workspace via "..".
			if (!isWithin(absEntry, tomlDir)) {
				log(
					`Rejecting projection ${proj.name}: entry path "${proj.entry}" escapes ${tomlDir}`,
				);
				continue;
			}
			const { fixtures, invalidFixtures } = partitionFixtures(
				classifyFixtures(proj.name, proj.fixtures, tomlDir),
			);
			entries.set(normalizePath(absEntry), {
				name: proj.name,
				tomlDir,
				fixtures,
				invalidFixtures,
			});
		}
	}

	return {
		lookup: (filePath) => entries.get(normalizePath(filePath)) ?? null,
		get size() {
			return entries.size;
		},
		get entryPaths() {
			return [...entries.keys()];
		},
		get projections() {
			return [...entries.values()].map((entry) => ({
				name: entry.name,
				tomlUri: vscode.Uri.file(path.join(entry.tomlDir, "gaffer.toml")),
				fixtures: entry.fixtures,
				invalidFixtures: entry.invalidFixtures,
			}));
		},
		get projectRoot() {
			for (const entry of entries.values()) return entry.tomlDir;
			return undefined;
		},
	};
}

// NTFS is case-insensitive and VS Code can return mixed-case fsPaths;
// canonicalise on Windows so set and get hit the same key. Exported
// for test coverage; production callers don't need to import it.
export function normalizePath(p: string): string {
	const normalized = path.normalize(p);
	return process.platform === "win32" ? normalized.toLowerCase() : normalized;
}

// True iff `child` is `parent` itself or a descendant. Compares
// normalized paths; matches Windows case-insensitivity. Exported for
// test coverage.
export function isWithin(child: string, parent: string): boolean {
	const c = normalizePath(child);
	const p = normalizePath(parent);
	if (c === p) return true;
	const parentWithSep = p.endsWith(path.sep) ? p : p + path.sep;
	return c.startsWith(parentWithSep);
}

interface ParsedProjection {
	name: string;
	entry: string;
	// Loose name -> raw-value map straight off the TOML. Values are
	// `unknown` because a malformed toml could declare a non-string
	// (`fixtures.x = 42`) and we want to surface that as a warning
	// lens rather than silently dropping the entry. Type validation
	// happens in classifyFixtures.
	fixtures: Record<string, unknown>;
}

function parseProjections(tomlPath: string): ParsedProjection[] {
	let text: string;
	try {
		text = fs.readFileSync(tomlPath, "utf8");
	} catch {
		return [];
	}
	return projectionBlocks(text).filter(
		(p): p is ParsedProjection => p !== null,
	);
}

// Returns one slot per [[projection]] block in source order. Slots are null
// when the block is malformed (missing name or entry). Preserving order with
// nulls lets callers (e.g. the lens provider) zip against header line
// positions without index drift.
export function projectionBlocks(text: string): Array<ParsedProjection | null> {
	let parsed: unknown;
	try {
		parsed = parseToml(text);
	} catch {
		return [];
	}
	if (typeof parsed !== "object" || parsed === null) return [];
	const projections = (parsed as Record<string, unknown>)["projection"];
	if (!Array.isArray(projections)) return [];
	return projections.map((p) => {
		if (typeof p !== "object" || p === null) return null;
		const obj = p as Record<string, unknown>;
		const name = obj["name"];
		const entry = obj["entry"];
		if (typeof name !== "string" || typeof entry !== "string") return null;
		return { name, entry, fixtures: parseFixtures(obj["fixtures"]) };
	});
}

function parseFixtures(raw: unknown): Record<string, unknown> {
	if (typeof raw !== "object" || raw === null || Array.isArray(raw)) return {};
	return { ...(raw as Record<string, unknown>) };
}

// FixtureStatus is one entry per fixture key, classified as valid or
// invalid. classifyFixtures sorts entries alphabetically by name so
// the TOML provider's dropdown matches the CLI's FixtureNames() order;
// per-fixture line lenses anchor by name lookup, not by order.
export type FixtureStatus =
	| ({ kind: "valid" } & ProjectFixture)
	| ({ kind: "invalid" } & InvalidProjectFixture);

// classifyFixtures applies the editor-side validation. Mirrors the
// CLI's strict rules (empty path, path-escape) but without erroring
// on the first failure - the editor should keep working with one
// bad fixture in a projection that otherwise has good ones.
// Duplicate names are impossible by TOML semantics (parser rejects
// them at load time). Output is sorted alphabetically by name to
// match the CLI's FixtureNames() ordering for completion + JSON
// output.
export function classifyFixtures(
	projection: string,
	parsed: Record<string, unknown>,
	tomlDir: string,
): FixtureStatus[] {
	const out: FixtureStatus[] = [];
	const sortedEntries = Object.entries(parsed).sort(([a], [b]) =>
		a.localeCompare(b),
	);

	for (const [name, fixturePath] of sortedEntries) {
		if (typeof fixturePath !== "string") {
			out.push({ kind: "invalid", name, reason: "path must be a string" });
			log(
				`Rejecting fixture ${projection}/${name}: path is not a string (got ${typeof fixturePath})`,
			);
			continue;
		}
		if (fixturePath === "") {
			out.push({ kind: "invalid", name, reason: "empty path" });
			continue;
		}
		const absPath = path.resolve(tomlDir, fixturePath);
		if (!isWithin(absPath, tomlDir)) {
			out.push({
				kind: "invalid",
				name,
				path: fixturePath,
				reason: "path escapes project root",
			});
			log(
				`Rejecting fixture ${projection}/${name}: path "${fixturePath}" escapes ${tomlDir}`,
			);
			continue;
		}
		out.push({ kind: "valid", name, path: fixturePath });
	}
	return out;
}

interface ProjectionHeaderLine {
	line: number;
	length: number;
}

interface FixtureKeyLine {
	line: number;
	length: number;
	name: string;
}

export interface ScannedLines {
	projectionHeaders: ProjectionHeaderLine[];
	fixtureLines: FixtureKeyLine[];
}

// TOML bare keys allow [A-Za-z0-9_-]; we accept the same set for
// fixture names so the scanner matches whatever the parser will
// accept. The projection header may include leading whitespace,
// internal spaces, and a trailing line comment.
const projectionHeaderPattern = /^\s*\[\[\s*projection\s*\]\]\s*(?:#.*)?$/;
const fixtureKeyPattern = /^\s*fixtures\s*\.\s*([A-Za-z0-9_-]+)\s*=/;

// Find every [[projection]] header line and every `fixtures.<name> = ...`
// line in source order. smol-toml returns values but not positions;
// this lightweight scan zips back to lines by appearance order so the
// lens provider can place ranges on real source lines.
export function scanLines(text: string): ScannedLines {
	const out: ScannedLines = { projectionHeaders: [], fixtureLines: [] };
	const lines = text.split(/\r?\n/);
	for (const [i, line] of lines.entries()) {
		if (projectionHeaderPattern.test(line)) {
			out.projectionHeaders.push({ line: i, length: line.length });
			continue;
		}
		const m = fixtureKeyPattern.exec(line);
		if (m && m[1]) {
			out.fixtureLines.push({ line: i, length: line.length, name: m[1] });
		}
	}
	return out;
}

function partitionFixtures(statuses: FixtureStatus[]): {
	fixtures: ProjectFixture[];
	invalidFixtures: InvalidProjectFixture[];
} {
	const fixtures: ProjectFixture[] = [];
	const invalidFixtures: InvalidProjectFixture[] = [];
	for (const s of statuses) {
		if (s.kind === "valid") {
			fixtures.push({ name: s.name, path: s.path });
			continue;
		}
		const inv: InvalidProjectFixture = { reason: s.reason };
		if (s.name !== undefined) inv.name = s.name;
		if (s.path !== undefined) inv.path = s.path;
		invalidFixtures.push(inv);
	}
	return { fixtures, invalidFixtures };
}
