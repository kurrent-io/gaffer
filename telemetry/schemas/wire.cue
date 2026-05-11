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
	// Which gaffer surface emitted this envelope.
	emitter: "cli" | "mcp" | "extension"

	// Gaffer release version (semver).
	lib_version: string & strings.MaxRunes(32)

	// Host OS.
	os: "linux" | "darwin" | "windows"

	// Host CPU architecture.
	arch: "x64" | "arm64" | "ia32"

	// "ci" if any standard CI env var indicates an automated environment;
	// "local" otherwise.
	runtime_environment: "ci" | "local"

	// "Hello, this is my first envelope" flag, set on the first envelope
	// sent from a fresh install and absent thereafter. ISO 8601 date.
	install_date?: string & strings.MaxRunes(32)

	// Spawn-time identity link. Set when an extension spawns a CLI
	// process, populated with the spawning extension's emitter_id
	// (parsed from the GAFFER_INVOKER env var).
	invoker_id?: #UUID
}

// Lowercase-hyphen UUID, no curly braces.
#UUID: =~"^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$"
