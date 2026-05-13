package telemetry

import "strings"

// Invocation carries the three hidden-flag values that link a spawned
// CLI back to its launcher. Set on the Client at startup from main.go's
// argv peek, then surfaced on every envelope: invokerID stamps
// Context.InvokerID; invokedBy / invokedVia override the default
// (direct, terminal) on each command_invoked variant.
//
// The zero value is meaningful: every empty field falls through to a
// command-aware default in the stamp helpers. Callers don't need to
// populate this when running as a top-level terminal user.
type Invocation struct {
	InvokerID  UUID
	InvokedBy  InvokedBy
	InvokedVia InvokedVia
}

// IsZero reports whether every spawn-linkage field is empty - i.e.
// no invocation flag was passed. StartupGate keys the first-mint
// notice suppression on this: any non-empty field signals a spawner
// has already disclosed telemetry to the user, so a second notice
// printed to a non-TTY stderr would be invisible and redundant.
func (i Invocation) IsZero() bool {
	return i.InvokerID == "" && i.InvokedBy == "" && i.InvokedVia == ""
}

// IsConfigCommand reports whether args looks like a `gaffer config
// ...` invocation. main.go uses this to skip Client construction
// for the whole `config` subtree: these are management commands
// that own identity / opt-out lifecycle themselves, and the
// pre-cobra StartupGate path would otherwise fire the first-mint
// notice before the user-facing flag has been parsed. `config`
// commands don't emit command_invoked of their own, so skipping
// the Client costs nothing.
//
// Scanning is positional: returns true when "config" is the first
// non-flag token. Bare forms of the value-taking root flags
// (--invoker-id / --invoked-by / --invoked-via) consume the next
// token so an argv like `--invoker-id config version` doesn't trip
// the check on the flag's value. `--` ends flag scanning.
func IsConfigCommand(args []string) bool {
	valueFlags := map[string]bool{
		"--invoker-id":  true,
		"--invoked-by":  true,
		"--invoked-via": true,
	}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			if i+1 < len(args) {
				return args[i+1] == "config"
			}
			return false
		}
		if strings.HasPrefix(a, "-") {
			if valueFlags[a] && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				i++
			}
			continue
		}
		return a == "config"
	}
	return false
}

// PeekInvocationFlags scans args for --invoker-id / --invoked-by /
// --invoked-via and returns whatever it finds. Used by main.go before
// cobra parses, so the resolved values can be baked into the Client at
// construction (cobra also declares the flags as hidden persistent so
// it doesn't reject them as unknown).
//
// Supports both `--flag value` and `--flag=value` forms; later
// occurrences win. Scanning stops at the first bare `--` (cobra's
// end-of-flags marker) so positional args that happen to look like
// flags aren't slurped. Bare-form values that themselves start with
// `--` are rejected (treated as the next flag rather than this
// flag's value) so `--invoker-id --invoked-by=x` leaves InvokerID
// empty rather than capturing the next flag.
//
// Values are not validated against the schema enum sets - the worker
// rejects bad envelopes. Pre-commit validation would have to log
// somewhere and there's no good surface to log to from this layer.
func PeekInvocationFlags(args []string) Invocation {
	var inv Invocation
	consume := func(i int, name string) (string, int, bool) {
		a := args[i]
		eq := name + "="
		switch {
		case a == name:
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "--") {
				return args[i+1], i + 1, true
			}
			return "", i, true
		case strings.HasPrefix(a, eq):
			return strings.TrimPrefix(a, eq), i, true
		}
		return "", i, false
	}
	for i := 0; i < len(args); i++ {
		if args[i] == "--" {
			break
		}
		if v, ni, ok := consume(i, "--invoker-id"); ok {
			inv.InvokerID = v
			i = ni
			continue
		}
		if v, ni, ok := consume(i, "--invoked-by"); ok {
			inv.InvokedBy = InvokedBy(v)
			i = ni
			continue
		}
		if v, ni, ok := consume(i, "--invoked-via"); ok {
			inv.InvokedVia = InvokedVia(v)
			i = ni
			continue
		}
	}
	return inv
}

// defaultInvokedBy returns the InvokedBy value to stamp when a Tx
// setter or variant struct literal left it empty. Resolution order:
// explicit --invoked-by flag (any command), then command-aware
// default. mcp defaults to mcp_client because the command is only
// ever launched by an external MCP host (Claude, Cursor, ...) -
// terminal users running `gaffer mcp` directly are rare and we'd
// rather optimise for the dominant case.
func (c *Client) defaultInvokedBy(name CommandName) InvokedBy {
	if c.invocation.InvokedBy != "" {
		return c.invocation.InvokedBy
	}
	if name == CommandNameMCP {
		return InvokedByMCPClient
	}
	return InvokedByDirect
}

// defaultInvokedVia mirrors defaultInvokedBy: explicit flag wins,
// else mcp -> stdio, else terminal.
func (c *Client) defaultInvokedVia(name CommandName) InvokedVia {
	if c.invocation.InvokedVia != "" {
		return c.invocation.InvokedVia
	}
	if name == CommandNameMCP {
		return InvokedViaStdio
	}
	return InvokedViaTerminal
}
