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

// devBuild marks a build that was not produced by the release pipeline:
// a bare `go build`, `just cli build`, or any local compile. It is the
// signal the update-check uses to stay quiet, set explicitly via
// ldflags rather than inferred from Version's `-dev` suffix - a real
// published pre-release (e.g. `0.4.0-rc.1`) is a genuine release whose
// users should still be told about newer versions, so its build leaves
// devBuild "false". `build-release` overrides it to "false"; every
// other path keeps the "true" default.
var devBuild = "true"

// IsDevBuild reports whether this binary is a local/dev build (see
// devBuild). main passes it to the update-check so source builds aren't
// nagged to `npm install` over themselves.
func IsDevBuild() bool { return devBuild == "true" }

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the gaffer version",
		RunE: func(cmd *cobra.Command, args []string) (retErr error) {
			defer oneShotDefer(&retErr, func(o telemetry.Outcome) {
				telemetry.EmitVersion(cmd.Context(), telemetry.VersionCommandInvokedProperties{Outcome: o})
			})
			fmt.Fprintln(cmd.OutOrStdout(), Version)
			return nil
		},
	}
}
