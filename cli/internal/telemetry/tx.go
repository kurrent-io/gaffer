package telemetry

import (
	"context"
	"time"
)

// Hand-written runtime that the generated tx.gen.go binds against.
// The Begin<Cmd> constructors and (*<Cmd>Tx).End(ctx) methods are
// generated; only the shared base-stamping policy lives here.
//
// Tx is single-goroutine-owned: setters mutate tx.props without
// synchronisation, so callers must not share a *Tx across goroutines.
// Protocol-handler hot paths track counters internally and the cobra
// RunE drains them at End time via the server's Stats() accessor.

// stampInvocationBase fills in the four emit-side base fields plus
// the outcome default for a long-running command_invoked variant.
// Pointer args let one body handle all four concrete Tx types
// without reflection or generics; each generated End passes
// pointers into its own tx.props.
//
// InvokedBy / InvokedVia defaults route through the Client so the
// command-aware rule (mcp -> mcp_client / stdio) and the explicit
// --invoked-by / --invoked-via overrides apply uniformly across
// one-shot Emit and long-running Tx paths.
//
// Outcome cascade (highest priority first):
//   - explicit `*outcome != ""` from a prior tx.SetOutcome wins
//   - recovered panic -> internal_error
//   - ctx.Err() != nil -> user_interrupt
//   - fallthrough -> success
//
// See doc.go for the End() defer-direct contract.
func (c *Client) stampInvocationBase(
	command *CommandName,
	durationMs *RawDuration,
	outcome *Outcome,
	invokedBy *InvokedBy,
	invokedVia *InvokedVia,
	name CommandName,
	ctx context.Context,
	recovered any,
) {
	*command = name
	*durationMs = RawDuration(time.Since(c.startTime).Milliseconds())
	if *invokedBy == "" {
		*invokedBy = c.defaultInvokedBy(name)
	}
	if *invokedVia == "" {
		*invokedVia = c.defaultInvokedVia(name)
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
