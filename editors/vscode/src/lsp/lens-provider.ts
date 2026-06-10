import * as vscode from "vscode";
import type { LanguageClient } from "vscode-languageclient/node";
import * as v from "valibot";
import { hasCommand, hasFlag } from "../discovery/cli.js";
import type { Manifest } from "../discovery/schemas.js";
import type { DebugState } from "../types.js";
import { log } from "../output.js";

// A projection's debug state from a lens's perspective. "stop"
// means the projection-level lens should swap to a Stop button
// (with the embedded title for the current substate); the
// dropdown should hide. "off" means no active session for this
// projection - render the regular Debug affordance.
type LensState = { kind: "stop"; title: string } | { kind: "off" };

function lensState(debugState: Readonly<DebugState>, name: string): LensState {
	if (debugState.name !== name) return { kind: "off" };
	switch (debugState.status) {
		case "starting":
			return { kind: "stop", title: "$(sync~spin) Starting (cancel)" };
		case "running":
		case "inspecting":
			return { kind: "stop", title: "$(debug-stop) Debugging" };
		case "idle":
		case "ended":
			return { kind: "off" };
	}
}

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

// Args[0] payloads as the LSP server emits them, validated at
// the wire boundary. Schema mismatch (server-side typo, version
// skew) gets rejected loudly here instead of letting an undefined
// `name` propagate into a no-op debug command. Same posture as
// the manifest fetch.
const ProjectionArgsSchema = v.object({
	name: v.string(),
	configURI: v.string(),
	fixture: v.optional(v.string()),
	// Set on the projection-level Debug lens: the env to run live
	// against (the resolved default/sole env). Absent on per-fixture
	// lenses, which run a fixture rather than connecting.
	env: v.optional(v.string()),
});

// A configured [env.<name>] as the server reports it: name plus whether
// it's the default. `default` is omitted on the wire when false.
const EnvSchema = v.object({
	name: v.string(),
	default: v.optional(v.boolean(), false),
});

const ProjectionPickArgsSchema = v.object({
	name: v.string(),
	configURI: v.string(),
	fixtureNames: v.array(v.string()),
	envs: v.optional(v.array(EnvSchema), []),
});

// parseConfigURI guards `vscode.Uri.parse` so a malformed URI
// from the server doesn't throw out of provideCodeLenses and
// drop every remaining lens for the document.
function parseConfigURI(uri: string): vscode.Uri | null {
	try {
		return vscode.Uri.parse(uri, true);
	} catch (err) {
		log(
			`Lens: rejecting malformed configURI ${JSON.stringify(uri)}: ${
				err instanceof Error ? err.message : String(err)
			}`,
		);
		return null;
	}
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
export class LspCodeLensProvider
	implements vscode.CodeLensProvider, vscode.Disposable
{
	readonly #onDidChange = new vscode.EventEmitter<void>();
	readonly onDidChangeCodeLenses = this.#onDidChange.event;

	#client: LanguageClient | undefined;
	#manifest: Manifest | null = null;
	#debugState: DebugState = { name: null, status: "idle" };

	dispose(): void {
		this.#onDidChange.dispose();
	}

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
			try {
				const decorated = this.#decorate(sl);
				if (decorated) lenses.push(decorated);
			} catch (err) {
				// One bad lens shouldn't drop the whole document's
				// rendering. Log and continue.
				log(
					`Lens: dropping malformed lens: ${err instanceof Error ? err.message : String(err)}`,
				);
			}
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
		// command, but trust-gate it. Future intents we don't yet
		// know about still respect the workspace-trust contract -
		// no client-side dispatch on untrusted state.
		if (sl.command) {
			if (!vscode.workspace.isTrusted) return null;
			return new vscode.CodeLens(range, {
				title: sl.command.title,
				command: sl.command.command,
				...(sl.command.arguments ? { arguments: sl.command.arguments } : {}),
			});
		}
		return null;
	}

	#decorateDebug(sl: LspCodeLens, range: vscode.Range): vscode.CodeLens | null {
		const parsed = v.safeParse(
			ProjectionArgsSchema,
			sl.command?.arguments?.[0],
		);
		if (!parsed.success) {
			log(
				`Lens: rejecting debug args: ${parsed.issues.map((i) => i.message).join("; ")}`,
			);
			return null;
		}
		const args = parsed.output;
		const tomlUri = parseConfigURI(args.configURI);
		if (!tomlUri) return null;

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

		const cmdArgs: {
			name: string;
			tomlUri: vscode.Uri;
			fixture?: string;
			env?: string;
		} = {
			name: args.name,
			tomlUri,
		};
		if (args.fixture !== undefined) cmdArgs.fixture = args.fixture;
		if (args.env !== undefined) cmdArgs.env = args.env;
		return new vscode.CodeLens(range, {
			title: "$(debug-start) Debug",
			command: "gaffer.debugProjection",
			arguments: [cmdArgs],
		});
	}

	#decorateDebugChoose(
		sl: LspCodeLens,
		range: vscode.Range,
	): vscode.CodeLens | null {
		const parsed = v.safeParse(
			ProjectionPickArgsSchema,
			sl.command?.arguments?.[0],
		);
		if (!parsed.success) {
			log(
				`Lens: rejecting debug-choose args: ${parsed.issues.map((i) => i.message).join("; ")}`,
			);
			return null;
		}
		const args = parsed.output;

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

		const tomlUri = parseConfigURI(args.configURI);
		if (!tomlUri) return null;
		return new vscode.CodeLens(range, {
			title: "$(debug-start) Debug from...",
			command: "gaffer.debugProjectionPick",
			arguments: [
				{
					name: args.name,
					tomlUri,
					fixtureNames: args.fixtureNames,
					envs: args.envs,
				},
			],
		});
	}
}
