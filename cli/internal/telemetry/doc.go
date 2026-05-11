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
// `defer tx.End()` to emit on return. The Begin/End helpers and the
// runtime that backs them are wired in subsequent commits; this file
// documents the surface the call sites will use.
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
