// Package telemetry sends anonymised usage events from the gaffer CLI to a
// Cloudflare worker that forwards them to PostHog. Generation, identity,
// opt-out, transport, and the per-command Tx wrappers all live here.
//
// # Goroutine ownership
//
// Tx values (DevTx, MCPTx, ...) are single-goroutine-owned. The setter
// methods are NOT safe for concurrent use, and `defer tx.End()` only
// catches panics in the same goroutine. Long-running protocol servers
// must drain their internal counters on the main goroutine before End()
// runs, typically via a Stats() accessor exposed by the server.
//
// The Client's own emit / Flush methods ARE safe to call concurrently
// from multiple goroutines - that's how a long-running command can both
// emit projection_shape events from request handlers and still call Flush
// from main on exit.
//
// # Helper API
//
// One-shot events (Version, Init, Scaffold, Info, Manifest) use
// `telemetry.Emit<X>(ctx, props)`. Long-running commands (Dev, MCP, LSP,
// Debug) use `tx := telemetry.Begin<X>(ctx)` returning a *<X>Tx, with
// `defer tx.End(ctx)` to emit on return.
//
// # End contract (load-bearing)
//
// `tx.End(ctx)` MUST be deferred directly:
//
//	tx := telemetry.BeginDev(ctx)
//	defer tx.End(ctx)
//
// Wrapping End in a closure (`defer func() { ...; tx.End(ctx) }()`)
// silently breaks recover(): Go's recover() only fires when called from
// the IMMEDIATE deferred function. The wrapping closure becomes the
// deferred frame; End's recover() runs one frame too deep and returns
// nil, so a body panic propagates unrecovered and the matching
// command_invoked envelope never emits with outcome=internal_error.
//
// End's body also installs a deferred re-panic so the original panic
// propagates even if stamping or fireCommandInvoked itself panics
// synchronously - telemetry failure must not mask user panics.
//
// TestDevTx_EndMustBeDirectDeferShape pins this invariant against
// future refactors.
//
// # Outcome cascade
//
// At End time, the Outcome field is filled (highest priority first):
//   - explicit `tx.SetOutcome(...)` from the command body wins
//   - recovered panic -> internal_error
//   - ctx.Err() != nil -> user_interrupt
//   - fallthrough -> success
//
// The cobra RunE wrappers also map non-nil retErr to a coarse default
// (user_error for dev, protocol-specific errors for mcp/lsp) as a
// safety net for unclassified errors. Specific outcomes
// (manifest_not_found, db_disconnect, fixture_exhausted, projection_*)
// belong inline at the error site so the wrapper fallback only fires
// for genuinely-unclassified failures.
//
// # Best-effort transport
//
// Sends are fire-and-forget: every emit spawns a goroutine that runs the
// sink synchronously with a per-send deadline (default 2 seconds).
// Goroutines are tracked by an internal sync.WaitGroup that Flush drains
// at process exit. Errors land in the configured error log (no-op by
// default - the CLI never surfaces telemetry failures to the user; tests
// and a future GAFFER_TELEMETRY_DEBUG=1 path inject their own logger).
// A panicking sink (a buggy decorator, a runtime error inside http.Client)
// is recovered inside the goroutine and reported via the error log; it
// can never propagate up to kill the CLI.
//
// # Flush contract
//
// Call Flush exactly once at process exit, bounded by a caller-supplied
// context.WithTimeout. After Flush returns (cleanly or via timeout), the
// Client is closed: further emits become silent no-ops. Flush itself is
// idempotent; calling it a second time waits again on whatever's still
// outstanding (typically nothing) and returns promptly.
//
// # Envelope ownership
//
// Each emit takes ownership of the *Envelope passed in. The caller must
// not mutate or re-use the value after emit returns. The generated Tx
// types follow this contract by building a fresh Envelope inside End()
// and dropping the reference.
package telemetry
