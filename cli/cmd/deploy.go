package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/kurrent-io/gaffer/cli/internal/config"
	"github.com/kurrent-io/gaffer/cli/internal/deploy"
	"github.com/kurrent-io/gaffer/cli/internal/remote"
)

type deployOpts struct {
	Env                string
	Connection         string
	JSON               bool
	NoValidate         bool
	Yes                bool
	ResetOnLogicChange bool
}

// deployAction is what deploy decides to do with one projection, derived from
// its drift comparison by planAction.
type deployAction string

const (
	actCreate deployAction = "create"
	actUpdate deployAction = "update"
	actReset  deployAction = "reset" // a logic-change update applied with a rebuild from zero
	actSkip   deployAction = "skip"
	actRefuse deployAction = "refuse"
)

// applies reports whether the action performs a server write (so the apply phase
// runs it); skip and refuse don't.
func (a deployAction) applies() bool {
	return a == actCreate || a == actUpdate || a == actReset
}

// deployResult is the outcome for one projection. Reason is set only for refuse;
// Err is set when the apply RPC (or the pre-compare read) failed. LogicChange
// marks an update that changed the query, so the rendering can note that
// continuing keeps state computed by the old logic.
type deployResult struct {
	Name        string
	Action      deployAction
	Reason      string
	LogicChange bool
	Err         error
}

// projectionManager is the slice of remote.Client the apply step needs: create
// and update, plus the disable/reset/enable a logic-change rebuild sequences.
// Seaming it lets the orchestration be tested without a live database;
// *remote.Client satisfies it.
type projectionManager interface {
	Create(ctx context.Context, name, query string, opts remote.CreateOptions) error
	Update(ctx context.Context, name, query string, opts remote.UpdateOptions) error
	Disable(ctx context.Context, name string) error
	Reset(ctx context.Context, name string, writeCheckpoint bool) error
	Enable(ctx context.Context, name string) error
}

// outcome is the past-tense verdict for one projection, used as the JSON value
// and the text word. A failure (Err set) reads as "failed" regardless of which
// action was attempted.
func (res deployResult) outcome() string {
	if res.Err != nil {
		return "failed"
	}
	switch res.Action {
	case actCreate:
		return "created"
	case actUpdate:
		return "updated"
	case actReset:
		return "rebuilt"
	case actSkip:
		return "skipped"
	case actRefuse:
		return "refused"
	default:
		return "unknown"
	}
}

// deployCounts tallies outcomes for the summary line and the live progress bar.
type deployCounts struct {
	created, updated, rebuilt, skipped, refused, failed int
}

