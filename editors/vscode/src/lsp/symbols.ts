import * as vscode from "vscode";
import * as v from "valibot";
import { getLanguageClient } from "./client.js";
import { log } from "../output.js";

const EnvSchema = v.object({
	name: v.string(),
	default: v.optional(v.boolean(), false),
});

const ProjectionDetailsSchema = v.object({
	connection: v.nullable(v.string()),
	fixtures: v.array(v.string()),
	environments: v.optional(v.array(EnvSchema), []),
});

export type ProjectionDetails = v.InferOutput<typeof ProjectionDetailsSchema>;

// Wire shape - mirrors the LSP server's emitWorkspaceSymbols
// (legacy SymbolInformation form, not LSP 3.17 WorkspaceSymbol).
const SymbolInformationSchema = v.object({
	name: v.string(),
	kind: v.number(),
	location: v.object({
		uri: v.string(),
		range: v.object({
			start: v.object({ line: v.number(), character: v.number() }),
			end: v.object({ line: v.number(), character: v.number() }),
		}),
	}),
	containerName: v.optional(v.string()),
});
const SymbolListSchema = v.array(SymbolInformationSchema);

export interface ProjectionSymbol {
	name: string;
	tomlUri: vscode.Uri;
}

export type FetchProjectionsResult =
	| { kind: "ok"; projections: ProjectionSymbol[] }
	| { kind: "not-ready" }
	| { kind: "error" };

/**
 * Fetch every projection across the workspace via the LSP
 * server's `workspace/symbol` endpoint. Replaces the in-process
 * `createProjectIndex().projections` walk - the server is the
 * single source of truth for which projections exist.
 *
 * Returns a tagged result so callers can differentiate the
 * three failure modes:
 *   - `not-ready`: client isn't up yet (e.g. user fired the
 *     command before the LSP `initialize` round-trip
 *     completed, or the workspace is untrusted and the spawn
 *     was deferred). Show a "still starting" message.
 *   - `error`: request failed on the wire. Logged.
 *   - `ok`: list (possibly empty) of projections.
 */
export async function fetchProjections(): Promise<FetchProjectionsResult> {
	const client = getLanguageClient();
	if (!client) return { kind: "not-ready" };
	let raw: unknown;
	try {
		raw = await client.sendRequest("workspace/symbol", { query: "" });
	} catch (err) {
		log(
			`workspace/symbol failed: ${err instanceof Error ? err.message : String(err)}`,
		);
		return { kind: "error" };
	}
	if (raw == null) return { kind: "ok", projections: [] };
	const parsed = v.safeParse(SymbolListSchema, raw);
	if (!parsed.success) {
		log(
			`workspace/symbol: malformed response: ${parsed.issues.map((i) => i.message).join("; ")}`,
		);
		return { kind: "error" };
	}
	const projections: ProjectionSymbol[] = [];
	for (const s of parsed.output) {
		try {
			projections.push({
				name: s.name,
				tomlUri: vscode.Uri.parse(s.location.uri, true),
			});
		} catch (err) {
			log(
				`workspace/symbol: rejecting malformed location.uri ${JSON.stringify(s.location.uri)}: ${
					err instanceof Error ? err.message : String(err)
				}`,
			);
		}
	}
	return { kind: "ok", projections };
}

/**
 * Fetch the parsed details for a single projection: its
 * project-level connection (or null if undeclared) and the list
 * of named fixtures. Used by the Run Projection picker to choose
 * between live and fixture runs.
 *
 * Returns null on any failure (LSP not ready, request error,
 * malformed response). The caller falls back to "live" - the
 * worst case is the user sees the same single-step pick as
 * before the feature landed.
 */
export async function fetchProjectionDetails(
	name: string,
	tomlUri: vscode.Uri,
): Promise<ProjectionDetails | null> {
	const client = getLanguageClient();
	if (!client) return null;
	let raw: unknown;
	try {
		raw = await client.sendRequest("gaffer/projectionDetails", {
			name,
			configURI: tomlUri.toString(),
		});
	} catch (err) {
		log(
			`gaffer/projectionDetails failed: ${err instanceof Error ? err.message : String(err)}`,
		);
		return null;
	}
	const parsed = v.safeParse(ProjectionDetailsSchema, raw);
	if (!parsed.success) {
		log(
			`gaffer/projectionDetails: malformed response: ${parsed.issues.map((i) => i.message).join("; ")}`,
		);
		return null;
	}
	return parsed.output;
}
