package cmd

import (
	"context"
	"encoding/json"
	"io"

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
			"Reports one of five states: in sync, drifted, not deployed (local only), untracked " +
			"(on the server but absent from gaffer.toml), or invalid. Invalid means the local " +
			"source doesn't compile; the source and config still diff, but emit is unknown.\n\n" +
			"When the query differs, the source is shown in an external diff viewer (git diff " +
			"--no-index by default; set GAFFER_EXTERNAL_DIFF to override). Pass --json for " +
			"machine-readable output.",
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
	ctx, cancel := context.WithTimeout(cmd.Context(), projectionRPCTimeout)
	defer cancel()
	entry, err := compareProjection(ctx, r, cfg, root, name)
	if err != nil {
		return err
	}

	if opts.JSON {
		return renderDiffJSON(cmd.OutOrStdout(), entry)
	}
	newTextWriter(cmd.OutOrStdout(), cmd.ErrOrStderr()).WriteDiff(entry)
	// The query is read from source, not compiled, so the source diff is still
	// worth opening when the local projection is invalid (its whole point is
	// comparing source to what's deployed). Both sides must exist and differ.
	if entry.Cmp.QueryDiffers && entry.Deployed != nil && (entry.State == driftDrifted || entry.State == driftInvalid) {
		return openSourceDiff(entry.Name, entry.Deployed.CanonicalQuery(), entry.Local.CanonicalQuery(), cmd.OutOrStdout(), cmd.ErrOrStderr())
	}
	return nil
}

// diffJSON is the --json shape for one projection. drift is the verdict (one of
// in-sync, drifted, not-deployed, untracked, invalid), matching gaffer status;
// changes names the dimensions that differ, present only when drifted. error is
// the compile failure, present only when invalid (the local hash is then omitted
// because emit can't be derived).
type diffJSON struct {
	Name         string       `json:"name"`
	Drift        string       `json:"drift"`
	LocalHash    string       `json:"localHash,omitempty"`
	DeployedHash string       `json:"deployedHash,omitempty"`
	Changes      *changesJSON `json:"changes,omitempty"`
	Error        string       `json:"error,omitempty"`
}

type changesJSON struct {
	Query               bool `json:"query"`
	EngineVersion       bool `json:"engineVersion"`
	Emit                bool `json:"emit"`
	TrackEmittedStreams bool `json:"trackEmittedStreams"`
}

func renderDiffJSON(w io.Writer, e comparison) error {
	j := diffJSON{Name: e.Name, Drift: string(e.State)}
	// A local hash needs emit, which an invalid (uncompilable) projection can't
	// provide, so omit it and report the compile error instead.
	if e.Local != nil && e.State != driftInvalid {
		j.LocalHash = e.Local.Hash()
	}
	if e.Deployed != nil {
		j.DeployedHash = e.Deployed.Hash()
	}
	if e.State == driftInvalid && e.LocalErr != nil {
		j.Error = e.LocalErr.Error()
	}
	if e.State == driftDrifted {
		j.Changes = &changesJSON{
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
