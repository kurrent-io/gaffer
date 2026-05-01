import * as vscode from "vscode";
import path from "node:path";
import { buildLens } from "./lens.js";
import type { Manifest } from "../discovery/schemas.js";
import type { ProjectIndex } from "../discovery/project-index.js";
import type { DebugState } from "../types.js";

const fromPattern = /^(fromAll|fromStream|fromCategory|fromStreams)\s*\(/;

export class JsCodeLensProvider implements vscode.CodeLensProvider {
	readonly #onDidChange = new vscode.EventEmitter<void>();
	readonly onDidChangeCodeLenses = this.#onDidChange.event;
	readonly #debugState: DebugState;
	#index: ProjectIndex;
	#manifest: Manifest | null;

	constructor(
		initialIndex: ProjectIndex,
		initialManifest: Manifest | null,
		debugState: DebugState,
	) {
		this.#index = initialIndex;
		this.#manifest = initialManifest;
		this.#debugState = debugState;
	}

	setIndex(index: ProjectIndex): void {
		this.#index = index;
		this.#onDidChange.fire();
	}

	setManifest(manifest: Manifest | null): void {
		this.#manifest = manifest;
		this.#onDidChange.fire();
	}

	refresh(): void {
		this.#onDidChange.fire();
	}

	provideCodeLenses(document: vscode.TextDocument): vscode.CodeLens[] {
		const resolved = this.#index.lookup(document.uri.fsPath);
		if (!resolved) return [];

		const lines = document.getText().split(/\r?\n/);
		let fromLine = -1;
		let fromLineLength = 0;
		for (const [i, line] of lines.entries()) {
			if (fromPattern.test(line.trim())) {
				fromLine = i;
				fromLineLength = line.length;
				break;
			}
		}
		if (fromLine === -1) return [];

		const { name, tomlDir } = resolved;
		const range = new vscode.Range(fromLine, 0, fromLine, fromLineLength);
		const tomlUri = vscode.Uri.file(path.join(tomlDir, "gaffer.toml"));
		const lens = buildLens(
			this.#manifest,
			this.#debugState,
			name,
			range,
			tomlUri,
		);
		return lens ? [lens] : [];
	}
}
