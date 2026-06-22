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
	Env        string
	Connection string
	JSON       bool
	Force      bool
}

// deployAction is what deploy decides to do with one projection, derived from
// its drift comparison by planAction.
type deployAction string

const (
	actCreate deployAction = "create"
	actUpdate deployAction = "update"
	actSkip   deployAction = "skip"
	actRefuse deployAction = "refuse"
)

// deployResult is the outcome for one projection. Reason is set only for refuse;
// Err is set when the create/update RPC (or the pre-compare read) failed.
type deployResult struct {
	Name   string
	Action deployAction
	Reason string
	Err    error
}

// projectionWriter is the slice of remote.Client the apply step needs. Seaming
// it lets the orchestration be tested without a live database; *remote.Client
// satisfies it.
type projectionWriter interface {
	Create(ctx context.Context, name, query string, opts remote.CreateOptions) error
	Update(ctx context.Context, name, query string, opts remote.UpdateOptions) error
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
	created, updated, skipped, refused, failed int
}

func (c *deployCounts) add(res deployResult) {
	switch {
	case res.Err != nil:
		c.failed++
	case res.Action == actCreate:
		c.created++
	case res.Action == actUpdate:
		c.updated++
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
			"The emit flag is always sent explicitly so an update never clears it. A change to engine " +
			"version or track-emitted-streams can't be applied in place (it would mean recreating the " +
			"projection and dropping its state), so deploy refuses it and leaves the projection " +
			"untouched.\n\n" +
			"Every projection is compiled before anything is sent to the server; if any fails to " +
			"compile or has errors that would fault on the server, the whole deploy is refused so " +
			"a bad projection can't leave a half-applied set. --force skips this check. Pass --json " +
			"for machine-readable output.",
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
	cmd.Flags().BoolVar(&opts.Force, "force", false, "Skip the preflight compile check and deploy anyway")
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
	if !opts.Force {
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

	sink := newDeploySink(cmd.OutOrStdout(), cmd.ErrOrStderr(), opts.JSON, names, ctx, cancel)
	failed := deployRun(ctx, r, cfg, root, names, sink)
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

// deployRun deploys each projection in order, reporting progress through the
// sink, and returns how many failed (a create/update error) or were refused. It
// continues past a failure so the remaining projections still deploy and the
// summary is complete; the caller turns a non-zero count into a non-zero exit.
func deployRun(ctx context.Context, r *remote.Client, cfg *config.Config, root string, names []string, sink deploySink) int {
	return runDeployLoop(ctx, names, sink, func(name string) deployResult {
		return deployOne(ctx, r, cfg, root, name)
	})
}

// runDeployLoop drives the sink over each projection's outcome, stopping early
// if the context is cancelled (an interrupt). The per-projection work is a
// parameter so the loop's accounting - the failed/refused tally and the
// start/done event order - is testable without a live database.
func runDeployLoop(ctx context.Context, names []string, sink deploySink, one func(name string) deployResult) (failed int) {
	total := len(names)
	for i, name := range names {
		if ctx.Err() != nil {
			break
		}
		sink.start(name, i+1, total)
		res := one(name)
		if res.Err != nil || res.Action == actRefuse {
			failed++
		}
		sink.done(res)
	}
	return failed
}

// deployOne compares one projection and applies the planned action. Each
// projection gets its own bounded context: a management RPC blocks until its
// deadline if the projections subsystem is slow to respond, and one stalled
// projection should not consume the whole run's budget.
func deployOne(ctx context.Context, r *remote.Client, cfg *config.Config, root, name string) deployResult {
	ctx, cancel := context.WithTimeout(ctx, projectionRPCTimeout)
	defer cancel()

	cmp, err := compareProjection(ctx, r, cfg, root, name)
	if err != nil {
		return deployResult{Name: name, Err: err}
	}
	action, reason := planAction(cmp)
	if action == actCreate || action == actUpdate {
		if err := applyAction(ctx, r, name, action, cmp.Local); err != nil {
			return deployResult{Name: name, Action: action, Err: err}
		}
	}
	return deployResult{Name: name, Action: action, Reason: reason}
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
		// Only reachable under --force (preflight otherwise blocks first): the
		// source doesn't compile, so emit is unknown and the projection can't be
		// applied correctly. Refuse rather than send a wrong definition.
		return actRefuse, "local source does not compile"
	default:
		// Untracked never reaches here: deployNames only yields names in config,
		// so compareProjection returns one of the above. Defensive only.
		return actRefuse, "not in gaffer.toml"
	}
}

// recreateReason states which create-time field changed, matching gaffer diff's
// "remote X, local Y" phrasing. It deliberately stops at the fact: the way to
// resolve it (a destructive recreate) lands with the guardrails work.
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
	return which + " can't be changed in place"
}

// applyAction performs the create or update. Emit is always sent on update
// (as a non-nil pointer) because the server resets it to false on any update
// that omits it. A created continuous projection starts enabled server-side, so
// there is no separate enable step.
func applyAction(ctx context.Context, w projectionWriter, name string, action deployAction, local *deploy.Descriptor) error {
	switch action {
	case actCreate:
		return w.Create(ctx, name, local.Query, remote.CreateOptions{
			EngineVersion:       local.EngineVersion,
			Emit:                local.Emit,
			TrackEmittedStreams: local.TrackEmittedStreams,
		})
	case actUpdate:
		emit := local.Emit
		return w.Update(ctx, name, local.Query, remote.UpdateOptions{Emit: &emit})
	default:
		return nil
	}
}
