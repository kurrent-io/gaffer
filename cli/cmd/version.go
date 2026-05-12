package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Version is the gaffer release string, set at build time via
// ldflags (`-X github.com/kurrent-io/gaffer/cli/cmd.Version=...`).
// Exported so main.go can stamp it onto the telemetry User-Agent
// header without re-importing a build-info package.
var Version = "0.0.1-dev"

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the gaffer version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println(Version)
		},
	}
}
