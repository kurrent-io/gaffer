package cmd

import (
	"context"
	"encoding/json"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/kurrent-io/gaffer/cli/internal/cliout"
	"github.com/kurrent-io/gaffer/cli/internal/config"
	"github.com/kurrent-io/gaffer/cli/internal/deploy"
	"github.com/kurrent-io/gaffer/cli/internal/drift"
	"github.com/kurrent-io/gaffer/cli/internal/engine"
	"github.com/kurrent-io/gaffer/cli/internal/remote"
	"github.com/kurrent-io/gaffer/cli/internal/target"
	"github.com/kurrent-io/gaffer/cli/internal/telemetry"
	"github.com/kurrent-io/gaffer/cli/internal/versiondiff"
)

type diffOpts struct {
	Env        string
	Connection string
	Left       string
	Right      string
	JSON       bool
}

func newDiffCmd() *cobra.Command {
	var opts diffOpts
	cmd := &cobra.Command{
		Use:   "diff <projection>",
		Short: "Compare two versions of a projection",
		Long: "By default gaffer diff compares the local definition against what's deployed " +
			"on KurrentDB; --left and --right pick any two versions to compare instead.\n\n" +
			"Each side is one of: local (the definition in gaffer.toml), deployed (what's " +
			"live now), or a content-hash prefix (a past version from the projection's " +
			"history; resolving a hash costs a history read). The default is " +
			"--left deployed --right local.\n\n" +
			"The default deployed-vs-local diff reports one of five states: in sync, drifted, " +
			"not deployed (local only), untracked (on the server but absent from gaffer.toml), " +
			"or invalid. Invalid means the local definition can't be used - it doesn't compile, " +
			"or has a config error such as track_emitted_streams on engine version 2; the source " +
			"and config still diff where possible, but emit is unknown. When deploy metadata is " +
			"present, a drifted projection is attributed as local ahead (you edited local since " +
			"deploying) or changed externally (a tool or a direct write changed the server since). " +
			"An untracked projection is shown as an orphan when gaffer deployed it, otherwise as " +
			"plain untracked. A version-to-version diff (any --left/--right other than the default) " +
			"is a pure source diff with no verdict.\n\n" +
			"When the query differs, the source diff is rendered inline: every line of both sides " +
			"with the changes marked, and the span that changed within a line highlighted. Set " +
			"GAFFER_EXTERNAL_DIFF to open an external viewer instead (e.g. git diff, delta, difft).\n\n" +
			"Pass --json for machine-readable output: the two sides (ref, hash, canonical source), " +
			"the structured line diff, and (for the default deployed-vs-local diff) the drift " +
			"verdict, owner, and provenance.",
		Example: "  gaffer diff order-count\n" +
			"  gaffer diff order-count --env staging\n" +
			"  gaffer diff order-count --left 9f2a1c --right local",
		Args: exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) (retErr error) {
			defer oneShotDefer(&retErr, func(o telemetry.Outcome) {
				telemetry.EmitDiff(cmd.Context(), telemetry.DiffCommandInvokedProperties{Outcome: o})
			})
			return runDiff(cmd, args[0], opts)
		},
	}
	cmd.Flags().StringVar(&opts.Env, "env", "", "Environment from gaffer.toml to compare against")
	cmd.Flags().StringVar(&opts.Connection, "connection", "", "KurrentDB connection string (overrides --env)")
	cmd.Flags().StringVar(&opts.Left, "left", "deployed", "Left (base) side: local, deployed, or a content-hash prefix")
	cmd.Flags().StringVar(&opts.Right, "right", "local", "Right (compared) side: local, deployed, or a content-hash prefix")
	cmd.Flags().BoolVar(&opts.JSON, "json", false, "Output as JSON")
	return cmd
}

func runDiff(cmd *cobra.Command, name string, opts diffOpts) error {
	left, err := versiondiff.ParseRef(opts.Left)
	if err != nil {
		return err
	}
	right, err := versiondiff.ParseRef(opts.Right)
	if err != nil {
		return err
	}

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

	// The default deployed↔local diff carries the full drift verdict (owner,
	// attribution, provenance) and the one-sided/invalid rendering; every other
	// combination is a pure source diff between two versions.
	var derr error
	if left.Kind == versiondiff.RefDeployed && right.Kind == versiondiff.RefLocal {
		derr = runComparisonDiff(ctx, cmd, r, cfg, root, name, opts.JSON)
	} else {
		derr = runVersionDiff(ctx, cmd, r, cfg, root, name, left, right, opts.JSON)
	}
	return reclassifyAuth(derr, conn.authInv, conn.env.Name)
}

