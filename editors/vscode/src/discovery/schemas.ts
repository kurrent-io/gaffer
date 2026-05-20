// Schema for the `gaffer manifest` JSON output.
//
// Validated in cli.ts at the JSON.parse boundary - we do not trust the
// manifest format.

import * as v from "valibot";

export const ManifestSchema = v.object({
	version: v.string(),
	// Optional rather than nullable-only: older gaffer binaries
	// predate the field and omit the key entirely.
	updateAvailable: v.optional(v.nullable(v.string())),
	commands: v.record(
		v.string(),
		v.object({
			flags: v.optional(v.array(v.string())),
		}),
	),
});
export type Manifest = v.InferOutput<typeof ManifestSchema>;
