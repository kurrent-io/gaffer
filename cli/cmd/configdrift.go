package cmd

import (
	"context"
	"fmt"
	"io"

	"github.com/kurrent-io/gaffer/cli/internal/config"
	"github.com/kurrent-io/gaffer/cli/internal/envvar"
	"github.com/kurrent-io/gaffer/cli/internal/remote"
)

// configDrift is one [database_config] knob whose live value on the target
// node diverges from the declared one - the fixtures and local runs assumed a
// different engine configuration than the server enforces.
type configDrift struct {
	Knob   string
	Server int64
	Local  int64
	unit   string // display unit: "ms" (joined) or "bytes" (spaced)
}

// text is the human warning line for one divergence.
func (d configDrift) text() string {
	sep := ""
	if d.unit != "ms" {
		sep = " "
	}
	return fmt.Sprintf("%s is %d%s%s on the server, %d%s%s in gaffer.toml",
		d.Knob, d.Server, sep, d.unit, d.Local, sep, d.unit)
}

// configDriftItems compares the declared [database_config] knobs against the
// node's live values: one item per knob that is both declared locally and
// reported by the server with a different value. Undeclared knobs and options
// an older server doesn't report are silently fine.
func configDriftItems(dc *config.DatabaseConfig, node *remote.NodeProjectionOptions) []configDrift {
	if dc == nil || node == nil {
		return nil
	}
	var out []configDrift
	check := func(knob string, local int64, server *int64, unit string) {
		if server != nil && *server != local {
			out = append(out, configDrift{Knob: knob, Server: *server, Local: local, unit: unit})
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

// startConfigDriftCheck fetches the node's live projection options in the
// background, so the HTTP round-trip overlaps the command's own RPCs; drain
// the channel once the main output is ready. It carries the divergences - nil
// when [database_config] isn't declared or on any fetch failure (unreachable
// HTTP surface, auth refusal, older server). Advisory only: it never fails
// the command.
func startConfigDriftCheck(ctx context.Context, cfg *config.Config, root, envName, connection string) <-chan []configDrift {
	ch := make(chan []configDrift, 1)
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
		fctx, cancel := context.WithTimeout(ctx, projectionRPCTimeout)
		defer cancel()
		node, err := remote.FetchNodeOptions(fctx, conn)
		if err != nil {
			ch <- nil
			return
		}
		ch <- configDriftItems(cfg.DatabaseConfig, node)
	}()
	return ch
}

// writeConfigDriftWarnings prints one warning line per divergence, or nothing.
func writeConfigDriftWarnings(out io.Writer, items []configDrift) {
	if len(items) == 0 {
		return
	}
	tw := newTextWriter(out, out)
	for _, d := range items {
		tw.write("%s\n", tw.styles.warning.Render("⚠ target config drift: "+d.text()))
	}
}

// configDriftJSON is one divergence in machine output: the gaffer.toml knob
// name with the server's and the local declared values, in the knob's native
// unit (milliseconds for the timeouts, bytes for max_state_size).
type configDriftJSON struct {
	Knob   string `json:"knob"`
	Server int64  `json:"server"`
	Local  int64  `json:"local"`
}

func configDriftToJSON(items []configDrift) []configDriftJSON {
	out := make([]configDriftJSON, 0, len(items))
	for _, d := range items {
		out = append(out, configDriftJSON{Knob: d.Knob, Server: d.Server, Local: d.Local})
	}
	return out
}
