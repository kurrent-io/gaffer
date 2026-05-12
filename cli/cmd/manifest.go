package cmd

import (
	"encoding/json"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
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
		RunE: func(cmd *cobra.Command, args []string) error {
			m := manifest{
				Version:  Version,
				Commands: buildCommandManifest(cmd.Root()),
			}

			enc := json.NewEncoder(os.Stdout)
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
