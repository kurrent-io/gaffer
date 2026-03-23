package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Set at build time via ldflags.
var version = "dev"

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the gaffer version",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println(version)
	},
}
