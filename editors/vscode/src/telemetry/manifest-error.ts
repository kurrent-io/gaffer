// Map a manifest-fetch failure to a CLIUnreachableReason for the
// extension_activated event. The signals come from the Node
// execFile error attached to the rejection:
//
//   - `code: "ENOENT"` => the spawn itself failed because the
//     binary doesn't exist on the resolved PATH.
//   - `killed: true` (with no `code`) => execFile's `timeout` fired
//     and the process was terminated.
//   - any other `code: "E..."` => spawn happened but failed for
//     some other reason (EACCES, EISDIR, EMFILE, ...).
//   - anything else (parse error from the manifest, our own thrown
//     Error, etc.) => unknown_error.

import type { CLIUnreachableReason } from "@kurrent/gaffer-telemetry";

export function classifyManifestError(err: unknown): CLIUnreachableReason {
	if (typeof err !== "object" || err === null) return "unknown_error";
	const e = err as { code?: unknown; killed?: unknown };
	if (e.code === "ENOENT") return "binary_not_found";
	if (e.killed === true && e.code === undefined) return "timeout";
	if (typeof e.code === "string" && e.code.startsWith("E")) {
		return "binary_spawn_failed";
	}
	return "unknown_error";
}
