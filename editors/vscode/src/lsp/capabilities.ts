// What the language server told us it can do, read from the initialize result.
//
// The extension ships independently of the CLI - it auto-updates from the
// marketplace while `@kurrent/gaffer` is installed separately - so a new
// extension routinely drives an older gaffer. LSP advertises standard features
// through standard capability fields, but has no slot for a server's own
// requests, so gaffer namespaces them under the spec's `experimental` escape
// hatch as `experimental.gaffer.methods` (see gafferMethods in
// cli/internal/lsp/handlers.go).
//
// Surfaces gate on `serverServes` rather than calling and handling the failure:
// a hidden affordance is a better answer than one that errors after a click.

import * as v from "valibot";
import { log } from "../output.js";

// Validated at the wire boundary - the initialize result is data from another
// process, not a trusted internal shape. Every level is optional because a
// server that predates the field omits it entirely, which must parse cleanly to
// an empty set rather than failing.
const InitializeResultSchema = v.object({
	capabilities: v.optional(
		v.object({
			experimental: v.optional(
				v.nullable(
					v.object({
						gaffer: v.optional(
							v.object({
								methods: v.optional(v.array(v.string())),
							}),
						),
					}),
				),
			),
		}),
	),
});

// The set for the running server. Empty both before a server has started and
// when the running one advertises nothing - those cases are treated alike on
// purpose, since a server that can't tell us what it serves is one we shouldn't
// offer its surfaces for.
let servedMethods: ReadonlySet<string> = new Set();

/**
 * Parse the served gaffer/* methods out of an initialize result. Returns an
 * empty set for a server that doesn't advertise them (any gaffer before the
 * capability landed) and for a malformed block - in both cases we don't know
 * what's served, which the callers read as "don't offer it".
 */
export function readServedMethods(result: unknown): ReadonlySet<string> {
	const parsed = v.safeParse(InitializeResultSchema, result);
	if (!parsed.success) {
		log(
			`LSP: ignoring malformed initialize result: ${parsed.issues
				.map((i) => i.message)
				.join("; ")}`,
		);
		return new Set();
	}
	const methods = parsed.output.capabilities?.experimental?.gaffer?.methods;
	if (methods === undefined) return new Set();
	return new Set(methods);
}

/**
 * Record what the newly-started server serves. Called on every successful
 * start, including restarts - a restart can land on a different binary if the
 * user updated the CLI, so the set is replaced rather than merged.
 */
export function setServedMethods(methods: ReadonlySet<string>): void {
	servedMethods = methods;
	log(
		methods.size > 0
			? `LSP serves ${methods.size} gaffer method(s): ${[...methods].sort().join(", ")}`
			: "LSP advertises no gaffer methods - capability-gated surfaces stay hidden",
	);
}

/** Drop the recorded set when no server is running, so a stale capability can't
 * keep a surface visible after the server goes away. */
export function clearServedMethods(): void {
	servedMethods = new Set();
}

/**
 * Whether the running server serves this gaffer/* method.
 *
 * False when no server is running, when the server predates the capability, and
 * when it advertises the method's absence - all three mean the same thing to a
 * caller deciding whether to offer a surface.
 */
export function serverServes(method: string): boolean {
	return servedMethods.has(method);
}

// The gaffer/* methods the extension gates surfaces on and sends. Defined here so
// each name has a single definition shared by the gate and the send site - the
// two matching is the whole mechanism, and a name that drifts between them reads
// as permanently unsupported rather than as an error. served-methods.test.ts
// checks them against what the CLI actually advertises.
//
// projectionDetails is deliberately not gated on: every gaffer the extension can
// drive serves it, so a gate would add a way to break workspace symbols for no
// benefit. It has a constant so the name is covered by that test.
export const METHOD_DIFF_PROJECTION = "gaffer/diffProjection";
export const METHOD_DIFF_VERSIONS = "gaffer/diffVersions";
export const METHOD_OPERATE_PROJECTION = "gaffer/operateProjection";
export const METHOD_REFRESH_STATUS = "gaffer/refreshStatus";
export const METHOD_PROJECTION_DETAILS = "gaffer/projectionDetails";
