package cliout

import (
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// Manifest is the JSON shape returned by both `gaffer manifest` and the
// MCP get_manifest tool. Typed so the SDK can derive an output schema
// from it (rather than untyped map[string]any).
type Manifest struct {
	Version  string                     `json:"version"`
	Commands map[string]ManifestCommand `json:"commands"`
}

type ManifestCommand struct {
	Flags []string `json:"flags"`
}

// BuildManifest walks the cobra command tree and produces the manifest.
// `manifest`, `help`, `version`, `completion`, and `man` are excluded -
// they're meta and tooling commands that don't represent gaffer
// capabilities. version and manifest are exposed via separate MCP tools
// (get_version, get_manifest) rather than appearing in their own output.
func BuildManifest(root *cobra.Command, version string) Manifest {
	commands := map[string]ManifestCommand{}

	for _, child := range root.Commands() {
		name := child.Name()
		if name == "manifest" || name == "help" || name == "version" || name == "completion" || name == "man" {
			continue
		}

		flags := []string{}
		child.Flags().VisitAll(func(f *pflag.Flag) {
			flags = append(flags, f.Name)
		})

		commands[name] = ManifestCommand{Flags: flags}
	}

	return Manifest{Version: version, Commands: commands}
}
