package telemetry

import (
	"context"
	"time"
)

// Begin / End plumbing for the four long-running commands. The
// generated *Tx types in events.gen.go own the accumulator state +
// typed setters; this file owns the lifecycle (constructor, base
// stamping, outcome defaulting, panic-recover, emit).
//
// Tx is single-goroutine-owned: setters mutate tx.props without
// synchronisation, so callers must not share a *Tx across goroutines.
// Protocol-handler hot paths track counters internally and the cobra
// RunE drains them at End time via the server's Stats() accessor -
// see commit 9b+ for the per-server wiring.

// BeginDev opens a Dev command_invoked transaction. Stash the
// returned Tx and `defer tx.End(ctx)` at the top of the cobra
// RunE. Returns nil when ctx carries no Client (opt-out or
// unreadable config); End is nil-safe so callers don't need to
// branch on the return.
func BeginDev(ctx context.Context) *DevTx {
	if ClientFromContext(ctx) == nil {
		return nil
	}
	return &DevTx{}
}

// BeginMCP opens an MCP command_invoked transaction.
func BeginMCP(ctx context.Context) *MCPTx {
	if ClientFromContext(ctx) == nil {
		return nil
	}
	return &MCPTx{}
}

// BeginLSP opens an LSP command_invoked transaction.
func BeginLSP(ctx context.Context) *LSPTx {
	if ClientFromContext(ctx) == nil {
		return nil
	}
	return &LSPTx{}
}

// BeginDebug opens a Debug command_invoked transaction.
func BeginDebug(ctx context.Context) *DebugTx {
	if ClientFromContext(ctx) == nil {
		return nil
	}
	return &DebugTx{}
}

// End finalises the dev transaction: recovers any in-flight panic,
// stamps the emit-side base fields on tx.props, defaults Outcome
// from (recovered panic | ctx.Err() | success), enqueues the
// envelope, and re-panics if the command body panicked.
//
// MUST be called as `defer tx.End(ctx)` from the cobra RunE -
// directly, not from a wrapping closure. Go's recover() only fires
// when called from the immediate deferred function, so a wrapping
// `defer func() { ...; tx.End(ctx) }()` would put recover() one
// frame too deep and silently drop the panic. The deferred-defer
// pattern below ensures the recovered value is re-thrown even if
// stamping or fireCommandInvoked panics synchronously - telemetry
// failure must not mask user panics.
//
// Nil-safe: a nil *DevTx (returned by BeginDev when telemetry is
// off) just propagates any recovered panic and returns.
func (tx *DevTx) End(ctx context.Context) {
	r := recover()
	defer func() {
		if r != nil {
			panic(r)
		}
	}()
	if tx == nil {
		return
	}
	c := ClientFromContext(ctx)
	if c == nil {
		return
	}
	stampInvocationBase(&tx.props.Command, &tx.props.DurationMs, &tx.props.Outcome, &tx.props.InvokedBy, &tx.props.InvokedVia, c.startTime, CommandNameDev, ctx, r)
	c.fireCommandInvoked(tx.props)
}

// End for the MCP command. See DevTx.End for the contract.
func (tx *MCPTx) End(ctx context.Context) {
	r := recover()
	defer func() {
		if r != nil {
			panic(r)
		}
	}()
	if tx == nil {
		return
	}
	c := ClientFromContext(ctx)
	if c == nil {
		return
	}
	stampInvocationBase(&tx.props.Command, &tx.props.DurationMs, &tx.props.Outcome, &tx.props.InvokedBy, &tx.props.InvokedVia, c.startTime, CommandNameMCP, ctx, r)
	c.fireCommandInvoked(tx.props)
}

// End for the LSP command. See DevTx.End for the contract.
func (tx *LSPTx) End(ctx context.Context) {
	r := recover()
	defer func() {
		if r != nil {
			panic(r)
		}
	}()
	if tx == nil {
		return
	}
	c := ClientFromContext(ctx)
	if c == nil {
		return
	}
	stampInvocationBase(&tx.props.Command, &tx.props.DurationMs, &tx.props.Outcome, &tx.props.InvokedBy, &tx.props.InvokedVia, c.startTime, CommandNameLSP, ctx, r)
	c.fireCommandInvoked(tx.props)
}

// End for the Debug command. See DevTx.End for the contract.
func (tx *DebugTx) End(ctx context.Context) {
	r := recover()
	defer func() {
		if r != nil {
			panic(r)
		}
	}()
	if tx == nil {
		return
	}
	c := ClientFromContext(ctx)
	if c == nil {
		return
	}
	stampInvocationBase(&tx.props.Command, &tx.props.DurationMs, &tx.props.Outcome, &tx.props.InvokedBy, &tx.props.InvokedVia, c.startTime, CommandNameDebug, ctx, r)
	c.fireCommandInvoked(tx.props)
}

// stampInvocationBase fills in the four emit-side base fields plus
// the outcome default. Long-running variant of one-shot's
// stampInvocation: the extra `outcome` arg lets End cascade
// (recovered panic > ctx cancellation > success) when no setter
// gave it an explicit value. Pointer args avoid per-variant
// generics; each End passes pointers into its own tx.props.
func stampInvocationBase(
	command *CommandName,
	durationMs *RawCount,
	outcome *Outcome,
	invokedBy *InvokedBy,
	invokedVia *InvokedVia,
	startTime time.Time,
	name CommandName,
	ctx context.Context,
	recovered any,
) {
	*command = name
	*durationMs = RawCount(time.Since(startTime).Milliseconds())
	if *invokedBy == "" {
		*invokedBy = InvokedByDirect
	}
	if *invokedVia == "" {
		*invokedVia = InvokedViaTerminal
	}
	if *outcome == "" {
		switch {
		case recovered != nil:
			*outcome = OutcomeInternalError
		case ctx.Err() != nil:
			*outcome = OutcomeUserInterrupt
		default:
			*outcome = OutcomeSuccess
		}
	}
}
