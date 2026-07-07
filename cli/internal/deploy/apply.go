package deploy

import (
	"context"
	"fmt"
	"time"
)

// RPCTimeout bounds a single projection management call. The projections
// subsystem replies with nothing while it is still starting, so an unbounded
// call would hang; diff and status bound the whole command by it, deploy bounds
// each projection by it, and the multi-step rebuild/recreate sequences below
// bound each step by it. Shared so they all move together.
const RPCTimeout = 30 * time.Second

// Step is one server call in a destructive sequence. The orchestration bounds it
// by RPCTimeout, so a multi-step rebuild isn't squeezed into the budget for a
// single call. cmd binds each step to its remote client and option mapping; the
// ordering and recovery semantics - the load-bearing correctness contract - live
// here, testable without a live database.
type Step func(context.Context) error

// bound runs one step under its own RPCTimeout, so every step of a multi-step
// sequence gets a full budget rather than sharing one.
func bound(ctx context.Context, s Step) error {
	ctx, cancel := context.WithTimeout(ctx, RPCTimeout)
	defer cancel()
	return s(ctx)
}

type namedStep struct {
	name string
	step Step
}

// validateSteps refuses a sequence whose steps aren't fully wired, before any of
// them run. Rebuild and Recreate are destructive, so a nil step caught mid-flight
// (a panic, or a failure after Delete) could strand a half-rebuilt projection; a
// wiring gap is a programming error and should surface before the first call.
func validateSteps(steps ...namedStep) error {
	for _, s := range steps {
		if s.step == nil {
			return fmt.Errorf("%s step is not wired", s.name)
		}
	}
	return nil
}

// RebuildSteps are the calls of a logic-change rebuild, in the order Rebuild
// runs them.
type RebuildSteps struct {
	Disable Step
	Update  Step
	Reset   Step
	Enable  Step
}

func (s RebuildSteps) validate() error {
	return validateSteps(
		namedStep{"disable", s.Disable},
		namedStep{"update", s.Update},
		namedStep{"reset", s.Reset},
		namedStep{"enable", s.Enable},
	)
}

// Rebuild rebuilds a projection from zero for a logic change: stop it, update to
// the new query, reset to the beginning, restart. Update needs the projection
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
func Rebuild(ctx context.Context, name string, s RebuildSteps) error {
	if err := s.validate(); err != nil {
		return fmt.Errorf("rebuild %s: %w", name, err)
	}
	if err := bound(ctx, s.Disable); err != nil {
		return fmt.Errorf("stopping for reset (projection untouched): %w", err)
	}
	if err := bound(ctx, s.Update); err != nil {
		return fmt.Errorf("updating for reset - the projection is stopped; run `gaffer enable %s` to resume it on the old logic: %w", name, err)
	}
	if err := bound(ctx, s.Reset); err != nil {
		return fmt.Errorf("resetting - the projection is stopped on the new query but not rewound; finish the rebuild with `gaffer recreate %s`: %w", name, err)
	}
	if err := bound(ctx, s.Enable); err != nil {
		// State is already wiped and the projection is stopped; no auto-rollback.
		return fmt.Errorf("reset succeeded but the projection failed to restart - run `gaffer enable %s` to rebuild it: %w", name, err)
	}
	return nil
}

// RecreateSteps are the calls of a destroy-and-rebuild, in the order Recreate
// runs them.
type RecreateSteps struct {
	Disable Step
	Delete  Step
	Create  Step
	// RetryCreate, when set, marks a Create error as worth retrying over the
	// settle budget. The server deletes asynchronously - the Delete RPC
	// returns while the name can still be registered - so an immediate
	// Create can bounce off the lingering registration (the projections
	// subsystem replies Conflict; see remote.IsCreateConflict). Optional:
	// nil means one attempt.
	RetryCreate func(error) bool
}

func (s RecreateSteps) validate() error {
	return validateSteps(
		namedStep{"disable", s.Disable},
		namedStep{"delete", s.Delete},
		namedStep{"create", s.Create},
	)
}

// createSettleBudget bounds how long Recreate keeps retrying a Create that
// bounces off the deleted projection's lingering registration, on top of the
// per-attempt RPCTimeout.
const createSettleBudget = 10 * time.Second

// Recreate destroys a projection and rebuilds it from local config: stop it,
// delete it (with its state and checkpoint streams), then create it fresh,
// reprocessing from zero. The destroy precedes the create, so a failure after
// Delete leaves the projection gone: each step names the recovery rather than a
// bare error. There's no auto-rollback. A Create refused while the server is
// still settling the delete retries over createSettleBudget (see
// RecreateSteps.RetryCreate) before giving up with the recovery message.
func Recreate(ctx context.Context, name string, s RecreateSteps) error {
	if err := s.validate(); err != nil {
		return fmt.Errorf("recreate %s: %w", name, err)
	}
	if err := bound(ctx, s.Disable); err != nil {
		return fmt.Errorf("could not disable %s before recreating: %w", name, err)
	}
	if err := bound(ctx, s.Delete); err != nil {
		return fmt.Errorf("could not delete %s before recreating: %w", name, err)
	}
	err := bound(ctx, s.Create)
	if s.RetryCreate != nil {
		deadline := time.Now().Add(createSettleBudget)
		for delay := 250 * time.Millisecond; err != nil && s.RetryCreate(err) && time.Now().Before(deadline); delay = min(delay*2, 2*time.Second) {
			select {
			case <-ctx.Done():
				return fmt.Errorf("%s was deleted but recreating it failed - re-run gaffer recreate %s, or gaffer deploy %s: %w", name, name, name, ctx.Err())
			case <-time.After(delay):
			}
			err = bound(ctx, s.Create)
		}
	}
	if err != nil {
		return fmt.Errorf("%s was deleted but recreating it failed - re-run gaffer recreate %s, or gaffer deploy %s: %w", name, name, name, err)
	}
	return nil
}

// RecreateReason states which create-time field changed, matching gaffer diff's
// "remote X, local Y" phrasing, and points at gaffer recreate (the resolve path,
// a separate verb since it destroys and rebuilds the projection). The deploy plan
// uses it to refuse an in-place change deploy can't apply (engine version or
// track-emitted-streams, both create-only).
func RecreateReason(cmp Comparison, local, deployed Descriptor) string {
	var which string
	switch {
	case cmp.EngineVersionDiffers && cmp.TrackEmittedStreamsDiffers:
		which = fmt.Sprintf("engine version (remote %d, local %d) and track emitted streams (remote %t, local %t)",
			deployed.EngineVersion, local.EngineVersion,
			deployed.TrackEmittedStreams, local.TrackEmittedStreams)
	case cmp.EngineVersionDiffers:
		which = fmt.Sprintf("engine version (remote %d, local %d)", deployed.EngineVersion, local.EngineVersion)
	default:
		which = fmt.Sprintf("track emitted streams (remote %t, local %t)", deployed.TrackEmittedStreams, local.TrackEmittedStreams)
	}
	return which + " can't be changed in place, use gaffer recreate"
}
