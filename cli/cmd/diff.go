package cmd

import (
	"context"
	"encoding/json"
	"io"
	"time"

	"github.com/spf13/cobra"
)

type diffOpts struct {
	Env        string
	Connection string
	JSON       bool
}

func newDiffCmd() *cobra.Command {
	var opts diffOpts
	cmd := &cobra.Command{
		Use:   "diff <projection>",
		Short: "Show how a projection differs from what's deployed",
		Long: "Compare a projection's local definition against what's deployed on KurrentDB.\n\n" +
			"Reports one of four states: in sync, drifted, not deployed (local only), or " +
			"untracked (on the server but absent from gaffer.toml). When the query differs, " +
			"the source is shown in an external diff viewer (git diff --no-index by default; " +
			"set GAFFER_EXTERNAL_DIFF to override). Pass --json for machine-readable output.",
		Example: "  gaffer diff order-count\n" +
			"  gaffer diff order-count --env staging",
		Args: exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDiff(cmd, args[0], opts)
		},
	}
	cmd.Flags().StringVar(&opts.Env, "env", "", "Environment from gaffer.toml to compare against")
	cmd.Flags().StringVar(&opts.Connection, "connection", "", "KurrentDB connection string (overrides --env)")
	cmd.Flags().BoolVar(&opts.JSON, "json", false, "Output as JSON")
	return cmd
}

func runDiff(cmd *cobra.Command, name string, opts diffOpts) error {
	cfg, root, r, cleanup, err := connectEnv(opts.Connection, opts.Env)
	if err != nil {
		return err
	}
	defer cleanup()

	// remote calls block until their context deadline if the projections
	// subsystem doesn't respond, so bound the read rather than hang the command.
	ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
	defer cancel()
	entry, err := compareProjection(ctx, r, cfg, root, name)
	if err != nil {
		return err
	}

	if opts.JSON {
		return renderDiffJSON(cmd.OutOrStdout(), entry)
	}
	newTextWriter(cmd.OutOrStdout(), cmd.ErrOrStderr()).WriteDiff(entry)
	if entry.State == driftDrifted && entry.Cmp.QueryDiffers {
		return openSourceDiff(entry.Name, entry.Deployed.CanonicalQuery(), entry.Local.CanonicalQuery(), cmd.OutOrStdout(), cmd.ErrOrStderr())
	}
	return nil
}

// diffJSON is the --json shape for one projection. State is one of in-sync,
// drifted, not-deployed, untracked.
type diffJSON struct {
	Name         string     `json:"name"`
	State        string     `json:"state"`
	LocalHash    string     `json:"localHash,omitempty"`
	DeployedHash string     `json:"deployedHash,omitempty"`
	Drift        *driftJSON `json:"drift,omitempty"`
}

type driftJSON struct {
	Query               bool `json:"query"`
	EngineVersion       bool `json:"engineVersion"`
	Emit                bool `json:"emit"`
	TrackEmittedStreams bool `json:"trackEmittedStreams"`
}

func renderDiffJSON(w io.Writer, e comparison) error {
	j := diffJSON{Name: e.Name, State: string(e.State)}
	if e.Local != nil {
		j.LocalHash = e.Local.Hash()
	}
	if e.Deployed != nil {
		j.DeployedHash = e.Deployed.Hash()
	}
	if e.State == driftDrifted {
		j.Drift = &driftJSON{
			Query:               e.Cmp.QueryDiffers,
			EngineVersion:       e.Cmp.EngineVersionDiffers,
			Emit:                e.Cmp.EmitDiffers,
			TrackEmittedStreams: e.Cmp.TrackEmittedStreamsDiffers,
		}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(j)
}
