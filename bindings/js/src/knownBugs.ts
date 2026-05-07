import { getNativeBindings } from "./native.js";

/**
 * Describes a KurrentDB upstream bug that gaffer reproduces. The runtime is
 * the source of truth; consumers (CLI, MCP, tests) call {@link knownBugs} to
 * fetch the current registry.
 *
 * `fixedIn` is `undefined` when no upstream fix has shipped (the bug fires in
 * every configuration). When set, it's a `MAJOR.MINOR.PATCH` string; setting
 * `dbVersion >= fixedIn` disables the bug.
 */
export interface KnownBug {
	code: string;
	description: string;
	fixedIn?: string;
}

/**
 * Returns the runtime's registry of compat-tracked bugs. Loaded each call
 * from the native runtime; cache at the call site if used hot.
 */
export function knownBugs(): KnownBug[] {
	const json = getNativeBindings().knownBugs();
	return JSON.parse(json) as KnownBug[];
}
