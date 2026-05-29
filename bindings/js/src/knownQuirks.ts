import { getNativeBindings } from "./native.js";

/**
 * Describes a KurrentDB upstream quirk that gaffer reproduces. The runtime is
 * the source of truth; consumers (CLI, MCP, tests) call {@link knownQuirks} to
 * fetch the current registry.
 *
 * `fixedIn` is `undefined` when no upstream fix has shipped (the quirk fires in
 * every configuration). When set, it's a `MAJOR.MINOR.PATCH` string; setting
 * `quirksVersion >= fixedIn` disables the quirk.
 */
export interface KnownQuirk {
	code: string;
	description: string;
	fixedIn?: string;
}

/**
 * Returns the runtime's registry of compat-tracked quirks. Loaded each call
 * from the native runtime; cache at the call site if used hot.
 */
export function knownQuirks(): KnownQuirk[] {
	const json = getNativeBindings().knownQuirks();
	// Native returns null on allocation failure. Surface as a descriptive
	// error rather than letting JSON.parse(null) throw "Unexpected token u".
	if (json === null) {
		throw new Error("gaffer_known_quirks returned null (allocation failure)");
	}
	return JSON.parse(json) as KnownQuirk[];
}
