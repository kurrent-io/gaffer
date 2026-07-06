package drift

import (
	"context"
	"fmt"

	"github.com/kurrent-io/gaffer/cli/internal/config"
	"github.com/kurrent-io/gaffer/cli/internal/deploy"
	"github.com/kurrent-io/gaffer/cli/internal/envvar"
	"github.com/kurrent-io/gaffer/cli/internal/remote"
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

// StartConfigDriftCheck fetches the node's live projection options in the
// background, so the HTTP round-trip overlaps the command's own RPCs; drain
// the channel once the main output is ready. It carries the divergences - nil
// when [database_config] isn't declared or on any fetch failure (unreachable
// HTTP surface, auth refusal, older server). Advisory only: it never fails
// the command.
func StartConfigDriftCheck(ctx context.Context, cfg *config.Config, root, envName, connection string) <-chan []ConfigDrift {
	ch := make(chan []ConfigDrift, 1)
	if cfg == nil || cfg.DatabaseConfig == nil || connection == "" {
		ch <- nil
		return ch
	}
	go func() {
		defer close(ch)
		// The connection string may keep credentials in ${VAR}s; expand with the
		// same env overlay the connect used.
		overlay, err := envvar.Overlay(root, envName)
		if err != nil {
			ch <- nil
			return
		}
		conn, err := envvar.Expand(connection, overlay)
		if err != nil {
			ch <- nil
			return
		}
		fctx, cancel := context.WithTimeout(ctx, deploy.RPCTimeout)
		defer cancel()
		node, err := remote.FetchNodeOptions(fctx, conn)
		if err != nil {
			ch <- nil
			return
		}
		ch <- ConfigDriftItems(cfg.DatabaseConfig, node)
	}()
	return ch
}
