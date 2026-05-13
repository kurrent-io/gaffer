import * as vscode from "vscode";
import { buildGafferArgv } from "../discovery/cli.js";

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
	readonly #getInvokerId: () => string | null;

	constructor(getInvokerId: () => string | null) {
		this.#getInvokerId = getInvokerId;
	}

	dispose(): void {
		this.#onDidChange.dispose();
	}

	refresh(): void {
		this.#onDidChange.fire();
	}

	provideMcpServerDefinitions(
		_token: vscode.CancellationToken,
	): vscode.McpStdioServerDefinition[] {
		// VS Code calls this on every picker open and on each
		// onDidChange fire, so the untrusted/empty paths intentionally
		// don't log - the channel doesn't need a line per query and
		// trust state is already user-visible elsewhere.
		if (!vscode.workspace.isTrusted) return [];
		const folders = vscode.workspace.workspaceFolders ?? [];
		if (folders.length === 0) return [];

		// argv[0] is unreachable-undefined (buildGafferArgv falls back to
		// ["gaffer"] when User scope is empty), but noUncheckedIndexedAccess
		// requires the narrow.
		const argv = buildGafferArgv(["mcp"], {
			invokerId: this.#getInvokerId(),
			invokedVia: "mcp_provider",
		});
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
