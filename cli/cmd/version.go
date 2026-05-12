package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/kurrent-io/gaffer/cli/internal/telemetry"
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
		RunE: func(cmd *cobra.Command, args []string) (retErr error) {
			defer func() {
				telemetry.EmitVersion(cmd.Context(), telemetry.VersionCommandInvokedProperties{
					Outcome: outcomeFor(retErr),
				})
			}()
			fmt.Println(Version)
			return nil
		},
	}
}
