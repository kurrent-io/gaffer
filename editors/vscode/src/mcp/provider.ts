import * as vscode from "vscode";
import { buildGafferArgv } from "../discovery/cli.js";
import { log } from "../output.js";

/**
 * Provides one MCP server definition per workspace folder, invoking
 * `gaffer mcp` via the User-scoped `gaffer.command` argv (so a
 * hostile workspace can't override the binary path - same posture as
 * the LSP spawn). Returns [] under workspace untrust; the caller
 * fires `refresh()` on grant so the picker tracks reality.
 */
export class GafferMcpProvider implements vscode.McpServerDefinitionProvider<vscode.McpStdioServerDefinition> {
	readonly #onDidChange = new vscode.EventEmitter<void>();
	readonly onDidChangeMcpServerDefinitions = this.#onDidChange.event;

	dispose(): void {
		this.#onDidChange.dispose();
	}

	refresh(): void {
		this.#onDidChange.fire();
	}

	provideMcpServerDefinitions(
		_token: vscode.CancellationToken,
	): vscode.McpStdioServerDefinition[] {
		if (!vscode.workspace.isTrusted) {
			log("mcp: workspace untrusted, withholding gaffer mcp server");
			return [];
		}
		const folders = vscode.workspace.workspaceFolders ?? [];
		if (folders.length === 0) return [];

		// argv[0] is unreachable-undefined (buildGafferArgv falls back to
		// ["gaffer"] when User scope is empty), but noUncheckedIndexedAccess
		// requires the narrow.
		const argv = buildGafferArgv(["mcp"]);
		const command = argv[0];
		if (command === undefined) return [];
		const args = argv.slice(1);

		const multi = folders.length > 1;
		return folders.map((folder) => {
			const label = multi ? `Gaffer (${folder.name})` : "Gaffer";
			const def = new vscode.McpStdioServerDefinition(label, command, args, {});
			def.cwd = folder.uri;
			return def;
		});
	}
}
