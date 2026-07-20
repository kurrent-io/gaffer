// Client for the gaffer/operateProjection LSP request: run an operate verb
// (pause/resume/abort/delete) on one projection over the language server's warm
// per-env connection. The editor renders the confirm tier before calling, so the
// server performs the write unconditionally.

import * as vscode from "vscode";
import * as v from "valibot";
import { sendGafferRequest } from "./request.js";

// The operate verbs the menu offers. recreate/rollback live on other surfaces.
export type OperateVerb = "pause" | "resume" | "abort" | "delete";

const OperateResultSchema = v.object({
	name: v.string(),
	outcome: v.string(),
	target: v.optional(v.string(), ""),
});

export type OperateResult = v.InferOutput<typeof OperateResultSchema>;

export interface OperateRequestArgs {
	name: string;
	tomlUri: vscode.Uri;
	env: string;
	verb: OperateVerb;
	deleteEmitted?: boolean;
}

// requestOperateProjection runs one operate verb on a projection. Throws
// LspAuthRequiredError when the env needs sign-in, or LspUnavailableError for any
// other failure (see sendGafferRequest).
export function requestOperateProjection(
	args: OperateRequestArgs,
): Promise<OperateResult> {
	return sendGafferRequest(
		"gaffer/operateProjection",
		{
			name: args.name,
			configURI: args.tomlUri.toString(),
			env: args.env,
			verb: args.verb,
			deleteEmitted: args.deleteEmitted ?? false,
		},
		OperateResultSchema,
	);
}
