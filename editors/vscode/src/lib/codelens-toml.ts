import * as vscode from "vscode";
import { projectionBlocks } from "./project.js";
import type { GafferCli } from "./cli.js";
import type { DebugState } from "../types.js";

export class TomlCodeLensProvider implements vscode.CodeLensProvider {
	private readonly _onDidChange = new vscode.EventEmitter<void>();
	readonly onDidChangeCodeLenses = this._onDidChange.event;

	constructor(
		private readonly _cli: GafferCli,
		private readonly _debugState: DebugState,
	) {}

	refresh(): void {
		this._onDidChange.fire();
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
		if (blocks.length !== headers.length) return [];

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
				this._cli,
				this._debugState,
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
	const lines = text.split("\n");
	for (const [i, line] of lines.entries()) {
		if (projectionHeaderPattern.test(line)) {
			headers.push({ line: i, length: line.length });
		}
	}
	return headers;
}

export function buildLens(
	cli: GafferCli,
	debugState: DebugState,
	name: string,
	range: vscode.Range,
	tomlUri: vscode.Uri,
): vscode.CodeLens | null {
	if (debugState.name === name) {
		const labels: Record<DebugState["status"], string> = {
			idle: "idle",
			starting: "$(sync~spin) Starting",
			debugging: "$(debug-stop) Debugging",
		};
		const label = labels[debugState.status];
		if (debugState.status === "debugging") {
			return new vscode.CodeLens(range, {
				title: label,
				command: "gaffer.stopDebug",
			});
		}
		// Informational lens (no command). VS Code accepts a command-less lens at runtime;
		// the cast satisfies @types/vscode which marks `command` as required.
		return new vscode.CodeLens(range, { title: label } as vscode.Command);
	}

	if (!vscode.workspace.isTrusted) {
		return new vscode.CodeLens(range, {
			title: "$(workspace-untrusted) Trust workspace to debug",
			command: "workbench.trust.manage",
		});
	}

	if (!cli.hasCommand("dev") || !cli.hasFlag("dev", "debug")) return null;

	return new vscode.CodeLens(range, {
		title: "$(debug-start) Debug",
		command: "gaffer.debugProjection",
		arguments: [{ name, tomlUri }],
	});
}
