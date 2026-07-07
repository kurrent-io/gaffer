package drift

import (
	"context"
	"fmt"

	"github.com/kurrent-io/gaffer/cli/internal/config"
	"github.com/kurrent-io/gaffer/cli/internal/deploy"
	"github.com/kurrent-io/gaffer/cli/internal/remote"
	"github.com/kurrent-io/gaffer/cli/internal/target"
)

// ConfigDrift is one [database_config] knob whose live value on the target
// node diverges from the declared one - the fixtures and local runs assumed a
// different engine configuration than the server enforces.
type ConfigDrift struct {
	Knob   string
	Server int64
	Local  int64
	Unit   string // display unit: "ms" (joined) or "bytes" (spaced)
}

// Text is the human warning line for one divergence.
func (d ConfigDrift) Text() string {
	sep := ""
	if d.Unit != "ms" {
		sep = " "
	}
	return fmt.Sprintf("%s is %d%s%s on the server, %d%s%s in gaffer.toml",
		d.Knob, d.Server, sep, d.Unit, d.Local, sep, d.Unit)
}

// ConfigDriftItems compares the declared [database_config] knobs against the
// node's live values: one item per knob that is both declared locally and
// reported by the server with a different value. Undeclared knobs and options
// an older server doesn't report are silently fine.
func ConfigDriftItems(dc *config.DatabaseConfig, node *remote.NodeProjectionOptions) []ConfigDrift {
	if dc == nil || node == nil {
		return nil
	}
	var out []ConfigDrift
	check := func(knob string, local int64, server *int64, unit string) {
		if server != nil && *server != local {
			out = append(out, ConfigDrift{Knob: knob, Server: *server, Local: local, Unit: unit})
		}
	}
	if dc.CompilationTimeout != nil {
		check("compilation_timeout", int64(*dc.CompilationTimeout), node.CompilationTimeoutMs, "ms")
	}
	if dc.ExecutionTimeout != nil {
		check("execution_timeout", int64(*dc.ExecutionTimeout), node.ExecutionTimeoutMs, "ms")
	}
	// A non-positive max_state_size means "use the engine default" (see
	// config.DatabaseConfig), so it declares nothing to compare against.
	if dc.MaxStateSize != nil && *dc.MaxStateSize > 0 {
		check("max_state_size", *dc.MaxStateSize, node.MaxStateSizeBytes, "bytes")
	}
	return out
}

// ConfigDriftResult is the advisory check's outcome: the divergent knobs, or
// the reason the node's config couldn't be read. Items and Err are mutually
// exclusive; both empty means nothing was declared to compare, or the check
// ran and found no drift. Err exists because a failed read must not look
// identical to "in sync" (UI-1820) - callers surface it as a warning, never
// as a command failure.
type ConfigDriftResult struct {
	Items []ConfigDrift
	Err   error
}

// StartConfigDriftCheck fetches the node's live projection options in the
// background, so the HTTP round-trip overlaps the command's own RPCs; drain
// the channel once the main output is ready. The zero result means
// [database_config] isn't declared (nothing to compare). Credentials resolve
// exactly like the main connection's (env overlay + Credentials, env creds
// over userinfo), so a .env-supplied login reaches the node's HTTP surface
// too. Advisory only: failures are carried on the result, never fail the
// command.
func StartConfigDriftCheck(ctx context.Context, cfg *config.Config, root string, env config.ResolvedEnv) <-chan ConfigDriftResult {
	ch := make(chan ConfigDriftResult, 1)
	if cfg == nil || cfg.DatabaseConfig == nil || env.Connection == "" {
		ch <- ConfigDriftResult{}
		close(ch)
		return ch
	}
	go func() {
		defer close(ch)
		// target.Resolve owns the credential stack, and never touches the
		// process environment - safe on this goroutine, which runs beside
		// the rest of the command (in the MCP server, beside live cgo
		// sessions reading environ). The base .env was loaded by the connect
		// that precedes this check.
		tgt, err := target.Resolve(root, env)
		if err != nil {
			ch <- ConfigDriftResult{Err: err}
			return
		}
		fctx, cancel := context.WithTimeout(ctx, deploy.RPCTimeout)
		defer cancel()
		node, err := remote.FetchNodeOptions(fctx, tgt)
		if err != nil {
			ch <- ConfigDriftResult{Err: fmt.Errorf("reading node options: %w", err)}
			return
		}
		ch <- ConfigDriftResult{Items: ConfigDriftItems(cfg.DatabaseConfig, node)}
	}()
	return ch
}