func (c *deployCounts) add(res deployResult) {
	switch {
	case res.Err != nil:
		c.failed++
	case res.Action == actCreate:
		c.created++
	case res.Action == actUpdate:
		c.updated++
	case res.Action == actReset:
		c.rebuilt++
	case res.Action == actSkip:
		c.skipped++
	case res.Action == actRefuse:
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
			"won't clear the fault. --yes skips the prompt; without a terminal (or with --json) deploy " +
			"won't apply unconfirmed, so pass --yes in scripts. A server that reports itself as " +
			"production gets a louder confirm and refuses --no-validate. " +
			"Pass --json for machine-readable output.",
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

	// Plan first (reads only), so the whole run is known before any write - the
	// basis for the confirm gate and, later, --dry-run.
	plan := planAll(ctx, r, cfg, root, names)
	if ctx.Err() != nil {
		return silent(ctx.Err())
	}

	// --reset-on-logic-change turns each logic-change update into a rebuild from
	// zero, so the confirm and the apply both treat it as a reset.
	resolveResets(plan, opts.ResetOnLogicChange)

	// Only a plan that changes something needs the target identity (and
	// confirmation). Reading server info is a leader round-trip, so skip it on a
	// no-op deploy.
	totals := planChangeCounts(plan)
	target, prod := "", false
	if totals.changes() > 0 {
		// The server's self-reported identity names the target in the confirm and
		// gates the production tier (it keys on the DB's own flag, never the env
		// label). Bound the read like the other management calls so a hung
		// $server-info can't stall the deploy.
		siCtx, siCancel := context.WithTimeout(ctx, projectionRPCTimeout)
		info, siErr := r.ServerInfo(siCtx)
		siCancel()
		// $server-info is advisory and often unreadable - absent on most DBs, or
		// ACL-restricted on a secured server - so any error falls back to baseline
		// silently (info is nil). Not worth a warning on every deploy, and a real
		// connection failure surfaces when the apply writes. Trade-off: an
		// unreadable prod DB drops the prod tier (re-permits --no-validate); the core
		// never-apply-unconfirmed guard still holds.
		_ = siErr
		target = deployTarget(opts.Env, info)
		prod = info.IsProduction()
	}

	// --no-validate skips the preflight compile gate; production never accepts it,
	// so a prod deploy always validates first. Refuse before applying - nothing has
	// been written yet.
	if prod && opts.NoValidate {
		return silent(fmt.Errorf("--no-validate is not allowed on production %s: it skips the preflight compile check. Deploy without it so projections are validated first", targetDesc(target)))
	}

	if err := confirmPlan(cmd.OutOrStdout(), cmd.ErrOrStderr(), plan, target, totals, opts.Yes, opts.JSON, prod); err != nil {
		return err
	}

	sink := newDeploySink(cmd.OutOrStdout(), cmd.ErrOrStderr(), opts.JSON, names, ctx, cancel)
	failed := applyPlan(ctx, plan, sink, func(item plannedItem) error {
		return applyOne(ctx, r, item)
	})
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

// plannedItem is one projection's planned action, computed by the plan phase
// before any write. cmp carries the comparison (Local for the apply, Deployed
// for the guards); err is a planning failure (the compare/read), kept distinct
// from an apply failure so the two surface with the right reason.
type plannedItem struct {
	name        string
	cmp         comparison
	action      deployAction
	reason      string
	logicChange bool // an update whose query changed (state may now be wrong)
	faulted     bool // deployed projection is currently faulted (update items only)
	err         error
}

// result is the deployResult for an item that was not (or not yet) applied: a
// planning error, or a skip/refuse that the apply phase emits verbatim.
func (p plannedItem) result() deployResult {
	// LogicChange marks a continued logic change (an update that kept state). A
	// reset rebuilds, so it reports outcome "rebuilt", not a logic-change flag -
	// drop the flag once the item is no longer an update.
	return deployResult{Name: p.name, Action: p.action, Reason: p.reason, LogicChange: p.logicChange && p.action == actUpdate, Err: p.err}
}

// planAll computes the action for every projection without writing anything -
// the read-only first half of deploy, shared with the confirm gate (and, later,
// --dry-run). Stops early on an interrupt; a per-projection compare failure is
// carried on the item, not fatal, so the rest of the plan still forms.
func planAll(ctx context.Context, r *remote.Client, cfg *config.Config, root string, names []string) []plannedItem {
	plan := make([]plannedItem, 0, len(names))
	updates := false
	for _, name := range names {
		if ctx.Err() != nil {
			break
		}
		item := planOne(ctx, r, cfg, root, name)
		updates = updates || item.action == actUpdate
		plan = append(plan, item)
	}
	// Faulted status only matters for update targets (to warn before clobbering),
	// so list once only when the plan actually updates something - a no-op
	// (all in sync) or create-only deploy skips the leader List entirely.
	if updates {
		faulted := faultedProjections(ctx, r)
		for i := range plan {
			if plan[i].action == actUpdate && faulted[plan[i].name] {
				plan[i].faulted = true
			}
		}
	}
	return plan
}

// faultedProjections lists deployed projections once and returns the set
// currently faulted, so the plan can flag a faulted update target without a
// status call per projection. Best-effort: a list failure yields an empty set
// (no faulted warnings) rather than failing the plan.
func faultedProjections(ctx context.Context, r *remote.Client) map[string]bool {
	ctx, cancel := context.WithTimeout(ctx, projectionRPCTimeout)
	defer cancel()
	statuses, err := r.List(ctx)
	if err != nil {
		return nil
	}
	faulted := make(map[string]bool)
	for i := range statuses {
		if statuses[i].State == remote.StateFaulted {
			faulted[statuses[i].Name] = true
		}
	}
	return faulted
}

// resolveResets promotes each logic-change update to a reset (rebuild from zero)
// when --reset-on-logic-change is set. The plan computes a logic change as an
// update; this is where the flag turns it into a rebuild, before the confirm and
// apply see it. A no-op when the flag is off.
func resolveResets(plan []plannedItem, resetOnLogicChange bool) {
	if !resetOnLogicChange {
		return
	}
	for i := range plan {
		if plan[i].logicChange {
			plan[i].action = actReset
		}
	}
}

// planOne compares one projection and decides its action, applying nothing. The
// read is bounded: a management call blocks until its deadline if the projections
// subsystem is slow, and one stalled projection shouldn't consume the whole
// plan's budget. The faulted flag is filled in by planAll afterwards, only when
// the plan turns out to have updates.
func planOne(ctx context.Context, r *remote.Client, cfg *config.Config, root, name string) plannedItem {
	ctx, cancel := context.WithTimeout(ctx, projectionRPCTimeout)
	defer cancel()

	cmp, err := compareProjection(ctx, r, cfg, root, name)
	if err != nil {
		return plannedItem{name: name, err: err}
	}
	action, reason := planAction(cmp)
	return plannedItem{name: name, cmp: cmp, action: action, reason: reason, logicChange: isLogicChange(action, cmp)}
}

// isLogicChange reports whether an update changes projection logic, meaning the
// query: the new code folds over old events differently, so the accumulated
// state may now be wrong. An emit-only update (query unchanged) is just a
// settings re-assert; a create, refuse or skip is never a logic change.
func isLogicChange(action deployAction, cmp comparison) bool {
	return action == actUpdate && cmp.Cmp.QueryDiffers
}

// applyPlan executes a plan, reporting progress through the sink, and returns how
// many failed (an apply error) or were refused. It applies only create/update
// items; skip/refuse/planning-error items stream their verdict unchanged. It
// continues past a failure so the summary is complete; the caller turns a
// non-zero count into a non-zero exit. The apply itself is a parameter so the
// loop's accounting and event order are testable without a live database.
func applyPlan(ctx context.Context, plan []plannedItem, sink deploySink, apply func(plannedItem) error) (failed int) {
	total := len(plan)
	for i := range plan {
		item := plan[i]
		if ctx.Err() != nil {
			break
		}
		sink.start(item.name, i+1, total)
		res := item.result()
		if item.err == nil && item.action.applies() {
			if err := apply(item); err != nil {
				res.Err = err
			}
		}
		if res.Err != nil || res.Action == actRefuse {
			failed++
		}
		sink.done(res)
	}
	return failed
}

// applyOne performs one item's create/update/reset. Each underlying RPC is
// bounded separately (a reset issues four), so a multi-step rebuild isn't
// squeezed into the budget for a single call.
func applyOne(ctx context.Context, r *remote.Client, item plannedItem) error {
	return applyAction(ctx, r, item.name, item.action, item.cmp.Local)
}

// rpc bounds one server call by projectionRPCTimeout, so every step of a
// multi-step apply gets a full budget rather than sharing one.
func rpc(ctx context.Context, fn func(context.Context) error) error {
	ctx, cancel := context.WithTimeout(ctx, projectionRPCTimeout)
	defer cancel()
	return fn(ctx)
}

// planAction maps a drift comparison to the action deploy takes. It is pure: the
// reason string is non-empty only for refuse. Engine version and
// track-emitted-streams are create-time-only (no update path), so a drift in
// either forces a destructive recreate that deploy refuses rather than performs.
func planAction(c comparison) (deployAction, string) {
	switch c.State {
	case driftNotDeployed:
		return actCreate, ""
	case driftInSync:
		return actSkip, ""
	case driftDrifted:
		if c.Cmp.EngineVersionDiffers || c.Cmp.TrackEmittedStreamsDiffers {
			return actRefuse, recreateReason(c)
		}
		return actUpdate, ""
	case driftInvalid:
		// The local definition is invalid - it doesn't compile, or carries a
		// per-projection config error (e.g. a missing engine_version or a bad entry
		// path). Either way there's no correct definition to send, so refuse, naming
		// the actual problem when we have it.
		if c.LocalErr != nil {
			return actRefuse, c.LocalErr.Error()
		}
		return actRefuse, "local definition is invalid"
	default:
		// Untracked never reaches here: deployNames only yields names in config,
		// so compareProjection returns one of the above. Defensive only.
		return actRefuse, "not in gaffer.toml"
	}
}

// recreateReason states which create-time field changed, matching gaffer diff's
// "remote X, local Y" phrasing, and points at gaffer recreate (the resolve path,
// a separate verb since it destroys and rebuilds the projection).
func recreateReason(c comparison) string {
	var which string
	switch {
	case c.Cmp.EngineVersionDiffers && c.Cmp.TrackEmittedStreamsDiffers:
		which = fmt.Sprintf("engine version (remote %d, local %d) and track emitted streams (remote %t, local %t)",
			c.Deployed.EngineVersion, c.Local.EngineVersion,
			c.Deployed.TrackEmittedStreams, c.Local.TrackEmittedStreams)
	case c.Cmp.EngineVersionDiffers:
		which = fmt.Sprintf("engine version (remote %d, local %d)", c.Deployed.EngineVersion, c.Local.EngineVersion)
	default:
		which = fmt.Sprintf("track emitted streams (remote %t, local %t)", c.Deployed.TrackEmittedStreams, c.Local.TrackEmittedStreams)
	}
	return which + " can't be changed in place, use gaffer recreate"
}

// applyAction performs the create, update, or logic-change reset. Emit is always
// sent on update (as a non-nil pointer) because the server resets it to false on
// any update that omits it. A created continuous projection starts enabled
// server-side, so there is no separate enable step.
func applyAction(ctx context.Context, mgr projectionManager, name string, action deployAction, local *deploy.Descriptor) error {
	switch action {
	case actCreate:
		return rpc(ctx, func(ctx context.Context) error {
			return mgr.Create(ctx, name, local.Query, remote.CreateOptions{
				EngineVersion:       local.EngineVersion,
				Emit:                local.Emit,
				TrackEmittedStreams: local.TrackEmittedStreams,
			})
		})
	case actUpdate:
		return rpc(ctx, func(ctx context.Context) error {
			return mgr.Update(ctx, name, local.Query, remote.UpdateOptions{Emit: emitPtr(local)})
		})
	case actReset:
		return applyReset(ctx, mgr, name, local)
	default:
		return nil
	}
}

// applyReset rebuilds a projection from zero for a logic change: stop it, update
// to the new query, reset to the beginning, restart. Update needs the projection
// stopped; reset rewinds and discards state; the restart reprocesses every event
// with the new logic. A checkpoint is written at the reset so the restart begins
// from zero rather than the pre-reset position.
//
// Disable (not Abort) is the stop: it writes a checkpoint, so a failure before
// the reset leaves the projection stopped at a real position rather than mid-
// batch. The reset overwrites that checkpoint with zero anyway, so the extra
// write is harmless on the happy path and a safer resting point on a partial
// failure. There's no auto-rollback, so each step names the state it leaves and
// the recovery.
func applyReset(ctx context.Context, mgr projectionManager, name string, local *deploy.Descriptor) error {
	if err := rpc(ctx, func(ctx context.Context) error { return mgr.Disable(ctx, name) }); err != nil {
		return fmt.Errorf("stopping for reset (projection untouched): %w", err)
	}
	if err := rpc(ctx, func(ctx context.Context) error {
		return mgr.Update(ctx, name, local.Query, remote.UpdateOptions{Emit: emitPtr(local)})
	}); err != nil {
		return fmt.Errorf("updating for reset - the projection is stopped; run `gaffer start %s` to resume it on the old logic: %w", name, err)
	}
	if err := rpc(ctx, func(ctx context.Context) error { return mgr.Reset(ctx, name, true) }); err != nil {
		return fmt.Errorf("resetting - the projection is stopped on the new query but not rewound; finish the rebuild with `gaffer recreate %s`: %w", name, err)
	}
	if err := rpc(ctx, func(ctx context.Context) error { return mgr.Enable(ctx, name) }); err != nil {
		// State is already wiped and the projection is stopped; no auto-rollback.
		return fmt.Errorf("reset succeeded but the projection failed to restart - run `gaffer start %s` to rebuild it: %w", name, err)
	}
	return nil
}

// emitPtr returns a non-nil pointer to the descriptor's derived emit flag, so an
// update always sends it explicitly (the server clears emit on any update that
// omits it).
func emitPtr(local *deploy.Descriptor) *bool {
	emit := local.Emit
	return &emit
}
