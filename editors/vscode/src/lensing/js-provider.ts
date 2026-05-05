import * as vscode from "vscode";
import path from "node:path";
import { buildLens, buildPickLens } from "./lens.js";
import type { Manifest } from "../discovery/schemas.js";
import type { ProjectIndex } from "../discovery/project-index.js";
import type { DebugState } from "../types.js";

const fromPattern = /^(fromAll|fromStream|fromCategory|fromStreams)\s*\(/;

export class JsCodeLensProvider implements vscode.CodeLensProvider {
	readonly #onDidChange = new vscode.EventEmitter<void>();
	readonly onDidChangeCodeLenses = this.#onDidChange.event;
	#debugState: DebugState = { name: null, status: "idle" };
	#index: ProjectIndex;
	#manifest: Manifest | null;

	constructor(initialIndex: ProjectIndex, initialManifest: Manifest | null) {
		this.#index = initialIndex;
		this.#manifest = initialManifest;
	}

	setIndex(index: ProjectIndex): void {
		this.#index = index;
		this.#onDidChange.fire();
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

		const { name, tomlDir, fixtures } = resolved;
		const range = new vscode.Range(fromLine, 0, fromLine, fromLineLength);
		const tomlUri = vscode.Uri.file(path.join(tomlDir, "gaffer.toml"));
		const lenses: vscode.CodeLens[] = [];
		const liveLens = buildLens(
			this.#manifest,
			this.#debugState,
			name,
			range,
			tomlUri,
		);
		if (liveLens) lenses.push(liveLens);
		// Second lens for fixture-driven debug. Only rendered when the
		// projection has at least one valid fixture - otherwise the
		// dropdown would be empty. Distinct command + title so the
		// existing "Debug" muscle memory still maps to live.
		if (fixtures.length > 0) {
			const fixtureLens = buildPickLens(
				this.#manifest,
				this.#debugState,
				name,
				range,
				tomlUri,
				fixtures.map((f) => f.name),
			);
			if (fixtureLens) lenses.push(fixtureLens);
		}
		return lenses;
	}
}
