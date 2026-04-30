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

			const nameMatch = trimmed.match(/^name\s*=\s*(?:"([^"]+)"|'([^']+)')/);
			if (nameMatch) current.name = nameMatch[1] ?? nameMatch[2];

			const entryMatch = trimmed.match(/^entry\s*=\s*(?:"([^"]+)"|'([^']+)')/);
			if (entryMatch) current.entry = entryMatch[1] ?? entryMatch[2];
		}
		if (current) projections.push(current);
	} catch {
		// ignore read errors
	}
	return projections.filter(
		(p): p is ParsedProjection => Boolean(p.name && p.entry),
	);
}
