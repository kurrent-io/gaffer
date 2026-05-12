package cmd

import (
	"encoding/json"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

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
			defer func() {
				// JSON encode failures are gaffer-side, not user-side -
				// the only inputs are the cobra tree shape. Map non-nil
				// to internal_error so the dataset reflects that.
				outcome := telemetry.OutcomeSuccess
				if retErr != nil {
					outcome = telemetry.OutcomeInternalError
				}
				telemetry.EmitManifest(cmd.Context(), telemetry.ManifestCommandInvokedProperties{Outcome: outcome})
			}()

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
