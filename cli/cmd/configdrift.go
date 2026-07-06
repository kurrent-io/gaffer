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
