package cmd

import (
	"io"

	"github.com/kurrent-io/gaffer/cli/internal/drift"
)

// writeConfigDriftWarnings prints one warning line per divergence, or nothing.
func writeConfigDriftWarnings(out io.Writer, items []drift.ConfigDrift) {
	if len(items) == 0 {
		return
	}
	tw := newTextWriter(out, out)
	for _, d := range items {
		tw.write("%s\n", tw.styles.warning.Render("⚠ target config drift: "+d.Text()))
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

func configDriftToJSON(items []drift.ConfigDrift) []configDriftJSON {
	out := make([]configDriftJSON, 0, len(items))
	for _, d := range items {
		out = append(out, configDriftJSON{Knob: d.Knob, Server: d.Server, Local: d.Local})
	}
	return out
}
