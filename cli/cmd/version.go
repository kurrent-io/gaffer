package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/kurrent-io/gaffer/cli/internal/telemetry"
)

// Version is the gaffer release string, set at build time via ldflags
// (`-X github.com/kurrent-io/gaffer/cli/cmd.Version=...`). Both build
// recipes stamp it: `just cli build` with `<package.json version>-dev`,
// `build-release` with the release version. This default only shows
// through on a bare `go build` with no ldflags. Exported so main.go can
// stamp it onto the telemetry User-Agent header without re-importing a
// build-info package.
var Version = "0.0.0-dev"

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the gaffer version",
		RunE: func(cmd *cobra.Command, args []string) (retErr error) {
			defer oneShotDefer(&retErr, func(o telemetry.Outcome) {
				telemetry.EmitVersion(cmd.Context(), telemetry.VersionCommandInvokedProperties{Outcome: o})
			})
			fmt.Println(Version)
			return nil
		},
	}
}
