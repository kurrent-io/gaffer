// Host-only types - things the extension's own code passes around that
// never appear on the wire. Wire-format schemas live with their consumers
// under each feature folder's schemas.ts.

export interface DebugState {
	name: string | null;
	status: "idle" | "starting" | "debugging";
}

export interface ProjectEntry {
	name: string;
	tomlDir: string;
}
