package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/kurrent-io/gaffer/cli/internal/apply"
	"github.com/kurrent-io/gaffer/cli/internal/config"
	"github.com/kurrent-io/gaffer/cli/internal/drift"
	"github.com/kurrent-io/gaffer/cli/internal/remote"
)

type deployOpts struct {
	Env                string
	Connection         string
	JSON               bool
	NoValidate         bool
	Yes                bool
	ResetOnLogicChange bool
	DryRun             bool
}

// deployCounts tallies outcomes for the summary line and the live progress bar.
type deployCounts struct {
	created, updated, rebuilt, skipped, refused, failed int
}

func (c *deployCounts) add(res drift.Result) {
	switch {
	case res.Err != nil:
		c.failed++
	case res.Action == drift.ActionCreate:
		c.created++
	case res.Action == drift.ActionUpdate:
		c.updated++
	case res.Action == drift.ActionReset:
		c.rebuilt++
	case res.Action == drift.ActionSkip:
		c.skipped++
	case res.Action == drift.ActionRefuse:
		c.refused++
	}
}

func newDeployCmd() *cobra.Command {
	var opts deployOpts
	cmd := &cobra.Command{
		Use:   "deploy [projection]",
		Short: "Create or update projections on an environment",
		Long: "Deploy projections from gaffer.toml to a KurrentDB environment: create the ones " +
			"not yet on the server, update the ones whose definition changed, and skip the ones " +
			"already in sync (matched by content hash).\n\n" +
			"With no argument, deploys every projection in gaffer.toml; name one to deploy just it. " +
			"The emit flag is always sent explicitly so an update never clears it.\n\n" +
			"A changed query is a logic change: the new code may interpret already-processed events " +
			"differently, so the accumulated state could now be wrong. By default deploy continues from " +
			"the existing checkpoint (state is kept) and flags the change. Pass --reset-on-logic-change " +
			"to rebuild instead, reprocessing from zero with the new logic (slower, and an emitting " +
			"projection re-emits). A change to engine version or track-emitted-streams can't be applied " +
			"in place; deploy refuses it and points you at gaffer recreate.\n\n" +
			"Every projection is compiled before anything is sent to the server; if any fails to " +
			"compile or has errors that would fault on the server, the whole deploy is refused so " +
			"a bad projection can't leave a half-applied set. --no-validate skips this check.\n\n" +
			"When the plan would change something, deploy shows it and asks to confirm before " +
			"applying; updating a projection that's currently faulted is flagged, since the update " +
			"won't clear the fault, and so is one whose deployed definition was changed outside gaffer " +
			"since its last deploy (deploying overwrites it). --yes skips the prompt; " +
			"without a terminal (or with --json) deploy " +
			"won't apply unconfirmed, so pass --yes in scripts. A server that reports itself as " +
			"production gets a louder confirm and refuses --no-validate. " +
			"Pass --json for machine-readable output.\n\n" +
			"--dry-run shows the plan and applies nothing. The exit code is stable for scripts: " +
			"0 succeeded (or nothing to do), 1 an error, 2 changes are pending (--dry-run only), " +
			"3 refused by a guardrail (confirmation needed but no terminal or --yes, or " +
			"--no-validate against production).\n\n" +
			"Each create or update records tool metadata on the projection for attribution: the " +
			"tool and version, the source revision, and the acting identity. The revision is the " +
			"project's git commit, suffixed +changes when the tree is dirty; the actor is the user " +
			"gaffer connects as. For CI, the GAFFER_REVISION and GAFFER_ACTOR environment variables " +
			"override them (to record the canonical commit or the pipeline identity). A KurrentDB " +
			"that predates the feature ignores the metadata and deploy is unaffected.\n\n" +
			"When gaffer.toml declares a [database_config], deploy also checks the target node's " +
			"live engine settings and warns on a divergence before anything is applied, since the " +
			"fixtures and local runs assumed the declared values. Advisory only: a server that " +
			"doesn't expose its options (or refuses the read) skips the check silently.",
		Example: "  gaffer deploy\n" +
			"  gaffer deploy order-count --env staging",
		Args: maxArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var name string
			if len(args) == 1 {
				name = args[0]
			}
			return runDeploy(cmd, name, opts)
		},
	}
	cmd.Flags().StringVar(&opts.Env, "env", "", "Environment from gaffer.toml to deploy to")
	cmd.Flags().StringVar(&opts.Connection, "connection", "", "KurrentDB connection string (overrides --env)")
	cmd.Flags().BoolVar(&opts.JSON, "json", false, "Output as JSON")
	cmd.Flags().BoolVar(&opts.NoValidate, "no-validate", false, "Skip the preflight compile check and deploy anyway")
	cmd.Flags().BoolVarP(&opts.Yes, "yes", "y", false, "Skip the confirmation prompt")
	cmd.Flags().BoolVar(&opts.ResetOnLogicChange, "reset-on-logic-change", false, "Rebuild from zero on a logic change instead of continuing from checkpoint")
	cmd.Flags().BoolVar(&opts.DryRun, "dry-run", false, "Show the plan and exit without applying (exit 2 if changes are pending)")
	return cmd
}

