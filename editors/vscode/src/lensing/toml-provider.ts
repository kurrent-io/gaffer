import * as vscode from "vscode";
import { projectionBlocks } from "../discovery/project-index.js";
import { log } from "../output.js";
import { buildLens } from "./lens.js";
import type { Manifest } from "../discovery/schemas.js";
import type { DebugState } from "../types.js";

export class TomlCodeLensProvider implements vscode.CodeLensProvider {
	readonly #onDidChange = new vscode.EventEmitter<void>();
	readonly onDidChangeCodeLenses = this.#onDidChange.event;
	#debugState: DebugState = { name: null, status: "idle" };
	#manifest: Manifest | null;

	constructor(initialManifest: Manifest | null) {
		this.#manifest = initialManifest;
	}

	setManifest(manifest: Manifest | null): void {
		this.#manifest = manifest;
		this.#onDidChange.fire();
	}

	// Lens-side copy of the controller's debug state. Pushed via the
	// SessionController's pushDebugState callback rather than a shared
	// reference so the controller's mutations don't reach in here.
	setDebugState(state: Readonly<DebugState>): void {
		this.#debugState = { ...state };
		this.#onDidChange.fire();
	}

	refresh(): void {
		this.#onDidChange.fire();
	}

	provideCodeLenses(document: vscode.TextDocument): vscode.CodeLens[] {
		const text = document.getText();
		const blocks = projectionBlocks(text);
		if (blocks.length === 0) return [];

		const headers = findProjectionHeaderLines(text);
		const tomlUri = document.uri;

		const lenses: vscode.CodeLens[] = [];
		// blocks and headers are aligned by appearance order. If they don't
		// match in length the source is malformed (or the header scan missed
		// a variant); skip rather than mis-attribute lenses.
		if (blocks.length !== headers.length) {
			log(
				`Lens skipped for ${document.uri.fsPath}: ${blocks.length} blocks vs ${headers.length} headers (malformed toml or unhandled syntax)`,
			);
			return [];
		}

		for (const [i, block] of blocks.entries()) {
			if (!block) continue;
			const header = headers[i];
			if (!header) continue;
			const range = new vscode.Range(
				header.line,
				0,
				header.line,
				header.length,
			);
			const lens = buildLens(
				this.#manifest,
				this.#debugState,
				block.name,
				range,
				tomlUri,
			);
			if (lens) lenses.push(lens);
		}
		return lenses;
	}
}

interface HeaderLine {
	line: number;
	length: number;
}

// TOML array-of-table header for [[projection]]. Allows leading whitespace,
// optional spaces inside the brackets, and a trailing line comment - all
// valid per the TOML spec. We'd rather over-match here than under-match
// since the parser is the source of truth.
const projectionHeaderPattern = /^\s*\[\[\s*projection\s*\]\]\s*(?:#.*)?$/;

// Find each [[projection]] header line so we can position lenses on it.
// smol-toml returns values but not source positions; this lightweight scan
// zips back to lines by appearance order.
function findProjectionHeaderLines(text: string): HeaderLine[] {
	const headers: HeaderLine[] = [];
	const lines = text.split(/\r?\n/);
	for (const [i, line] of lines.entries()) {
		if (projectionHeaderPattern.test(line)) {
			headers.push({ line: i, length: line.length });
		}
	}
	return headers;
}
