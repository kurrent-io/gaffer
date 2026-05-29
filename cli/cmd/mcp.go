package cmd

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/kurrent-io/gaffer/cli/internal/mcpserver"
	"github.com/kurrent-io/gaffer/cli/internal/telemetry"
)

// envProject pins the MCP server's project root, mirroring the
// --project flag. The flag takes precedence when both are set.
const envProject = "GAFFER_PROJECT"

func newMCPCmd() *cobra.Command {
	var projectDir string

	cmd := &cobra.Command{
		Use:         "mcp",
		Short:       "Start an MCP server for AI agent integration",
		Annotations: map[string]string{AnnotationOutput: OutputStructured},
		RunE: func(cmd *cobra.Command, args []string) error {
			// `defer tx.End(ctx)` must be direct - see DevTx.End
			// for why recover() under a wrapping closure silently
			// drops body panics.
			tx := telemetry.BeginMCP(cmd.Context())
			defer tx.End(cmd.Context())

			projectOverride := projectDir
			if projectOverride == "" {
				projectOverride = os.Getenv(envProject)
			}

			srv, err := mcpserver.NewFromProjectRoot(Version, projectOverride)
			if err != nil {
				// Classify the project-load failure (no project /
				// parse / validation) so the outcome is specific
				// rather than a generic user_error.
				tx.SetOutcome(outcomeFor(err))
				return err
			}

			// Whether the server launched inside a project. Known at
			// construction and immutable, so stamp it now.
			tx.SetStartedInProject(srv.StartedInProject())

			// Stamp manifest-derived props before the long Run blocks,
			// not at End time, so the values are recorded even if the
			// session terminates unexpectedly.
			if cfg := srv.Config(); cfg != nil {
				tx.SetManifestFeaturesUsed(telemetry.ManifestFeaturesOf(cfg))
			}

			runErr := srv.Run(cmd.Context())

			// A server that started project-less resolves its project
			// lazily on first tool use, so Config() was nil above. Re-stamp
			// after Run (handlers have finished) to capture manifest
			// features for those sessions. Idempotent for in-project starts.
			if cfg := srv.Config(); cfg != nil {
				tx.SetManifestFeaturesUsed(telemetry.ManifestFeaturesOf(cfg))
			}

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

	cmd.Flags().StringVar(&projectDir, "project", "",
		"Project directory to use instead of searching from the working directory (also set via "+envProject+")")

	return cmd
}