func runDeploy(cmd *cobra.Command, name string, opts deployOpts) error {
	cfg, root, err := loadProject()
	if err != nil {
		return err
	}

	names, err := deployNames(cfg, name)
	if err != nil {
		return err
	}
	if len(names) == 0 {
		if opts.JSON {
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), "[]")
		} else {
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), "No projections to deploy.")
		}
		return nil
	}

	// A deploy-scoped context so an interrupt (Ctrl-C: a signal during preflight,
	// or a raw-mode key during the interactive apply) cancels the in-flight work
	// and stops the loops rather than running to completion.
	ctx, cancel := context.WithCancel(cmd.Context())
	defer cancel()

	// Preflight gate, before connecting: compile everything locally, unless
	// bypassed. A failure refuses the whole run - so a bad projection can't leave
	// the earlier ones already applied - and needs no reachable server.
	if !opts.NoValidate {
		failures := runPreflight(ctx, root, cfg, names)
		if ctx.Err() != nil {
			return silent(ctx.Err())
		}
		if len(failures) > 0 {
			if err := renderPreflightFailures(cmd.OutOrStdout(), opts.JSON, len(names), failures); err != nil {
				return err
			}
			return silent(fmt.Errorf("preflight failed: %d of %d projections have errors", len(failures), len(names)))
		}
	}

	r, cleanup, err := connectResolved(cfg, root, opts.Connection, opts.Env)
	if err != nil {
		return err
	}
	defer cleanup()

	// The [database_config] drift check runs in the background so its HTTP
	// round-trip overlaps the planning RPCs; drained before the confirm, so an
	// operator sees the target's engine config diverging before applying.
	resolved, _ := resolveLiveEnv(opts.Connection, opts.Env, cfg)
	driftCh := drift.StartConfigDriftCheck(cmd.Context(), cfg, root, resolved.Name, resolved.Connection)

	// Plan first (reads only), so the whole run is known before any write - the
	// basis for the confirm gate and --dry-run.
	plan := drift.PlanAll(ctx, r, cfg, root, names)
	if ctx.Err() != nil {
		return silent(ctx.Err())
	}

	// --reset-on-logic-change turns each logic-change update into a rebuild from
	// zero, so the confirm and the apply both treat it as a reset.
	drift.ResolveResets(plan, opts.ResetOnLogicChange)

	// Only a plan that changes something needs the target identity (and
	// confirmation). Reading server info is a leader round-trip, so skip it on a
	// no-op deploy. The same bounded $server-info read the operate verbs use names
	// the target in the confirm and gates the production tier (keyed on the DB's
	// own flag, never the env label); an unreadable $server-info falls back to the
	// env label and non-production.
	totals := planChangeCounts(plan)
	target, prod := "", false
	if totals.changes() > 0 {
		target, prod = resolveOperateTarget(ctx, r, opts.Env)
	}

	// Refuse the prod --no-validate combination before applying - nothing has been
	// written yet. Enforced even under --dry-run: this guards the dangerous flag
	// combination itself, not the write, so a preview of it is refused too rather
	// than misreporting a plan the real deploy would never run. The guardrail is
	// defined once (shared with recreate) so its message and exit code can't drift.
	if prod && opts.NoValidate {
		return refuseNoValidateOnProd("Deploy", "projections are", target)
	}

	// The engine-config warning lands before the dry-run render and the confirm
	// alike: fixtures and local runs assumed the declared config, so a diverging
	// target is worth seeing before any decision. Stderr, so a --json consumer's
	// stdout payload stays clean while the warning still reaches CI logs.
	writeConfigDriftWarnings(cmd.ErrOrStderr(), <-driftCh)

	// --dry-run reports the plan and applies nothing, so it stops before the confirm
	// gate below - an apply-only guardrail a read-only preview needs no answer to (a
	// non-interactive dry-run must still report drift as exit 2, not refuse as 3). It
	// does honour the production --no-validate refusal above, which a real deploy with
	// the same flags would hit before any prompt.
	if opts.DryRun {
		return renderDryRun(cmd.OutOrStdout(), plan, target, totals, prod, opts.JSON)
	}

	if err := confirmPlan(cmd.OutOrStdout(), cmd.ErrOrStderr(), plan, target, totals, opts.Yes, opts.JSON, prod); err != nil {
		return err
	}

	// The tool-metadata gaffer stamps on every create/update this deploy makes.
	ledger := toolLedger(opts.Connection, opts.Env, remote.OpDeploy, cfg, root)

	sink := newDeploySink(cmd.OutOrStdout(), cmd.ErrOrStderr(), opts.JSON, names, ctx, cancel)
	failed := apply.Plan(ctx, plan, r, ledger, sink.start, sink.done)
	if ferr := sink.finish(); ferr != nil {
		return ferr
	}
	if ctx.Err() != nil {
		// Interrupted: the summary so far is already shown. Exit non-zero (the
		// deploy is incomplete) without fang printing the cancellation.
		return silent(ctx.Err())
	}
	if failed > 0 {
		// The sink has already reported each outcome and the summary; exit
		// non-zero without fang re-printing a redundant error.
		return silent(fmt.Errorf("%d of %d projections not deployed", failed, len(names)))
	}
	return nil
}

// deployNames resolves the projections to deploy: a single named one (validated
// against config) or every projection in config when name is empty.
func deployNames(cfg *config.Config, name string) ([]string, error) {
	if name != "" {
		if cfg.FindProjection(name) == nil {
			return nil, fmt.Errorf("projection %q is not in gaffer.toml", name)
		}
		return []string{name}, nil
	}
	names := make([]string, 0, len(cfg.Projection))
	for i := range cfg.Projection {
		names = append(names, cfg.Projection[i].Name)
	}
	return names, nil
}

// rpc bounds one server call by projectionRPCTimeout for the commands that
// read or write outside the apply loop (rollback's read and update, the
// operate verbs' existence checks and RPCs).
func rpc(ctx context.Context, fn func(context.Context) error) error {
	ctx, cancel := context.WithTimeout(ctx, projectionRPCTimeout)
	defer cancel()
	return fn(ctx)
}
