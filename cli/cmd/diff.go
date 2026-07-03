package cmd

import (
	"context"
	"encoding/json"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/kurrent-io/gaffer/cli/internal/cliout"
	"github.com/kurrent-io/gaffer/cli/internal/deploy"
	"github.com/kurrent-io/gaffer/cli/internal/drift"
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
			"definition can't be used - it doesn't compile, or has a config error such as " +
			"track_emitted_streams on engine version 2; the source and config still diff where " +
			"possible, but emit is unknown.\n\n" +
			"When the query differs, the source diff is rendered inline: every line of both " +
			"sides with the changes marked, and the span that changed within a line " +
			"highlighted. Set GAFFER_EXTERNAL_DIFF to open an external viewer instead (e.g. " +
			"git diff, delta, difft).\n\n" +
			"When deploy metadata is present, a drifted projection is attributed as local " +
			"ahead (you edited local since deploying) or changed externally (a tool or a " +
			"direct write changed the server since). An untracked projection is shown as an " +
			"orphan when gaffer deployed it, otherwise as plain untracked. The provenance " +
			"block names the tool, deployer, and revision behind it. Pass --json for " +
			"machine-readable output, which splits changed externally into changed-by-tool " +
			"and changed-server and carries the owner (including foreign).",
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
	conn, err := connectEnv(opts.Connection, opts.Env)
	if err != nil {
		return err
	}
	defer conn.cleanup()
	cfg, root, r := conn.cfg, conn.root, conn.r

	// remote calls block until their context deadline if the projections
	// subsystem doesn't respond, so bound the read rather than hang the command.
	ctx, cancel := context.WithTimeout(cmd.Context(), projectionRPCTimeout)
	defer cancel()
	entry, err := drift.Compare(ctx, r, cfg, root, name)
	if err != nil {
		return err
	}

	if opts.JSON {
		return renderDiffJSON(cmd.OutOrStdout(), entry)
	}
	tw := newTextWriter(cmd.OutOrStdout(), cmd.ErrOrStderr())
	tw.WriteDiff(entry)
	// The query is read from source, not compiled, so the source diff is still
	// worth showing when the local projection is invalid (its whole point is
	// comparing source to what's deployed). Both sides must exist and differ.
	if entry.Cmp.QueryDiffers && entry.Deployed != nil && entry.Local != nil && (entry.State == drift.Drifted || entry.State == drift.Invalid) {
		if argv, ok := externalDiffCommand(os.Getenv); ok {
			return openSourceDiff(argv, entry.Name, entry.Deployed.CanonicalQuery(), entry.Local.CanonicalQuery(), cmd.OutOrStdout(), cmd.ErrOrStderr())
		}
		tw.blank()
		tw.WriteQueryDiff(deploy.LineDiff(entry.Deployed.Query, entry.Local.Query))
	}
	return nil
}

// diffJSON is the --json shape for one projection. drift is the verdict (one of
// in-sync, drifted, not-deployed, untracked, invalid), matching gaffer status;
// changes names the dimensions that differ, present only when drifted. error is
// the compile failure, present only when invalid (the local hash is then omitted
// because emit can't be derived).
type diffJSON struct {
	Name         string             `json:"name"`
	Drift        string             `json:"drift"`
	Owner        string             `json:"owner"`
	Attribution  string             `json:"attribution,omitempty"`
	LastDeployed string             `json:"lastDeployed,omitempty"`
	LastWrite    *cliout.LedgerJSON `json:"lastWrite,omitempty"`
	LocalHash    string             `json:"localHash,omitempty"`
	DeployedHash string             `json:"deployedHash,omitempty"`
	Changes      *changesJSON       `json:"changes,omitempty"`
	Error        string             `json:"error,omitempty"`
}

type changesJSON struct {
	Query               bool `json:"query"`
	EngineVersion       bool `json:"engineVersion"`
	Emit                bool `json:"emit"`
	TrackEmittedStreams bool `json:"trackEmittedStreams"`
}

func renderDiffJSON(w io.Writer, e drift.Comparison) error {
	j := diffJSON{Name: e.Name, Drift: string(e.State), Owner: string(e.Owner()), Attribution: string(e.Attribution()), LastDeployed: cliout.LastDeployedJSON(e), LastWrite: cliout.BuildLedgerJSON(e)}
	// A local hash needs emit, which an invalid (uncompilable) projection can't
	// provide, so omit it and report the compile error instead.
	if e.Local != nil && e.State != drift.Invalid {
		j.LocalHash = e.Local.Hash()
	}
	if e.Deployed != nil {
		j.DeployedHash = e.Deployed.Hash()
	}
	if e.State == drift.Invalid && e.LocalErr != nil {
		j.Error = e.LocalErr.Error()
	}
	if e.State == drift.Drifted {
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
