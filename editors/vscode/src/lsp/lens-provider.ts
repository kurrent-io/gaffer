import * as vscode from "vscode";
import type { LanguageClient } from "vscode-languageclient/node";
import * as v from "valibot";
import { hasCommand, hasFlag } from "../discovery/cli.js";
import type { Manifest } from "../discovery/schemas.js";
import type { DebugState } from "../types.js";
import { log } from "../output.js";
import type { BadgeCell } from "./status-badges.js";

// Sink for the per-projection badge health the server delivers alongside the
// clickable lenses. Called once per provideCodeLenses with every badge cell
// for the document (empty to clear), so the extension can paint decorations.
export type BadgeSink = (uri: vscode.Uri, cells: BadgeCell[]) => void;

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
const IntentStatusEnv = "status-env";
const IntentStatusLoading = "status-loading";
const IntentSignIn = "sign-in";
const IntentStatusBadges = "status-badges";
const IntentActions = "actions";

// Server-reported per-environment health, validated at the wire boundary like
// the lens arg payloads.
const BadgeHealthsSchema = v.array(
	v.picklist(["green", "orange", "red", "locked", "error", "loading"]),
);

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
	tooltip?: string;
	arguments?: unknown[];
}

interface LspCodeLens {
	range: LspRange;
	command?: LspCommand;
	data?: { intent?: string; healths?: string[] };
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

// Args[0] for an env-block sign-in lens: the env that needs auth and the
// declaring gaffer.toml.
const SignInArgsSchema = v.object({
	env: v.string(),
	configURI: v.string(),
});

// Args[0] for the per-projection "actions.." lens: the projection, its
// declaring gaffer.toml, and the configured environments the action menu is
// grouped by.
const ProjectionActionsArgsSchema = v.object({
	name: v.string(),
	configURI: v.string(),
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
	readonly #onBadges: BadgeSink | undefined;

	// onBadges receives the per-projection badge health carried alongside the
	// lenses; omitted by clients (and tests) that don't paint the badge.
	constructor(onBadges?: BadgeSink) {
		this.#onBadges = onBadges;
	}

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
		// Server gone (not yet spawned, stopped, or mid-restart): clear any
		// painted badge dots so stale health doesn't linger with no data
		// behind it. The catch path below deliberately doesn't clear - a
		// cancelled request is transient and clearing would flicker.
		if (!client) {
			this.#onBadges?.(document.uri, []);
			return [];
		}
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
		if (!serverLenses) {
			this.#onBadges?.(document.uri, []);
			return [];
		}

		const lenses: vscode.CodeLens[] = [];
		const badges: BadgeCell[] = [];
		for (const sl of serverLenses) {
			// Badge markers ride the codeLens channel but aren't rendered as
			// lenses - peel them off before the clickable-lens decoration so a
			// data-only lens never surfaces as an empty clickable annotation.
			if (sl.data?.intent === IntentStatusBadges) {
				const cell = this.#badgeCell(sl);
				if (cell) badges.push(cell);
				continue;
			}
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
		// Forward every provideCodeLenses (empty badge included) so the sink
		// clears markers for a document that no longer reports any.
		this.#onBadges?.(document.uri, badges);
		return lenses;
	}

	#badgeCell(sl: LspCodeLens): BadgeCell | null {
		const parsed = v.safeParse(BadgeHealthsSchema, sl.data?.healths);
		if (!parsed.success || parsed.output.length === 0) {
			log(
				`Lens: rejecting badge healths: ${
					parsed.success
						? "empty"
						: parsed.issues.map((i) => i.message).join("; ")
				}`,
			);
			return null;
		}
		return {
			range: new vscode.Range(
				sl.range.start.line,
				sl.range.start.character,
				sl.range.end.line,
				sl.range.end.character,
			),
			healths: parsed.output,
		};
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
		if (intent === IntentStatusEnv) {
			return this.#decorateStatusEnv(sl, range);
		}
		if (intent === IntentStatusLoading) {
			return this.#decorateStatusLoading(sl, range);
		}
		if (intent === IntentSignIn) {
			return this.#decorateSignIn(sl, range);
		}
		if (intent === IntentActions) {
			return this.#decorateActions(sl, range);
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

	// The env-block roll-up is informational, not an action. An empty command
	// id makes VS Code render the title as a plain, non-clickable span (no
	// pointer, no hover-link) rather than a dead clickable link. A tooltip
	// (only set on the "status unavailable" case) still shows on hover.
	#decorateStatusEnv(
		sl: LspCodeLens,
		range: vscode.Range,
	): vscode.CodeLens | null {
		const title = sl.command?.title;
		if (title === undefined || title === "") return null;
		const tooltip = sl.command?.tooltip;
		return new vscode.CodeLens(range, {
			title,
			command: "",
			...(tooltip ? { tooltip } : {}),
		});
	}

	// Placeholder shown while an env's status fetch is in flight. Non-clickable
	// (empty command, like the roll-up) with a spinning sync codicon prefixed
	// client-side, so the user sees the surface is working rather than a gap.
	#decorateStatusLoading(
		sl: LspCodeLens,
		range: vscode.Range,
	): vscode.CodeLens | null {
		const title = sl.command?.title ?? "loading status...";
		return new vscode.CodeLens(range, {
			title: `$(sync~spin) ${title}`,
			command: "",
		});
	}

	#decorateSignIn(
		sl: LspCodeLens,
		range: vscode.Range,
	): vscode.CodeLens | null {
		const parsed = v.safeParse(SignInArgsSchema, sl.command?.arguments?.[0]);
		if (!parsed.success) {
			log(
				`Lens: rejecting sign-in args: ${parsed.issues.map((i) => i.message).join("; ")}`,
			);
			return null;
		}
		const args = parsed.output;
		// Sign-in launches a gaffer process, so gate it on workspace trust like
		// the debug affordances.
		if (!vscode.workspace.isTrusted) return null;
		const tomlUri = parseConfigURI(args.configURI);
		if (!tomlUri) return null;
		return new vscode.CodeLens(range, {
			title: "$(key) Sign in",
			command: "gaffer.signIn",
			arguments: [{ env: args.env, tomlUri }],
		});
	}

	// The per-projection "actions.." lens opens the action menu (diff against
	// deployed today; operate / history later). Trust-gated because every action
	// launches a gaffer process, and hidden when the CLI can't `diff` - the only
	// action wired so far, so a menu that led nowhere would be a dead end.
	#decorateActions(
		sl: LspCodeLens,
		range: vscode.Range,
	): vscode.CodeLens | null {
		const parsed = v.safeParse(
			ProjectionActionsArgsSchema,
			sl.command?.arguments?.[0],
		);
		if (!parsed.success) {
			log(
				`Lens: rejecting actions args: ${parsed.issues.map((i) => i.message).join("; ")}`,
			);
			return null;
		}
		if (!vscode.workspace.isTrusted) return null;
		if (!hasCommand(this.#manifest, "diff")) return null;
		const args = parsed.output;
		const tomlUri = parseConfigURI(args.configURI);
		if (!tomlUri) return null;
		return new vscode.CodeLens(range, {
			title: "$(list-unordered) actions..",
			command: "gaffer.projectionActions",
			arguments: [{ name: args.name, tomlUri, envs: args.envs }],
		});
	}
}
