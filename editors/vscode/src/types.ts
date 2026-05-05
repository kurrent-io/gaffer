// Host-only types - things the extension's own code passes around that
// never appear on the wire. Wire-format schemas live with their consumers
// under each feature folder's schemas.ts.

export interface DebugState {
	name: string | null;
	status: "idle" | "starting" | "running" | "inspecting" | "ended";
}

export interface ProjectEntry {
	name: string;
	tomlDir: string;
	fixtures: ReadonlyArray<ProjectFixture>;
	// Fixtures we parsed but rejected (duplicate name, path
	// escape). Surface as a non-actionable error lens in the toml
	// view; the user fixes them at source. We drop them from
	// `fixtures` so they can never reach the JS lens dropdown or
	// session args.
	invalidFixtures: ReadonlyArray<InvalidProjectFixture>;
}

export interface ProjectFixture {
	name: string;
	path: string;
}

export interface InvalidProjectFixture {
	name?: string;
	path?: string;
	reason: string;
}