// reclassifyAuth turns a failed read on a rejected credential into an
// *AuthRequiredError so a caller (an editor) offers a sign-in rather than a bare
// error. A stored token the IdP rejected (invalid_grant) surfaces only on the
// read, not at connect: the lazy connect returns a healthy client and the
// rejection trips the auth flag while returning a generic error - mirrors the
// language server's status fetch. A connect-time no-token failure is already
// *AuthRequiredError from the dial, so it passes through untouched.
func reclassifyAuth(err error, authInv *engine.AuthInvalidation, env string) error {
	if err != nil && authInv != nil && authInv.Tripped() {
		return &target.AuthRequiredError{Env: env}
	}
	return err
}

// runComparisonDiff is the default deployed↔local diff: the drift comparison, its
// verdict, and the inline (or external) source diff - today's gaffer diff.
func runComparisonDiff(ctx context.Context, cmd *cobra.Command, r *remote.Client, cfg *config.Config, root, name string, asJSON bool) error {
	entry, err := drift.Compare(ctx, r, cfg, root, name)
	if err != nil {
		return err
	}

	if asJSON {
		return renderDiffJSON(cmd.OutOrStdout(), entry)
	}
	tw := newTextWriter(cmd.OutOrStdout(), cmd.ErrOrStderr())
	tw.WriteDiff(entry)
	// The query is read from source, not compiled, so the source diff is still
	// worth showing when the local projection is invalid (its whole point is
	// comparing source to what's deployed). Both sides must exist and differ.
	if entry.Cmp.QueryDiffers && entry.Deployed != nil && entry.Local != nil && (entry.State == drift.Drifted || entry.State == drift.Invalid) {
		if argv, ok := externalDiffCommand(os.Getenv); ok {
			return openSourceDiff(argv, entry.Name, "deployed", entry.Deployed.CanonicalQuery(), "local", entry.Local.CanonicalQuery(), cmd.OutOrStdout(), cmd.ErrOrStderr())
		}
		tw.blank()
		tw.WriteQueryDiff(deploy.LineDiff(entry.Deployed.Query, entry.Local.Query))
	}
	return nil
}

// runVersionDiff is a pure source diff between two arbitrary versions - no drift
// verdict (that's meaningful only for deployed↔local). Both sides must resolve;
// a missing side is an error rather than a one-sided diff.
func runVersionDiff(ctx context.Context, cmd *cobra.Command, r *remote.Client, cfg *config.Config, root, name string, left, right versiondiff.Ref, asJSON bool) error {
	j, ls, rs, err := versiondiff.Build(ctx, r, cfg, root, name, left, right)
	if err != nil {
		return err
	}
	// A non-compiling local side still diffs its source, but say so rather than
	// pass off the diff as a clean comparison. On stderr so --json stdout stays
	// pure; the JSON already signals it by omitting that side's hash.
	warnUncompiledSide(cmd.ErrOrStderr(), name, ls)
	warnUncompiledSide(cmd.ErrOrStderr(), name, rs)

	if asJSON {
		return encodeDiffJSON(cmd.OutOrStdout(), j)
	}
	tw := newTextWriter(cmd.OutOrStdout(), cmd.ErrOrStderr())
	tw.heading(name)
	tw.status(tw.styles.muted.Render(transition(ls.Label, rs.Label)))
	if argv, ok := externalDiffCommand(os.Getenv); ok {
		return openSourceDiff(argv, name, ls.Label, ls.JSON.Source, rs.Label, rs.JSON.Source, cmd.OutOrStdout(), cmd.ErrOrStderr())
	}
	tw.blank()
	tw.WriteQueryDiff(j.Lines)
	return nil
}

// warnUncompiledSide notes on stderr that a side's local definition didn't
// compile, so a source-only diff isn't mistaken for a clean comparison.
func warnUncompiledSide(w io.Writer, name string, s versiondiff.ResolvedSide) {
	if s.Uncompiled != nil {
		warningf(w, "local %q doesn't compile, diffing source only: %v", name, s.Uncompiled)
	}
}

// renderDiffJSON emits the default deployed↔local diff: the two sides, the drift
// verdict, and the structured line diff.
func renderDiffJSON(w io.Writer, e drift.Comparison) error {
	return encodeDiffJSON(w, cliout.ComparisonDiffJSON(e))
}

func encodeDiffJSON(w io.Writer, j cliout.DiffJSON) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(j)
}
