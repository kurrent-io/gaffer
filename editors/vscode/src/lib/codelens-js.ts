import * as vscode from "vscode";
import path from "node:path";
import { buildLens } from "./codelens-toml.js";
import type { GafferCli } from "./cli.js";
import type { ProjectIndex } from "./project.js";
import type { DebugState } from "../types.js";

const fromPattern = /^(fromAll|fromStream|fromCategory|fromStreams)\s*\(/;

export class JsCodeLensProvider implements vscode.CodeLensProvider {
	readonly #onDidChange = new vscode.EventEmitter<void>();
	readonly onDidChangeCodeLenses = this.#onDidChange.event;
	readonly #cli: GafferCli;
	readonly #projectIndex: ProjectIndex;
	readonly #debugState: DebugState;

	constructor(
		cli: GafferCli,
		projectIndex: ProjectIndex,
		debugState: DebugState,
	) {
		this.#cli = cli;
		this.#projectIndex = projectIndex;
		this.#debugState = debugState;
	}

	refresh(): void {
		this.#onDidChange.fire();
	}

	provideCodeLenses(document: vscode.TextDocument): vscode.CodeLens[] {
		const resolved = this.#projectIndex.lookup(document.uri.fsPath);
		if (!resolved) return [];

		const lines = document.getText().split("\n");
		let fromLine = -1;
		let fromLineLength = 0;
		for (const [i, line] of lines.entries()) {
			if (i >= 20) break;
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
		const lens = buildLens(this.#cli, this.#debugState, name, range, tomlUri);
		return lens ? [lens] : [];
	}
}
