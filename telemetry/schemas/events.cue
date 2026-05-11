// Package telemetry, file events.cue: the event types gaffer emits.
package telemetry

import "strings"

// The four events gaffer emits.
#Event: #CommandInvoked | #ProjectionShape | #ExtensionActivated | #Exception

// The set of gaffer commands. Source of truth for the discriminator on
// `command_invoked` variants and the `command` field on `exception`.
#CommandName: "version" | "init" | "scaffold" | "manifest" | "info" | "dev" | "mcp" | "lsp" | "debug"

// ----------------------------------------------------------------------------
// command_invoked
// ----------------------------------------------------------------------------

// CommandInvoked fires once at the end of every CLI invocation - one-shot or
// long-running. The `command` property identifies which command ran.
#CommandInvoked: {
	name:      "command_invoked"
	timestamp: #Timestamp
	properties:
		#VersionCommandInvokedProperties |
		#InitCommandInvokedProperties |
		#ScaffoldCommandInvokedProperties |
		#InfoCommandInvokedProperties |
		#ManifestCommandInvokedProperties |
		#DevCommandInvokedProperties |
		#McpCommandInvokedProperties |
		#LspCommandInvokedProperties |
		#DebugCommandInvokedProperties
}

// CommandInvokedBaseProperties is the set of properties present on every
// command_invoked event regardless of which command ran. Variants embed this
// (`{ #CommandInvokedBaseProperties, extra fields }`) rather than unifying
// with `&`, because CUE definitions are recursively closed and `& { extras }`
// would silently drop the additions. Embedding composes the shared fields
// into each variant without inheriting the base's closedness in a way that
// blocks per-variant additions.
#CommandInvokedBaseProperties: {
	// Which gaffer command ran. Variants narrow this to a specific literal.
	command: #CommandName

	// Wall-clock duration from process start to exit. For long-running
	// commands (`dev`, `mcp`, `lsp`, `debug`) this is invocation
	// lifetime, not engagement time - includes idle stretches where the
	// editor was open but nobody was touching gaffer.
	duration_ms: #BucketCount

	// What ended the invocation.
	outcome: #Outcome

	// Who triggered the run.
	invoked_by: #InvokedBy

	// Specific surface the invocation came through.
	invoked_via: #InvokedVia
}

// InvokedBy is who triggered a CLI run.
#InvokedBy: "direct" | "vscode" | "mcp_client"

// InvokedVia is the specific surface the invocation came through.
#InvokedVia: "terminal" | "code_lens" | "command_palette" | "mcp_provider" | "stdio"

// `gaffer version` - print version, exit. No command-specific properties.
#VersionCommandInvokedProperties: {
	#CommandInvokedBaseProperties
	command: "version"
}

// `gaffer init` - scaffold a new project. No command-specific properties.
#InitCommandInvokedProperties: {
	#CommandInvokedBaseProperties
	command: "init"
}

// `gaffer scaffold` - generate boilerplate. No command-specific properties.
#ScaffoldCommandInvokedProperties: {
	#CommandInvokedBaseProperties
	command: "scaffold"
}

// `gaffer info` - print environment + manifest summary. No command-specific
// properties.
#InfoCommandInvokedProperties: {
	#CommandInvokedBaseProperties
	command: "info"
}

// `gaffer manifest` - parse and report manifest contents.
#ManifestCommandInvokedProperties: {
	#CommandInvokedBaseProperties
	command: "manifest"

	// Top-level manifest section names present (e.g. ["projections",
	// "fixtures"]). Section *presence* only, never contents.
	manifest_features_used?: [...string & strings.MaxRunes(64)]

	// Bucketed count from manifest.
	projection_count?: #BucketCount

	// Bucketed count from manifest.
	fixture_count?: #BucketCount
}

// `gaffer dev` - long-running development loop, optionally connected to a
// live KurrentDB.
#DevCommandInvokedProperties: {
	#CommandInvokedBaseProperties
	command: "dev"

	// Top-level manifest section names present.
	manifest_features_used?: [...string & strings.MaxRunes(64)]

	// Bucketed count from manifest.
	projection_count?: #BucketCount

	// Bucketed count from manifest.
	fixture_count?: #BucketCount

	// Whether a live KurrentDB connection was requested.
	connected_to_db?: bool

	// Major.minor of the connected server (e.g. "26.1", "27.0") truncated
	// from the full server `/info` version string, or "unknown" if it was
	// unparseable. Absent when not connected.
	db_version?: string & strings.MaxRunes(32)

	// Distinct, sorted set of `projection_*` outcome values seen during
	// the run. Empty when none.
	projection_errors_seen?: [...#ProjectionOutcome]
}

