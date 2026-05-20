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
		// Hidden from `gaffer --help` because the audience is editor
		// extensions and other wrappers feature-gating their UI
		// against a specific gaffer build, not interactive users.
		Hidden: true,
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
			return enc.Encode(buildManifest(cmd.Root(), Version))
		},
	}
}

func buildManifest(root *cobra.Command, version string) manifest {
	commands := map[string]manifestCommand{}
	collectCommands(root, "", commands)
	return manifest{Version: version, Commands: commands}
}

// collectCommands walks the cobra tree and emits one entry per runnable
// command, keyed by its full path ("config telemetry status"). Non-runnable
// group commands (e.g. `config`, `config telemetry`) are traversed but not
// emitted - the manifest lists invocable commands, not navigation nodes.
func collectCommands(parent *cobra.Command, prefix string, out map[string]manifestCommand) {
	for _, child := range parent.Commands() {
		name := child.Name()
		if name == "manifest" || name == "help" || name == "version" || name == "completion" || name == "man" {
			continue
		}

		key := name
		if prefix != "" {
			key = prefix + " " + name
		}

		if child.Runnable() {
			flags := []string{}
			child.Flags().VisitAll(func(f *pflag.Flag) {
				flags = append(flags, f.Name)
			})
			out[key] = manifestCommand{Flags: flags}
		}

		collectCommands(child, key, out)
	}
}
