import * as vscode from "vscode";
import path from "node:path";
import fs from "node:fs";
import { parse as parseToml } from "smol-toml";
import type { ProjectEntry } from "../types.js";

export interface ProjectIndex {
	lookup(filePath: string): ProjectEntry | null;
	readonly size: number;
	readonly entryPaths: ReadonlyArray<string>;
	readonly projections: ReadonlyArray<{
		name: string;
		tomlUri: vscode.Uri;
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
			const absEntry = path.resolve(tomlDir, proj.entry);
			entries.set(normalizePath(absEntry), {
				name: proj.name,
				tomlDir,
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
			const out: Array<{ name: string; tomlUri: vscode.Uri }> = [];
			for (const entry of entries.values()) {
				out.push({
					name: entry.name,
					tomlUri: vscode.Uri.file(path.join(entry.tomlDir, "gaffer.toml")),
				});
			}
			return out;
		},
		get projectRoot() {
			for (const entry of entries.values()) return entry.tomlDir;
			return undefined;
		},
	};
}

// NTFS is case-insensitive and VS Code can return mixed-case fsPaths;
// canonicalise on Windows so set and get hit the same key.
function normalizePath(p: string): string {
	const normalized = path.normalize(p);
	return process.platform === "win32" ? normalized.toLowerCase() : normalized;
}

interface ParsedProjection {
	name: string;
	entry: string;
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
		return { name, entry };
	});
}
