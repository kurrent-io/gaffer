package cmd

import (
	"encoding/json"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/kurrent-io/gaffer/cli/internal/config"
	"github.com/kurrent-io/gaffer/cli/internal/telemetry"
)

type manifest struct {
	Version  string                     `json:"version"`
	Commands map[string]manifestCommand `json:"commands"`
}

type manifestCommand struct {
	Flags []string `json:"flags"`
}

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

			m := manifest{
				Version:  Version,
				Commands: buildCommandManifest(cmd.Root()),
			}

			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetIndent("", "  ")
			return enc.Encode(m)
		},
	}
}

func buildCommandManifest(root *cobra.Command) map[string]manifestCommand {
	commands := map[string]manifestCommand{}

	for _, child := range root.Commands() {
		name := child.Name()
		if name == "manifest" || name == "help" || name == "version" || name == "completion" || name == "man" {
			continue
		}

		flags := []string{}
		child.Flags().VisitAll(func(f *pflag.Flag) {
			flags = append(flags, f.Name)
		})

		commands[name] = manifestCommand{Flags: flags}
	}

	return commands
}
