// Client for the gaffer/diffProjection LSP request: the projection diff served
// over the language server's warm per-env connection, replacing a cold `gaffer
// diff --json` spawn. The response is the same shape as `gaffer diff --json`, so
// the diff editor wiring is unchanged.

import * as vscode from "vscode";
import * as v from "valibot";
import { getLanguageClient } from "./client.js";

// Mirrors the server's CodeAuthRequired (protocol.go): the env needs an
// interactive sign-in. Keyed off the JSON-RPC error code, not message text, so
// the sign-in affordance is a stable signal.
export const LSP_AUTH_REQUIRED = -32001;

// A diff side as the server reports it. Only source is needed to render;
// ref/hash are validated so a shape change is caught at the boundary.
const DiffSideSchema = v.object({
	ref: v.string(),
	hash: v.optional(v.string()),
	source: v.optional(v.string(), ""),
});

// The subset of the diff payload the editor consumes: both sides' source and
// the drift verdict (to tell "not deployed" from a real diff). Unmodelled fields
// (lines, changes, provenance) pass through ignored.
export const ProjectionDiffSchema = v.object({
	name: v.string(),
	left: DiffSideSchema,
	right: DiffSideSchema,
	verdict: v.optional(v.object({ drift: v.optional(v.string()) })),
});

export type ProjectionDiff = v.InferOutput<typeof ProjectionDiffSchema>;

// The env needs sign-in before it can be read; the caller offers a one-click
// sign-in rather than a generic error.
export class DiffAuthRequiredError extends Error {
	constructor() {
		super("sign-in required");
		this.name = "DiffAuthRequiredError";
	}
}

// The diff couldn't be produced: the language server isn't running, the request
// failed, or the response didn't validate. The caller shows a generic error.
export class DiffUnavailableError extends Error {
	constructor(detail: string) {
		super(detail);
		this.name = "DiffUnavailableError";
	}
}

// diffRequestError maps a sendRequest rejection to a typed error: the server's
// CodeAuthRequired becomes a sign-in prompt; anything else is generic.
export function diffRequestError(
	err: unknown,
): DiffAuthRequiredError | DiffUnavailableError {
	if ((err as { code?: unknown })?.code === LSP_AUTH_REQUIRED) {
		return new DiffAuthRequiredError();
	}
	return new DiffUnavailableError(
		err instanceof Error ? err.message : String(err),
	);
}

// parseDiffResponse validates the server's response; a shape mismatch is a
// DiffUnavailableError so the caller shows a generic failure rather than crash.
export function parseDiffResponse(raw: unknown): ProjectionDiff {
	const parsed = v.safeParse(ProjectionDiffSchema, raw);
	if (!parsed.success) {
		throw new DiffUnavailableError(
			`malformed diff response: ${parsed.issues.map((i) => i.message).join("; ")}`,
		);
	}
	return parsed.output;
}

// requestProjectionDiff asks the language server for a projection's deployed↔local
// diff on one env. Throws DiffAuthRequiredError when the env needs sign-in, or
// DiffUnavailableError for any other failure.
export async function requestProjectionDiff(
	name: string,
	tomlUri: vscode.Uri,
	env: string,
): Promise<ProjectionDiff> {
	const client = getLanguageClient();
	if (!client) {
		throw new DiffUnavailableError("the gaffer language server isn't running");
	}
	let raw: unknown;
	try {
		raw = await client.sendRequest("gaffer/diffProjection", {
			name,
			configURI: tomlUri.toString(),
			env,
		});
	} catch (err) {
		throw diffRequestError(err);
	}
	return parseDiffResponse(raw);
}
