import * as vscode from "vscode";
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
		const lenses: vscode.CodeLens[] = [];
		const lines = document.getText().split("\n");
		const tomlUri = document.uri;

		for (const [i, line] of lines.entries()) {
			if (line.trim() !== "[[projection]]") continue;

			const name = extractName(lines, i + 1);
			if (!name) continue;

			const range = new vscode.Range(i, 0, i, line.length);
			const lens = buildLens(this._cli, this._debugState, name, range, tomlUri);
			if (lens) lenses.push(lens);
		}

		return lenses;
	}
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

function extractName(lines: string[], startLine: number): string | null {
	for (let i = startLine; i < lines.length && i < startLine + 10; i++) {
		const line = lines[i]?.trim();
		if (line === undefined) break;
		if (line.startsWith("[")) break;

		const match = line.match(/^name\s*=\s*(?:"([^"]+)"|'([^']+)')/);
		if (match) return match[1] ?? match[2] ?? null;
	}
	return null;
}
