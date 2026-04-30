import * as vscode from "vscode";
import path from "node:path";
import { buildLens } from "./codelens-toml.js";
import type { GafferCli } from "./cli.js";
import type { ProjectIndex } from "./project.js";
import type { DebugState } from "../types.js";

const fromPattern = /^(fromAll|fromStream|fromCategory|fromStreams)\s*\(/;

export class JsCodeLensProvider implements vscode.CodeLensProvider {
	private readonly _onDidChange = new vscode.EventEmitter<void>();
	readonly onDidChangeCodeLenses = this._onDidChange.event;

	constructor(
		private readonly _cli: GafferCli,
		private readonly _projectIndex: ProjectIndex,
		private readonly _debugState: DebugState,
	) {}

	refresh(): void {
		this._onDidChange.fire();
	}

	provideCodeLenses(document: vscode.TextDocument): vscode.CodeLens[] {
		const resolved = this._projectIndex.lookup(document.uri.fsPath);
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
		const lens = buildLens(this._cli, this._debugState, name, range, tomlUri);
		return lens ? [lens] : [];
	}
}
