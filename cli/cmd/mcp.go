package cmd

import (
	"github.com/spf13/cobra"

	"github.com/kurrent-io/gaffer/cli/internal/cliout"
	"github.com/kurrent-io/gaffer/cli/internal/mcpserver"
	"github.com/kurrent-io/gaffer/cli/internal/telemetry"
)

func newMCPCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "mcp",
		Short: "Start an MCP server for AI agent integration",
		RunE: func(cmd *cobra.Command, args []string) error {
			// `defer tx.End(ctx)` must be direct - see DevTx.End
			// for why recover() under a wrapping closure silently
			// drops body panics.
			tx := telemetry.BeginMCP(cmd.Context())
			defer tx.End(cmd.Context())

			srv, err := mcpserver.NewFromProjectRoot(Version, cliout.BuildManifest(cmd.Root(), Version))
			if err != nil {
				// Classify the project-load failure (no project /
				// parse / validation) so the outcome is specific
				// rather than a generic user_error.
				tx.SetOutcome(outcomeFor(err))
				return err
			}

			// Stamp manifest-derived props before the long Run blocks,
			// not at End time, so the values are recorded even if the
			// session terminates unexpectedly. Schema gives mcp only
			// features_used (no counts).
			if cfg := srv.Config(); cfg != nil {
				tx.SetManifestFeaturesUsed(telemetry.ManifestFeaturesOf(cfg))
			}

			runErr := srv.Run(cmd.Context())

			// Drain counters after Run returns - request goroutines
			// have finished mutating stats by then, so the Load
			// reads the final value. Tx setters are
			// single-goroutine-owned; this drain is on the main
			// goroutine, satisfying that contract.
			stats := srv.Stats()
			tx.SetToolCallCount(stats.ToolCallCount)
			tx.SetResourceReadCount(stats.ResourceReadCount)

			// Drain projection faults observed across tool calls
			// into projection_errors_seen. classifyOutcome handles
			// the picking-a-final-bucket logic below.
			tracker := newProjErrTracker()
			for _, projErr := range srv.ProjectionErrors() {
				tracker.Record(projErr)
			}
			if seen := tracker.Sorted(); len(seen) > 0 {
				tx.SetProjectionErrorsSeen(seen)
			}

			tx.SetOutcome(classifyMCPOutcome(runErr, tracker))
			return runErr
		},
	}
}
