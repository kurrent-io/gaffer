package cmd

import (
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "gaffer",
	Short: "Projection toolkit for KurrentDB",
	Long:  "Develop, test, debug, and deploy KurrentDB projections.",
}

func init() {
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(initCmd)
	rootCmd.AddCommand(scaffoldCmd)
	rootCmd.AddCommand(devCmd)
}

// Execute runs the root command.
func Execute() error {
	return rootCmd.Execute()
}