// `gaffer mcp` - long-running Model Context Protocol server.
#McpCommandInvokedProperties: {
	#CommandInvokedBaseProperties
	command: "mcp"

	// Top-level manifest section names present.
	manifest_features_used?: [...string & strings.MaxRunes(64)]

	// Bucketed. Total tool invocations across the session.
	tool_call_count?: #BucketCount

	// Bucketed. Total resource reads across the session.
	resource_read_count?: #BucketCount

	// Distinct, sorted set of `projection_*` outcome values seen during
	// the session. Empty when none.
	projection_errors_seen?: [...#ProjectionOutcome]
}

// `gaffer lsp` - language server.
#LspCommandInvokedProperties: {
	#CommandInvokedBaseProperties
	command: "lsp"

	// Bucketed total code-lens requests served.
	code_lens_request_count?: #BucketCount

	// Bucketed total diagnostic publishes.
	diagnostic_publish_count?: #BucketCount
}

// `gaffer debug` - DAP server for projection debugging.
#DebugCommandInvokedProperties: {
	#CommandInvokedBaseProperties
	command: "debug"

	// Bucketed. Initial breakpoints set by the client.
	breakpoint_count?: #BucketCount

	// Bucketed. Total step requests served.
	step_count?: #BucketCount

	// Bucketed. Times the session paused (breakpoint or pause request).
	pause_count?: #BucketCount

	// Bucketed. DAP restarts.
	restart_count?: #BucketCount

	// Bucketed. Events fed from the fixture.
	fixture_event_count?: #BucketCount

	// Distinct, sorted set of `projection_*` outcome values seen during
	// the session. Empty when none.
	projection_errors_seen?: [...#ProjectionOutcome]
}

// Outcome is the *final* outcome of a command invocation - whatever made it
// actually exit. Long-running sessions that hit transient errors and
// recovered carry the recovered outcome (typically `user_interrupt`) here,
// with the transient errors captured in `projection_errors_seen` for the
// commands that have it.
#Outcome:
	"success" |
	"user_interrupt" |
	"user_error" |
	"caught_up" |
	"internal_error" |
	"manifest_not_found" |
	"manifest_parse_error" |
	"manifest_validation_error" |
	"db_connect_error" |
	"db_disconnect" |
	"db_protocol_error" |
	"dap_protocol_error" |
	"lsp_protocol_error" |
	"mcp_protocol_error" |
	"fixture_exhausted" |
	#ProjectionOutcome

// The subset of #Outcome values that come from user-projection failure. Used
// both as final outcomes and as transient values in `projection_errors_seen`.
#ProjectionOutcome:
	"projection_user_throw" |
	"projection_reference_error" |
	"projection_type_error" |
	"projection_parse_error" |
	"projection_syntax_error" |
	"projection_range_error" |
	"projection_uri_error" |
	"projection_eval_error" |
	"projection_oom" |
	"projection_stack_overflow" |
	"projection_unknown_error"

// ----------------------------------------------------------------------------
// projection_shape
// ----------------------------------------------------------------------------

// ProjectionShape carries a snapshot of what a projection's source looks
// like, structurally. Emitted on first encounter and again whenever the
// bucketed shape drifts (whitespace / comment edits don't trigger re-emit).
// Booleans, enums, counts. Never names.
#ProjectionShape: {
	name:       "projection_shape"
	timestamp:  #Timestamp
	properties: #ProjectionShapeProperties
}

#ProjectionShapeProperties: {
	// Per-install hashed id: sha256(salt || projection_relative_path)[:16].
	// Stable across processes for the same projection. 16 hex chars.
	projection_id: =~"^[0-9a-f]{16}$"

	// True when the AST parser succeeded. False = parser error; the
	// `handlers` and `builtin_counts` data are best-effort partial.
	parsable: bool

	// Bucketed file size on disk in bytes.
	file_size: #FileSizeBucket

	// Which handlers the projection registers.
	handlers: {
		// `$any` handler registered.
		any: bool

		// `$init` or `$initShared` handler registered.
		init: bool

		// `$deleted` handler registered.
		deleted: bool

		// Bucketed count of distinct event-name handlers (i.e. those
		// other than `$any`, `$init`, `$deleted`). Names themselves are
		// never sent.
		distinct_event_names: #BucketCount
	}

	// Bucketed call counts per allowlisted projection builtin. Sparse -
	// a builtin not called is absent. Keys are the JS API names verbatim.
	// Adding a new builtin to gaffer means adding it here, the AST
	// walker, and the deprecation-diagnostic visitor in the same PR.
	builtin_counts: {
		fromAll?:        #BucketCount
		fromStream?:     #BucketCount
		fromStreams?:    #BucketCount
		fromCategory?:   #BucketCount
		fromCategories?: #BucketCount
		when?:           #BucketCount
		foreachStream?:  #BucketCount
		outputState?:    #BucketCount
		transformBy?:    #BucketCount
		partitionBy?:    #BucketCount
		emit?:           #BucketCount
		linkTo?:         #BucketCount
		copyTo?:         #BucketCount
		// linkStreamTo is deprecated; tracked because deprecated.
		linkStreamTo?:  #BucketCount
		chainHandlers?: #BucketCount
		updateOf?:      #BucketCount
	}
}

