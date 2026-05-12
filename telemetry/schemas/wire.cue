// Package telemetry, file wire.cue: the HTTP envelope gaffer's surfaces POST
// to the telemetry worker.
package telemetry

import "strings"

// Envelope is the top-level shape POSTed to the worker. One envelope per HTTP
// request, carrying a batch of events that share the same emitter and run.
#Envelope: {
	// Bumped only for breaking wire changes; additive changes keep the
	// same version.
	schema_version: "1"

	// Per-install random UUID identifying this gaffer installation.
	emitter_id: #UUID

	// Per-process UUID identifying a single CLI invocation or extension
	// activation. Correlates events emitted from the same process.
	run_id: #UUID

	context: #Context
	events: [...#Event]
}

// Context is per-envelope metadata describing the emitting environment.
// Identical for every event in a batch.
#Context: {
	emitter: #Emitter

	// Gaffer release version (semver).
	lib_version: string & strings.MaxRunes(32)

	os:                  #OS
	arch:                #Arch
	runtime_environment: #RuntimeEnvironment

	// Salted hash of the project root absolute path -
	// sha256(salt || abs-project-root)[:16]. Lets us distinguish one user
	// with five projects from five users with one project. The path itself
	// never leaves the machine. Absent for invocations that didn't run
	// inside a project (e.g. `gaffer version`).
	project_id?: =~"^[0-9a-f]{16}$"

	// Spawn-time identity link. Set when an extension spawns a CLI
	// process, populated with the spawning extension's emitter_id
	// (parsed from the --invoker-id flag).
	invoker_id?: #UUID
}

// Emitter is which gaffer surface emitted an envelope. Aligned with
// `invoked_by` on `command_invoked`: where the same surface appears in both
// fields it carries the same identifier. The asymmetry (`emitter` doesn't
// take `direct` / `mcp_client` / `ci`; `invoked_by` doesn't take `mcp`) is
// load-bearing - `mcp` means "gaffer's own MCP server emitted this" whereas
// `invoked_by: "mcp_client"` means "an external MCP host invoked us".
#Emitter: "cli" | "mcp" | "vscode"

// Host OS.
#OS: "linux" | "darwin" | "windows"

// Host CPU architecture.
#Arch: "x64" | "arm64" | "ia32"

// "ci" if any standard CI env var indicates an automated environment;
// "local" otherwise.
#RuntimeEnvironment: "ci" | "local"

// Lowercase-hyphen UUID, no curly braces.
#UUID: =~"^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$"
