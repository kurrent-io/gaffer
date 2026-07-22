// Client for the gaffer/diffProjection LSP request: the projection diff served
// over the language server's warm per-env connection, replacing a cold `gaffer
// diff --json` spawn. The response is the same shape as `gaffer diff --json`, so
// the diff editor wiring is unchanged.

import * as vscode from "vscode";
import * as v from "valibot";
import { sendGafferRequest } from "./request.js";

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

// requestProjectionDiff asks the language server for a projection's deployed↔local
// diff on one env. Throws LspAuthRequiredError when the env needs sign-in, or
// LspUnavailableError for any other failure (see sendGafferRequest).
export function requestProjectionDiff(
	name: string,
	tomlUri: vscode.Uri,
	env: string,
): Promise<ProjectionDiff> {
	return sendGafferRequest(
		"gaffer/diffProjection",
		{ name, configURI: tomlUri.toString(), env },
		ProjectionDiffSchema,
	);
}

// requestDiffVersions asks the language server for a diff between two versions of
// a projection - each ref a content hash, "deployed", or "local" - over its warm
// per-env connection, for the history viewer. Same response shape as
// diffProjection (minus the verdict). Throws LspAuthRequiredError when the env
// needs sign-in, LspUnavailableError otherwise.
export function requestDiffVersions(
	name: string,
	tomlUri: vscode.Uri,
	env: string,
	left: string,
	right: string,
): Promise<ProjectionDiff> {
	return sendGafferRequest(
		"gaffer/diffVersions",
		{ name, configURI: tomlUri.toString(), env, left, right },
		ProjectionDiffSchema,
	);
}
