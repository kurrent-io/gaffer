package cmd

import (
	"encoding/json"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

var manifestCmd = &cobra.Command{
	Use:   "manifest",
	Short: "Print CLI capabilities as JSON",
	RunE:  runManifest,
}

type manifest struct {
	Version  string                     `json:"version"`
	Commands map[string]manifestCommand `json:"commands"`
}

type manifestCommand struct {
	Flags []string `json:"flags"`
}

func runManifest(cmd *cobra.Command, args []string) error {
	m := manifest{
		Version:  version,
		Commands: buildCommandManifest(),
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(m)
}

func buildCommandManifest() map[string]manifestCommand {
	commands := map[string]manifestCommand{}

	for _, child := range rootCmd.Commands() {
		name := child.Name()
		if name == "manifest" || name == "help" || name == "version" || name == "completion" {
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
