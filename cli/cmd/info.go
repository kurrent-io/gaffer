package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	gafferruntime "github.com/kurrent-io/gaffer/bindings/go"
	"github.com/kurrent-io/gaffer/cli/internal/cliout"
	"github.com/kurrent-io/gaffer/cli/internal/engine"
	"github.com/kurrent-io/gaffer/cli/internal/telemetry"
)

func newInfoCmd() *cobra.Command {
	var asJSON bool

	cmd := &cobra.Command{
		Use:     "info <projection>",
		Short:   "Show projection details",
		Example: "gaffer info order-count",
		Args:    exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) (retErr error) {
			defer oneShotDefer(&retErr, func(o telemetry.Outcome) {
				telemetry.EmitInfo(cmd.Context(), telemetry.InfoCommandInvokedProperties{Outcome: o})
			})
			return runInfo(args[0], asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	return cmd
}

func runInfo(name string, asJSON bool) error {
	cfg, root, err := loadProject()
	if err != nil {
		return err
	}
	def := cfg.FindProjection(name)
	if def == nil {
		return fmt.Errorf("projection %q not found in gaffer.toml", name)
	}

	// An invalid projection degrades to its name + the reason - the same
	// presentation as diff/status - rather than a hard error. info is local, so the
	// name and reason are still useful. A per-projection config error is caught
	// before compiling; a compile failure after.
	if cfgErr := cfg.ProjectionConfigError(name); cfgErr != nil {
		return renderInvalidInfo(name, cfgErr, asJSON)
	}
	source, err := engine.ReadSource(root, def.Entry)
	if err != nil {
		return err
	}
	proj := engine.NewProjection(root, cfg, def, source)
	session, info, err := engine.CreateSession(proj, false, false)
	if err != nil {
		return renderInvalidInfo(name, err, asJSON)
	}
	defer session.Destroy()

	if asJSON {
		return writeInfoJSON(proj, info)
	}

	tw := newTextWriter(os.Stdout, os.Stderr)
	tw.WriteInfo(proj, info)
	return nil
}

// renderInvalidInfo reports an invalid projection. In text mode it shows the
// degraded body shared with diff (the projection name + the reason), then returns
// a silent error so the exit is non-zero - info is a single-projection command
// that couldn't do its job - without fang re-printing what we already rendered.
// The non-zero return also classifies the telemetry outcome as a user error
// rather than success. In --json mode it fails cleanly (non-zero, error on
// stderr) rather than emitting a partial object whose shape differs from valid
// info output.
func renderInvalidInfo(name string, reason error, asJSON bool) error {
	if asJSON {
		return reason
	}
	tw := newTextWriter(os.Stdout, os.Stderr)
	tw.heading(name)
	tw.writeInvalidBody(reason)
	return silent(reason)
}

func writeInfoJSON(proj *engine.Projection, info gafferruntime.ProjectionInfo) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(cliout.BuildInfoJSON(proj, info))
}
