package drift

import (
	"context"

	"github.com/kurrent-io/gaffer/cli/internal/config"
	"github.com/kurrent-io/gaffer/cli/internal/deploy"
	"github.com/kurrent-io/gaffer/cli/internal/remote"
)

// Action is what deploy decides to do with one projection, derived from
// its drift comparison by PlanAction.
type Action string

const (
	ActionCreate  Action = "create"
	ActionUpdate  Action = "update"
	ActionReset   Action = "reset" // a logic-change update applied with a rebuild from zero
	ActionSkip    Action = "skip"
	ActionRefuse  Action = "refuse"  // a valid projection that can't be applied in place - recreate required
	ActionInvalid Action = "invalid" // local definition won't run - compile error, config error, or fault-severity diagnostics
)

// Applies reports whether the action performs a server write (so the apply phase
// runs it); skip, refuse and invalid don't.
func (a Action) Applies() bool {
	return a == ActionCreate || a == ActionUpdate || a == ActionReset
}

// PlanItem is one projection's planned action, computed by the plan phase
// before any write. Cmp carries the comparison (Local for the apply, Deployed
// for the guards); Err is a planning failure (the compare/read), kept distinct
// from an apply failure so the two surface with the right reason.
type PlanItem struct {
	Name        string
	Cmp         Comparison
	Action      Action
	Reason      string
	LogicChange bool // an update whose query changed (state may now be wrong)
	Faulted     bool // deployed projection is currently faulted (update items only)
	Err         error
}

