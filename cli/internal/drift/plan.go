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
	ActionCreate Action = "create"
	ActionUpdate Action = "update"
	ActionReset  Action = "reset" // a logic-change update applied with a rebuild from zero
	ActionSkip   Action = "skip"
	ActionRefuse Action = "refuse"
)

// Applies reports whether the action performs a server write (so the apply phase
// runs it); skip and refuse don't.
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
// reason string is non-empty only for refuse. Engine version and
// track-emitted-streams are create-time-only (no update path), so a drift in
// either forces a destructive recreate that deploy refuses rather than performs.
func PlanAction(c Comparison) (Action, string) {
	switch c.State {
	case NotDeployed:
		return ActionCreate, ""
	case InSync:
		return ActionSkip, ""
	case Drifted:
		if c.Cmp.EngineVersionDiffers || c.Cmp.TrackEmittedStreamsDiffers {
			return ActionRefuse, deploy.RecreateReason(c.Cmp, *c.Local, *c.Deployed)
		}
		return ActionUpdate, ""
	case Invalid:
		// The local definition is invalid - it doesn't compile, or carries a
		// per-projection config error (e.g. a missing engine_version or a bad entry
		// path). Either way there's no correct definition to send, so refuse, naming
		// the actual problem when we have it.
		if c.LocalErr != nil {
			return ActionRefuse, c.LocalErr.Error()
		}
		return ActionRefuse, "local definition is invalid"
	default:
		// Untracked never reaches here: deploy only plans names in config,
		// so Compare returns one of the above. Defensive only.
		return ActionRefuse, "not in gaffer.toml"
	}
}