// ----------------------------------------------------------------------------
// extension_activated
// ----------------------------------------------------------------------------

// ExtensionActivated fires once on extension activation. The one event that
// can fire when gaffer is otherwise unable to run, used to detect broken
// installs.
#ExtensionActivated: {
	name:       "extension_activated"
	timestamp:  #Timestamp
	properties: #ExtensionActivatedProperties
}

#ExtensionActivatedProperties: {
	editor: #Editor

	// Editor version string (e.g. "1.95.2").
	editor_version: string & strings.MaxRunes(32)

	// Whether the gaffer CLI binary was reachable on activation
	// (PATH-resolvable + spawnable + responded to `gaffer version` within
	// timeout).
	cli_reachable: bool

	// Set when `cli_reachable = false`; absent otherwise.
	cli_unreachable_reason?: #CLIUnreachableReason

	// Bucketed major.minor of the CLI binary's reported version. Present
	// when `cli_reachable = true`.
	cli_version?: string & strings.MaxRunes(32)

	// Time from extension activation to first event-emit decision.
	activation_duration_ms: #BucketCount
}

// Editor is the specific editor runtime detected at activation (from
// `vscode.env.uriScheme`). Distinguishes VS Code from its forks so we know
// which targets to test against; VSCodium / Cursor / Windsurf all have
// non-trivial adoption. Stable and insiders builds collapse to `"vscode"`;
// unknown forks map to `"other"`.
#Editor: "vscode" | "vscodium" | "cursor" | "windsurf" | "other"

// CLIUnreachableReason narrows why the CLI couldn't be reached on extension
// activation.
#CLIUnreachableReason: "binary_not_found" | "binary_spawn_failed" | "timeout" | "unknown_error"

// ----------------------------------------------------------------------------
// exception
// ----------------------------------------------------------------------------

// Exception captures gaffer-side crashes only - Go panics in the CLI / MCP,
// unhandled JS errors in the extension host, runtime exceptions in our own
// .NET code. Projection runtime errors are NOT in scope; they surface as
// `outcome` on the relevant `command_invoked` event.
#Exception: {
	name:       "exception"
	timestamp:  #Timestamp
	properties: #ExceptionProperties
}

#ExceptionProperties: {
	// Causal chain of exceptions, outer wrapper first, root cause last.
	exceptions: [...#ExceptionEntry]

	// The CLI command that was running, if any. May be absent when the
	// crash happens before command dispatch.
	command?: #CommandName

	// Coarse lifecycle bucket the crash happened in.
	phase: #ExceptionPhase
}

// ExceptionPhase is a coarse lifecycle bucket for where an exception fired.
#ExceptionPhase: "startup" | "projection_init" | "event_processing" | "shutdown"

// ExceptionEntry is one exception in the causal chain.
#ExceptionEntry: {
	// Exception type name (e.g. "RuntimeError", "TypeError").
	type: string & strings.MaxRunes(200)

	// Exception message. ONLY ever a message gaffer wrote - never a Jint
	// or FFI-bubbled message that could embed user identifiers, source
	// snippets, or function names from user code.
	value: string & strings.MaxRunes(2000)

	// True for gaffer-owned code; false for stdlib / vendored deps.
	in_app: bool

	// Stack frames. User-JS frames are dropped entirely; remaining
	// frames carry basename + function + line.
	stacktrace: {
		type: "raw"
		frames: [...#Frame]
	}
}

#Frame: {
	// File basename only (never full path).
	filename: string & strings.MaxRunes(200)

	// Function name. May be absent for anonymous frames.
	function?: string & strings.MaxRunes(200)

	// 1-indexed line number in the source. May be absent if unknown.
	lineno?: int & >=1

	// True for gaffer-owned code, false for stdlib / vendored deps.
	in_app: bool
}

// ----------------------------------------------------------------------------
// Shared primitives
// ----------------------------------------------------------------------------

// RFC 3339 timestamp with millisecond precision.
#Timestamp: string & strings.MaxRunes(64)

// Bucketed count.
#BucketCount:
	// none
	0 |
	// exactly one
	1 |
	// 2-9
	2 |
	// 10-99
	10 |
	// 100-999
	100 |
	// 1000+
	1000

// Bucketed file size on disk in bytes (lower-bound of the half-open
// interval).
#FileSizeBucket:
	// under 1KB
	0 |
	// 1KB-5KB
	1024 |
	// 5KB-20KB
	5120 |
	// 20KB-100KB
	20480 |
	// 100KB+
	102400
