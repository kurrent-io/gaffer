package cmd

import (
	"encoding/json"
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
		Use:   "info [projection]",
		Short: "Show projection details",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) (retErr error) {
			defer oneShotDefer(&retErr, func(o telemetry.Outcome) {
				telemetry.EmitInfo(cmd.Context(), telemetry.InfoCommandInvokedProperties{Outcome: o})
			})
			return runInfo(cmd, args[0], asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	return cmd
}

func runInfo(cmd *cobra.Command, name string, asJSON bool) error {
	proj, err := engine.LoadProjection(name)
	if err != nil {
		return err
	}

	session, info, err := engine.CreateSession(proj, false, false)
	if err != nil {
		return handleSessionError(cmd, err)
	}
	defer session.Destroy()

	if asJSON {
		return writeInfoJSON(proj, info)
	}

	tw := newTextWriter(os.Stdout, os.Stderr)
	tw.WriteInfo(proj.Def.Name, info, proj.EngineVersion, proj.DbVersion)
	return nil
}

func writeInfoJSON(proj *engine.Projection, info gafferruntime.ProjectionInfo) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(cliout.BuildInfoJSON(proj, info))
}