// PlanAll computes the action for every projection without writing anything -
// the read-only first half of deploy, shared with the confirm gate, --dry-run,
// and the MCP plan tool. Stops early on an interrupt; a per-projection compare
// failure is carried on the item, not fatal, so the rest of the plan still forms.
func PlanAll(ctx context.Context, r *remote.Client, cfg *config.Config, root string, names []string) []PlanItem {
	plan := make([]PlanItem, 0, len(names))
	updates := false
	for _, name := range names {
		if ctx.Err() != nil {
			break
		}
		item := PlanOne(ctx, r, cfg, root, name)
		updates = updates || item.Action == ActionUpdate
		plan = append(plan, item)
	}
	// Faulted status only matters for update targets (to warn before clobbering),
	// so list once only when the plan actually updates something - a no-op
	// (all in sync) or create-only deploy skips the leader List entirely.
	if updates {
		faulted := faultedProjections(ctx, r)
		for i := range plan {
			if plan[i].Action == ActionUpdate && faulted[plan[i].Name] {
				plan[i].Faulted = true
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
	ctx, cancel := context.WithTimeout(ctx, deploy.RPCTimeout)
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

// ResolveResets promotes each logic-change update to a reset (rebuild from zero)
// when --reset-on-logic-change is set. The plan computes a logic change as an
// update; this is where the flag turns it into a rebuild, before the confirm and
// apply see it. A no-op when the flag is off.
func ResolveResets(plan []PlanItem, resetOnLogicChange bool) {
	if !resetOnLogicChange {
		return
	}
	for i := range plan {
		if plan[i].LogicChange {
			plan[i].Action = ActionReset
		}
	}
}

// Result is the outcome for one projection. Reason is set for refuse and
// invalid (why deploy won't apply it); Err is set when the apply RPC (or the
// pre-compare read) failed. LogicChange marks an update that changed the query,
// so the rendering can note that continuing keeps state computed by the old
// logic. ExternalChange marks an apply whose deployed definition was changed
// outside gaffer since its last deploy, so the rendering can caution that
// deploying overwrites that change; the apply phase clears it when the apply
// fails, since nothing was then overwritten. ExternalChangeTool names the tool
// behind that change when another tool made it (empty for a direct write).
type Result struct {
	Name               string
	Action             Action
	Reason             string
	LogicChange        bool
	ExternalChange     bool
	ExternalChangeTool string
	Err                error
}

// Outcome is the past-tense verdict for one projection, used as the JSON value
// and the text word. A failure (Err set) reads as "failed" regardless of which
// action was attempted.
func (r Result) Outcome() string {
	if r.Err != nil {
		return "failed"
	}
	switch r.Action {
	case ActionCreate:
		return "created"
	case ActionUpdate:
		return "updated"
	case ActionReset:
		return "rebuilt"
	case ActionSkip:
		return "skipped"
	case ActionRefuse:
		return "refused"
	case ActionInvalid:
		return "invalid"
	default:
		return "unknown"
	}
}

// Result is the outcome for an item that was not (or not yet) applied: a
// planning error, or a skip/refuse that the apply phase emits verbatim.
func (p PlanItem) Result() Result {
	// LogicChange marks a continued logic change (an update that kept state). A
	// reset rebuilds, so it reports outcome "rebuilt", not a logic-change flag -
	// drop the flag once the item is no longer an update.
	external := p.Action.Applies() && p.Cmp.ExternallyChanged()
	tool := ""
	if external && p.Cmp.Attribution() == AttrChangedByTool && p.Cmp.Ledger != nil {
		tool = p.Cmp.Ledger.Tool
	}
	return Result{Name: p.Name, Action: p.Action, Reason: p.Reason, LogicChange: p.LogicChange && p.Action == ActionUpdate, ExternalChange: external, ExternalChangeTool: tool, Err: p.Err}
}

// PlanOne compares one projection and decides its action, applying nothing. The
// read is bounded: a management call blocks until its deadline if the projections
// subsystem is slow, and one stalled projection shouldn't consume the whole
// plan's budget. The Faulted flag is filled in by PlanAll afterwards, only when
// the plan turns out to have updates.
func PlanOne(ctx context.Context, r *remote.Client, cfg *config.Config, root, name string) PlanItem {
	ctx, cancel := context.WithTimeout(ctx, deploy.RPCTimeout)
	defer cancel()

	cmp, err := Compare(ctx, r, cfg, root, name)
	if err != nil {
		return PlanItem{Name: name, Err: err}
	}
	action, reason := PlanAction(cmp)
	return PlanItem{Name: name, Cmp: cmp, Action: action, Reason: reason, LogicChange: isLogicChange(action, cmp)}
}

// isLogicChange reports whether an update changes projection logic, meaning the
// query: the new code folds over old events differently, so the accumulated
// state may now be wrong. An emit-only update (query unchanged) is just a
// settings re-assert; a create, refuse or skip is never a logic change.
func isLogicChange(action Action, cmp Comparison) bool {
	return action == ActionUpdate && cmp.Cmp.QueryDiffers
}

// PlanAction maps a drift comparison to the action deploy takes. It is pure: the
// reason string is non-empty only for refuse (the recreate explanation) and
// invalid (why the local definition won't run). Engine version and
// track-emitted-streams are create-time-only (no update path), so a drift in
// either forces a destructive recreate that deploy refuses rather than performs.
func PlanAction(c Comparison) (Action, string) {
	switch c.State {
	case NotDeployed:
		return ActionCreate, ""
	case InSync:
		return ActionSkip, ""
	case Drifted:
		if c.RecreateRequired() {
			return ActionRefuse, deploy.RecreateReason(c.Cmp, *c.Local, *c.Deployed)
		}
		return ActionUpdate, ""
	case Invalid:
		// The local definition is invalid - it doesn't compile, or carries a
		// per-projection config error (e.g. a missing engine_version or a bad entry
		// path). Either way there's no correct definition to send, so it's invalid,
		// naming the actual problem when we have it.
		if c.LocalErr != nil {
			return ActionInvalid, c.LocalErr.Error()
		}
		return ActionInvalid, "local definition is invalid"
	default:
		// Untracked never reaches here: deploy only plans names in config,
		// so Compare returns one of the above. Defensive only.
		return ActionInvalid, "not in gaffer.toml"
	}
}

// RecreateRequired reports whether a drift can only be applied by recreating the
// projection: engine version or track-emitted-streams changed, neither of which
// has an in-place update path. deploy refuses these (ActionRefuse) and points at
// gaffer recreate. Meaningful only for a Drifted comparison.
func (c Comparison) RecreateRequired() bool {
	return c.State == Drifted && (c.Cmp.EngineVersionDiffers || c.Cmp.TrackEmittedStreamsDiffers)
}

// PlanVerdict summarises a whole plan as what a real deploy would do, in the
// per-projection drift vocabulary scaled up: "blocked" if any item can't be
// deployed (invalid, recreate-required, or a planning error), otherwise
// "deployable" if anything would change, otherwise "in-sync".
func PlanVerdict(plan []PlanItem) string {
	changes := 0
	for _, it := range plan {
		switch {
		case it.Err != nil, it.Action == ActionInvalid, it.Action == ActionRefuse:
			return "blocked"
		case it.Action.Applies():
			changes++
		}
	}
	if changes > 0 {
		return "deployable"
	}
	return "in-sync"
}
