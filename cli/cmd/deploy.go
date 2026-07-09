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
	created, updated, rebuilt, skipped, refused, invalid, failed int
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
	case res.Action == drift.ActionInvalid:
		c.invalid++
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
			"Deploy builds the whole plan first, then validates it: it compiles the projections " +
			"it would create or update, and if any won't run (fails to compile, or would fault on " +
			"the server) it refuses before writing anything, so a bad projection can't leave a " +
			"half-applied set. --no-validate skips the check, deploying the valid projections and " +
			"refusing the invalid ones individually instead of aborting the whole run.\n\n" +
			"When the plan would change something, deploy shows it and asks to confirm before " +
			"applying; updating a projection that's currently faulted is flagged, since the update " +
			"won't clear the fault, and so is one whose deployed definition was changed outside gaffer " +
			"since its last deploy (deploying overwrites it). --yes skips the prompt; " +
			"without a terminal (or with --json) deploy " +
			"won't apply unconfirmed, so pass --yes in scripts. A production target (a server " +
			"that declares itself production, or an env with production = true) gets a louder " +
			"confirm and refuses --no-validate. " +
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
			"fixtures and local runs assumed the declared values. Advisory only: when the node's " +
			"options can't be read (no HTTP surface, auth refusal), deploy warns that the check " +
			"couldn't run instead of failing or reporting a false \"in sync\".",
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
	cmd.Flags().BoolVar(&opts.NoValidate, "no-validate", false, "Skip validation: deploy the valid projections and refuse invalid ones per-projection")
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
		switch {
		case opts.DryRun && opts.JSON:
			// An empty project still emits the plan envelope (verdict in-sync, no
			// changes) so a --json consumer always parses the same shape.
			return renderDryRun(cmd.OutOrStdout(), nil, "", "", nil, planTotals{}, drift.ConfigDriftResult{}, true)
		case opts.JSON:
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), "[]")
		default:
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), "No projections to deploy.")
		}
		return nil
	}

	// A deploy-scoped context so an interrupt (Ctrl-C: a signal during planning, or
	// a raw-mode key during the interactive apply) cancels the in-flight work and
	// stops the loops rather than running to completion.
	ctx, cancel := context.WithCancel(cmd.Context())
	defer cancel()

	// Connect before planning: the plan compares each projection against what's
	// deployed, so building it needs the server. (The old preflight compiled
	// locally and could refuse before connecting; now a projection that won't run
	// surfaces as an invalid item in the full plan, a truer picture than a fast
	// offline abort. You can't use deploy to check that your projections compile.)
	r, resolved, cleanup, err := connectResolved(cfg, root, opts.Connection, opts.Env)
	if err != nil {
		return err
	}
	defer cleanup()

	// The [database_config] drift check runs in the background so its HTTP
	// round-trip overlaps the planning RPCs; drained before the confirm, so an
	// operator sees the target's engine config diverging before applying.
	driftCh := drift.StartConfigDriftCheck(cmd.Context(), cfg, root, resolved)

	// Plan first (reads only), so the whole run is known before any write - the
	// basis for the confirm gate and --dry-run.
	plan := drift.PlanAll(ctx, r, cfg, root, names)
	if ctx.Err() != nil {
		return silent(ctx.Err())
	}

	// --reset-on-logic-change turns each logic-change update into a rebuild from
	// zero, so the confirm and the apply both treat it as a reset.
	drift.ResolveResets(plan, opts.ResetOnLogicChange)

	// Validate: run the runtime's preflight over the applying items and fold any
	// that would fault on the server into the plan as invalid. --no-validate skips
	// it - its whole purpose - so a fault-prone projection deploys anyway (a
	// projection that outright won't compile is already invalid from the plan's own
	// compile, --no-validate or not, and just refuses per-item).
	if !opts.NoValidate {
		validatePlan(ctx, root, cfg, plan)
		if ctx.Err() != nil {
			return silent(ctx.Err())
		}
	}

	// Only a plan that changes something needs the target identity (and
	// confirmation). Reading server info is a leader round-trip, so skip it on a
	// no-op deploy. The same bounded $server-info read the operate verbs use names
	// the target in the confirm and gates the production tier (the DB's own flag
	// OR the env's production opt-in, never the env label alone); an unreadable
	// $server-info falls back to the env name and its opt-in.
	totals := planChangeCounts(plan)
	targetName, prod := "", false
	var production *bool
	if totals.changes() > 0 {
		targetName, prod = r.OperateTarget(ctx, resolved, projectionRPCTimeout)
		production = &prod
	}

	// Refuse the prod --no-validate combination before applying - nothing has been
	// written yet. Enforced even under --dry-run: this guards the dangerous flag
	// combination itself, not the write, so a preview of it is refused too rather
	// than misreporting a plan the real deploy would never run. The guardrail is
	// defined once (shared with recreate) so its message and exit code can't drift.
	if prod && opts.NoValidate {
		return refuseNoValidateOnProd("Deploy", "projections are", targetName)
	}

	// The engine-config warning lands before the dry-run render and the confirm
	// alike: fixtures and local runs assumed the declared config, so a diverging
	// target is worth seeing before any decision. Stderr, so a --json consumer's
	// stdout payload stays clean while the warning still reaches CI logs.
	dr := <-driftCh
	writeConfigDriftWarnings(cmd.ErrOrStderr(), dr)

	// --dry-run reports the plan and applies nothing, so it stops before the apply
	// gate below - a read-only preview needs no confirmation, and a non-interactive
	// dry-run must report drift as exit 2, not refuse as 3. It does honour the
	// production --no-validate refusal above, which a real deploy would also hit.
	if opts.DryRun {
		return renderDryRun(cmd.OutOrStdout(), plan, resolved.Name, targetName, production, totals, dr, opts.JSON)
	}

	// Validate gate for the real apply: an invalid projection refuses the whole run
	// unless --no-validate, so a bad one can't leave a half-applied set (the
	// invariant the old preflight held, now enforced against the built plan).
	if !opts.NoValidate {
		if err := refuseInvalidPlan(cmd.OutOrStdout(), plan, opts.JSON); err != nil {
			return err
		}
	}

	if err := confirmPlan(cmd.OutOrStdout(), cmd.ErrOrStderr(), plan, targetName, totals, opts.Yes, opts.JSON, prod); err != nil {
		return err
	}

	// The tool-metadata gaffer stamps on every create/update this deploy makes.
	ledger := toolLedger(resolved, remote.OpDeploy, root)

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
