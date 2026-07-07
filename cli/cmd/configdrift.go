package cmd

import (
	"io"

	"github.com/kurrent-io/gaffer/cli/internal/drift"
)

// writeConfigDriftWarnings prints one warning line per divergence, a single
// line when the check couldn't read the node's config (a failed read must
// not look identical to "in sync" - UI-1820), or nothing.
func writeConfigDriftWarnings(out io.Writer, dr drift.ConfigDriftResult) {
	if dr.Err != nil {
		tw := newTextWriter(out, out)
		tw.write("%s\n", tw.styles.warning.Render("⚠ could not check [database_config] drift: "+dr.Err.Error()))
		return
	}
	if len(dr.Items) == 0 {
		return
	}
	tw := newTextWriter(out, out)
	for _, d := range dr.Items {
		tw.write("%s\n", tw.styles.warning.Render("⚠ target config drift: "+d.Text()))
	}
}
