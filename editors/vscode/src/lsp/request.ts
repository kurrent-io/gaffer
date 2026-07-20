// Shared client for gaffer's custom LSP requests (diff, operate, and more to
// come). Centralises the getLanguageClient → sendRequest → validate scaffold and
// the auth-error mapping so each request wrapper is a one-liner and they can't
// drift apart.

import * as v from "valibot";
import { getLanguageClient } from "./client.js";

// Mirrors the server's CodeAuthRequired (protocol.go): the env needs an
// interactive sign-in. Keyed off the JSON-RPC error code, not message text, so
// the sign-in affordance is a stable signal. -32050 sits in the JSON-RPC
// server-reserved range but clear of the codes LSP assigns there (-32001/-32002).
export const LSP_AUTH_REQUIRED = -32050;

// The env needs sign-in before the request can run; callers offer a one-click
// sign-in rather than a generic error.
export class LspAuthRequiredError extends Error {
	constructor() {
		super("sign-in required");
		this.name = "LspAuthRequiredError";
	}
}

// The request couldn't be served: the language server isn't running, the request
// failed, or the response didn't validate. Callers show a generic error.
export class LspUnavailableError extends Error {
	constructor(detail: string) {
		super(detail);
		this.name = "LspUnavailableError";
	}
}

// requestError maps a sendRequest rejection to a typed error: the server's
// CodeAuthRequired becomes a sign-in prompt; anything else is generic.
export function requestError(
	err: unknown,
): LspAuthRequiredError | LspUnavailableError {
	if ((err as { code?: unknown })?.code === LSP_AUTH_REQUIRED) {
		return new LspAuthRequiredError();
	}
	return new LspUnavailableError(
		err instanceof Error ? err.message : String(err),
	);
}

// sendGafferRequest sends a custom gaffer/* request and validates the response
// against schema. Throws LspAuthRequiredError when the env needs sign-in, or
// LspUnavailableError when the server isn't running, the request fails, or the
// response doesn't validate.
export async function sendGafferRequest<TSchema extends v.GenericSchema>(
	method: string,
	params: unknown,
	schema: TSchema,
): Promise<v.InferOutput<TSchema>> {
	const client = getLanguageClient();
	if (!client) {
		throw new LspUnavailableError("the gaffer language server isn't running");
	}
	let raw: unknown;
	try {
		raw = await client.sendRequest(method, params);
	} catch (err) {
		throw requestError(err);
	}
	const parsed = v.safeParse(schema, raw);
	if (!parsed.success) {
		throw new LspUnavailableError(
			`malformed ${method} response: ${parsed.issues.map((i) => i.message).join("; ")}`,
		);
	}
	return parsed.output;
}
