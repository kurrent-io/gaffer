import * as vscode from "vscode";
import path from "node:path";
import fs from "node:fs";
import type { ProjectEntry } from "../types.js";

export class ProjectIndex {
	private readonly _entries = new Map<string, ProjectEntry>();

	async refresh(): Promise<void> {
		this._entries.clear();
		const uris = await vscode.workspace.findFiles(
			"**/gaffer.toml",
			"**/node_modules/**",
		);
		for (const uri of uris) {
			const tomlDir = path.dirname(uri.fsPath);
			for (const proj of parseProjections(uri.fsPath)) {
				const absEntry = path.resolve(tomlDir, proj.entry);
				this._entries.set(absEntry, { name: proj.name, tomlDir });
			}
		}
	}

	lookup(filePath: string): ProjectEntry | null {
		return this._entries.get(filePath) ?? null;
	}

	get entryPaths(): string[] {
		return [...this._entries.keys()];
	}

	get projectRoot(): string | undefined {
		for (const entry of this._entries.values()) {
			return entry.tomlDir;
		}
		return undefined;
	}
}

interface ParsedProjection {
	name: string;
	entry: string;
}

function parseProjections(tomlPath: string): ParsedProjection[] {
	const projections: Partial<ParsedProjection>[] = [];
	try {
		const text = fs.readFileSync(tomlPath, "utf8");
		const lines = text.split("\n");

		let current: Partial<ParsedProjection> | null = null;
		for (const line of lines) {
			const trimmed = line.trim();
			if (trimmed === "[[projection]]") {
				if (current) projections.push(current);
				current = {};
				continue;
			}
			if (!current) continue;
			if (trimmed.startsWith("[")) {
				projections.push(current);
				current = null;
				continue;
			}

			const name = matchQuoted(trimmed, "name");
			if (name !== undefined) current.name = name;

			const entry = matchQuoted(trimmed, "entry");
			if (entry !== undefined) current.entry = entry;
		}
		if (current) projections.push(current);
	} catch {
		// ignore read errors
	}
	return projections.filter((p): p is ParsedProjection =>
		Boolean(p.name && p.entry),
	);
}

// Matches a TOML key with a double- or single-quoted string value:
//   key = "value"  or  key = 'value'
// Returns the captured value, or undefined if the line doesn't match.
// Caller must pass a regex-safe `key` (no metacharacters); current callers
// pass literals only.
function matchQuoted(line: string, key: string): string | undefined {
	const re = new RegExp(`^${key}\\s*=\\s*(?:"([^"]+)"|'([^']+)')`);
	const m = line.match(re);
	if (!m) return undefined;
	return m[1] ?? m[2];
}
