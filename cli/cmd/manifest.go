package cmd

import (
	"encoding/json"

	"github.com/spf13/cobra"

	"github.com/kurrent-io/gaffer/cli/internal/cliout"
	"github.com/kurrent-io/gaffer/cli/internal/config"
	"github.com/kurrent-io/gaffer/cli/internal/telemetry"
)

func newManifestCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "manifest",
		Short: "Print CLI capabilities as JSON",
		RunE: func(cmd *cobra.Command, args []string) (retErr error) {
			// Best-effort load: failures (no project, parse error)
			// are discarded - the user only asked for the CLI
			// capability listing, so leaving the manifest-derived
			// telemetry props absent is the right behaviour.
			projectCfg, _ := config.LoadFromCwd()

			defer oneShotDefer(&retErr, func(o telemetry.Outcome) {
				// JSON encode failures are gaffer-side, not user-side;
				// override the classifier's user_error to
				// internal_error for that path.
				if o == telemetry.OutcomeUserError {
					o = telemetry.OutcomeInternalError
				}
				props := telemetry.ManifestCommandInvokedProperties{Outcome: o}
				if projectCfg != nil {
					props.ManifestFeaturesUsed = telemetry.ManifestFeaturesOf(projectCfg)
					pc := telemetry.RawCount(projectCfg.ProjectionCount())
					fc := telemetry.RawCount(projectCfg.FixtureCount())
					props.ProjectionCount = &pc
					props.FixtureCount = &fc
				}
				telemetry.EmitManifest(cmd.Context(), props)
			})

			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetIndent("", "  ")
			return enc.Encode(cliout.BuildManifest(cmd.Root(), Version))
		},
	}
}
