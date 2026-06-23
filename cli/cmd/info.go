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

// renderInvalidInfo reports an invalid projection in the degraded style shared
// with diff/status: the name and the reason, exit 0. With --json it emits a
// minimal object carrying the error.
func renderInvalidInfo(name string, reason error, asJSON bool) error {
	if asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(map[string]any{"name": name, "error": reason.Error()})
	}
	tw := newTextWriter(os.Stdout, os.Stderr)
	tw.heading(name)
	tw.writeInvalidBody(reason)
	return nil
}

func writeInfoJSON(proj *engine.Projection, info gafferruntime.ProjectionInfo) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(cliout.BuildInfoJSON(proj, info))
}
