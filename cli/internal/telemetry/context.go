package telemetry

import "context"

// clientCtxKey is the unexported key under which the per-process
// Client is stashed on the root context. Unexported so callers go
// through WithClient / ClientFromContext and can't collide with
// another package's ctx-value usage.
type clientCtxKey struct{}

// WithClient returns a copy of ctx carrying c. Call once in main
// after telemetry.New(...); subcommands read it back via
// ClientFromContext.
//
// Storing nil is allowed and yields a ctx that ClientFromContext
// reports as "no client" - useful for tests that want to bypass
// telemetry without rebuilding the ctx chain.
func WithClient(ctx context.Context, c *Client) context.Context {
	return context.WithValue(ctx, clientCtxKey{}, c)
}

// ClientFromContext returns the Client previously stashed by
// WithClient, or nil if none. A nil return is the canonical "no
// telemetry for this run" signal - main.go skips WithClient
// entirely when StartupGate returns nil (opt-out active, config
// unreadable, identity not minted).
func ClientFromContext(ctx context.Context) *Client {
	c, _ := ctx.Value(clientCtxKey{}).(*Client)
	return c
}

// ShouldIncludeShape is the canonical predicate for "should the
// FFI walk the projection AST for shape telemetry?". Returns true
// iff a Client is on ctx (i.e. telemetry isn't opted out). Long-
// running commands that create a ProjectionSession (dev / mcp /
// lsp) call this at each session-creation site and pass the
// result to engine.CreateSession; one-shot or non-telemetry
// commands pass false directly. Centralised so the predicate
// stays in one place when it evolves (sampling, per-command
// opt-out, etc).
func ShouldIncludeShape(ctx context.Context) bool {
	return ClientFromContext(ctx) != nil
}
