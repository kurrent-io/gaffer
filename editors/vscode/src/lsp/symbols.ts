import * as vscode from "vscode";
import { getLanguageClient } from "./client.js";

// Minimal LSP wire shape - mirrors what the gaffer LSP server
// emits in cli/internal/lsp/protocol.go (SymbolInformation, the
// legacy form, not the LSP 3.17 WorkspaceSymbol).
interface LspSymbolInformation {
	name: string;
	kind: number;
	location: {
		uri: string;
		range: {
			start: { line: number; character: number };
			end: { line: number; character: number };
		};
	};
	containerName?: string;
}

export interface ProjectionSymbol {
	name: string;
	tomlUri: vscode.Uri;
}

/**
 * Fetch every projection across the workspace via the LSP
 * server's `workspace/symbol` endpoint. Replaces the in-process
 * `createProjectIndex().projections` walk - the server is now
 * the single source of truth for which projections exist.
 *
 * Returns an empty list if the LSP client isn't ready, the
 * request fails, or the server returns no symbols.
 */
export async function fetchProjections(): Promise<ProjectionSymbol[]> {
	const client = getLanguageClient();
	if (!client) return [];
	let symbols: LspSymbolInformation[] | null;
	try {
		symbols = await client.sendRequest<LspSymbolInformation[] | null>(
			"workspace/symbol",
			{ query: "" },
		);
	} catch {
		return [];
	}
	if (!symbols) return [];
	return symbols.map((s) => ({
		name: s.name,
		tomlUri: vscode.Uri.parse(s.location.uri),
	}));
}
