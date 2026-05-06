import * as vscode from "vscode";
import type { LanguageClient } from "vscode-languageclient/node";
import { hasCommand, hasFlag } from "../discovery/cli.js";
import type { Manifest } from "../discovery/schemas.js";
import type { DebugState } from "../types.js";
import { lensState } from "../lensing/lens.js";

// Server-side intent constants (must match cli/internal/lsp/protocol.go).
const IntentDebug = "debug";
const IntentDebugChoose = "debug-choose";

const codeLensMethod = "textDocument/codeLens";

// Minimal LSP wire-format types we receive from the server.
// Defined inline so we don't depend on `vscode-languageclient`
// (the cross-platform package) just for its type re-exports.
interface LspPosition {
	line: number;
	character: number;
}

interface LspRange {
	start: LspPosition;
	end: LspPosition;
}

interface LspCommand {
	title: string;
	command: string;
	arguments?: unknown[];
}

interface LspCodeLens {
	range: LspRange;
	command?: LspCommand;
	data?: { intent?: string };
}

interface CodeLensParams {
	textDocument: { uri: string };
}

// Args[0] payloads as the LSP server emits them. configURI is a
// file:// URI string; we convert to vscode.Uri before passing to
// the debug-launch command, which expects tomlUri as a Uri.
interface ProjectionArgs {
	name: string;
	configURI: string;
	fixture?: string;
}

interface ProjectionPickArgs {
	name: string;
	configURI: string;
	fixtureNames: string[];
}

/**
 * CodeLensProvider that fetches lenses from the gaffer LSP server
 * via `textDocument/codeLens` and applies client-side overlays:
 * workspace-trust gate, active-debug-session swap (Debug ->
 * Stop / Starting), and manifest gate (no `dev --debug` -> hide).
 *
 * Reactive refresh: callers update mutable state via setters,
 * each of which fires onDidChangeCodeLenses so VS Code re-calls
 * provideCodeLenses with the new context.
 */
export class LspCodeLensProvider implements vscode.CodeLensProvider {
	readonly #onDidChange = new vscode.EventEmitter<void>();
	readonly onDidChangeCodeLenses = this.#onDidChange.event;

	#client: LanguageClient | undefined;
	#manifest: Manifest | null = null;
	#debugState: DebugState = { name: null, status: "idle" };

	setClient(client: LanguageClient | undefined): void {
		this.#client = client;
		this.#onDidChange.fire();
	}

	setManifest(manifest: Manifest | null): void {
		this.#manifest = manifest;
		this.#onDidChange.fire();
	}

	setDebugState(state: Readonly<DebugState>): void {
		this.#debugState = { ...state };
		this.#onDidChange.fire();
	}

	refresh(): void {
		this.#onDidChange.fire();
	}

	async provideCodeLenses(
		document: vscode.TextDocument,
		token: vscode.CancellationToken,
	): Promise<vscode.CodeLens[]> {
		const client = this.#client;
		if (!client) return [];
		const params: CodeLensParams = {
			textDocument: { uri: document.uri.toString() },
		};
		let serverLenses: LspCodeLens[] | null;
		try {
			serverLenses = await client.sendRequest<LspCodeLens[] | null>(
				codeLensMethod,
				params,
				token,
			);
		} catch {
			// Cancellation or transient error - return empty rather than
			// surfacing a "no provider could compute lenses" toast.
			return [];
		}
		if (!serverLenses) return [];

		const lenses: vscode.CodeLens[] = [];
		for (const sl of serverLenses) {
			const decorated = this.#decorate(sl);
			if (decorated) lenses.push(decorated);
		}
		return lenses;
	}

	#decorate(sl: LspCodeLens): vscode.CodeLens | null {
		const range = new vscode.Range(
			sl.range.start.line,
			sl.range.start.character,
			sl.range.end.line,
			sl.range.end.character,
		);
		const intent = sl.data?.intent;
		if (intent === IntentDebug) {
			return this.#decorateDebug(sl, range);
		}
		if (intent === IntentDebugChoose) {
			return this.#decorateDebugChoose(sl, range);
		}
		// Unknown intent: pass through with the server's title and
		// command. Forward-compatible for future intents we don't yet
		// know about.
		if (sl.command) {
			return new vscode.CodeLens(range, {
				title: sl.command.title,
				command: sl.command.command,
				...(sl.command.arguments ? { arguments: sl.command.arguments } : {}),
			});
		}
		return null;
	}

	#decorateDebug(sl: LspCodeLens, range: vscode.Range): vscode.CodeLens | null {
		const args = (sl.command?.arguments?.[0] ?? null) as ProjectionArgs | null;
		if (!args) return null;
		const tomlUri = vscode.Uri.parse(args.configURI);

		// Active-session overlay only applies to projection-level
		// lenses. Per-fixture lenses (fixture set) stay clickable
		// mid-session so the user can switch fixtures without
		// stopping first.
		const state = lensState(this.#debugState, args.name);
		if (state.kind === "stop" && args.fixture === undefined) {
			return new vscode.CodeLens(range, {
				title: state.title,
				command: "gaffer.stopDebug",
			});
		}

		if (!vscode.workspace.isTrusted) {
			return new vscode.CodeLens(range, {
				title: "$(workspace-untrusted) Trust workspace to debug",
				command: "workbench.trust.manage",
			});
		}

		if (
			!hasCommand(this.#manifest, "dev") ||
			!hasFlag(this.#manifest, "dev", "debug")
		) {
			return null;
		}

		const cmdArgs: { name: string; tomlUri: vscode.Uri; fixture?: string } = {
			name: args.name,
			tomlUri,
		};
		if (args.fixture !== undefined) cmdArgs.fixture = args.fixture;
		const titleSuffix =
			args.fixture !== undefined ? (sl.command?.title ?? "Debug") : "Debug";
		return new vscode.CodeLens(range, {
			title: `$(debug-start) ${titleSuffix}`,
			command: "gaffer.debugProjection",
			arguments: [cmdArgs],
		});
	}

	#decorateDebugChoose(
		sl: LspCodeLens,
		range: vscode.Range,
	): vscode.CodeLens | null {
		const args = (sl.command?.arguments?.[0] ??
			null) as ProjectionPickArgs | null;
		if (!args) return null;

		// Hidden during an active session for this projection - the
		// user should stop (or use per-fixture lenses) before
		// launching another.
		if (lensState(this.#debugState, args.name).kind === "stop") return null;
		if (!vscode.workspace.isTrusted) return null;
		if (
			!hasCommand(this.#manifest, "dev") ||
			!hasFlag(this.#manifest, "dev", "debug")
		) {
			return null;
		}

		const tomlUri = vscode.Uri.parse(args.configURI);
		return new vscode.CodeLens(range, {
			title: `$(debug-start) ${sl.command?.title ?? "Debug from fixture..."}`,
			command: "gaffer.debugProjectionPick",
			arguments: [
				{ name: args.name, tomlUri, fixtureNames: args.fixtureNames },
			],
		});
	}
}
