package engine

import (
	"errors"
	"fmt"

	gafferruntime "github.com/kurrent-io/gaffer/bindings/go"
)

type DebugConfig struct {
	Session *gafferruntime.Session
	Info    gafferruntime.ProjectionInfo
	OnBreak func(gafferruntime.BreakInfo) // must not call Runner methods
	// OnError, if set, receives errors from debug-control commands the runner
	// issues on its own goroutines (the internal auto-step to a break_at
	// target in NewRunner's OnBreak) - the one debug-control path where a
	// returned error has no caller to receive it. Interactive step/continue
	// errors return synchronously to their callers instead.
	OnError func(error)
}

type Breakpoint struct {
	Line      int
	Column    int
	Condition string
}

// SetBreakpoints clears existing breakpoints and sets new ones.
func (r *Runner) SetBreakpoints(breakpoints []Breakpoint) ([]*gafferruntime.SnappedBreakpoint, error) {
	if r.debug == nil {
		return nil, fmt.Errorf("debug not enabled")
	}
	if err := r.debug.Session.ClearBreakpoints(); err != nil {
		return nil, fmt.Errorf("clearing breakpoints: %w", err)
	}
	snapped := make([]*gafferruntime.SnappedBreakpoint, len(breakpoints))
	for i, bp := range breakpoints {
		var opts *gafferruntime.BreakpointOptions
		if bp.Condition != "" {
			opts = &gafferruntime.BreakpointOptions{Condition: bp.Condition}
		}
		s, err := r.debug.Session.SetBreakpoint(bp.Line, bp.Column, opts)
		if err != nil {
			return nil, fmt.Errorf("setting breakpoint at line %d: %w", bp.Line, err)
		}
		snapped[i] = s
	}
	return snapped, nil
}

func (r *Runner) ClearBreakpoints() error {
	if r.debug == nil {
		return nil
	}
	return r.debug.Session.ClearBreakpoints()
}

func (r *Runner) doStep(fn func() error) error {
	if r.debug == nil {
		return nil
	}
	// Flip paused optimistically, before issuing the command. The only error
	// fn can return is the runtime's "not paused" - which means the engine
	// wasn't paused anyway, so false is the correct resulting state. Leaving it
	// false on error also lets the caller's next Paused() guard reject cleanly
	// rather than re-entering here and failing again.
	r.mu.Lock()
	r.paused = false
	r.mu.Unlock()
	return fn()
}

func (r *Runner) Continue() error {
	return r.doStep(func() error { return r.debug.Session.Continue() })
}

func (r *Runner) StepOver() error {
	return r.doStep(func() error { return r.debug.Session.StepOver() })
}

func (r *Runner) StepInto() error {
	return r.doStep(func() error { return r.debug.Session.StepInto() })
}

func (r *Runner) StepOut() error {
	return r.doStep(func() error { return r.debug.Session.StepOut() })
}

// Unblock releases the JS thread if it's paused at a breakpoint so a
// blocked ProcessOne can return. Use this on cancellation paths where
// the caller needs the source loop to exit but still wants to read
// state, write a summary, etc. against a live session afterwards.
//
// Safe to call when no breakpoints are set or the runner isn't paused.
// Safe to call when debug is disabled (no-op).
//
// Best-effort: it always attempts the Continue that actually releases a paused
// Feed even if clearing breakpoints failed, and joins both errors. Cancellation
// and teardown callers generally ignore the result - the session is going away -
// but it's returned rather than swallowed so a caller that cares can see it.
func (r *Runner) Unblock() error {
	if r.debug == nil {
		return nil
	}
	// Clear paused before resuming, like doStep and Drain, so a later
	// Paused() read (the DAP adapter's ensurePaused guard, a follow-up
	// Destroy->Unblock) doesn't see a stale true and act on an already
	// running engine.
	r.mu.Lock()
	wasPaused := r.paused
	r.paused = false
	r.mu.Unlock()
	clearErr := r.debug.Session.ClearBreakpoints()
	var contErr error
	if wasPaused {
		contErr = r.debug.Session.Continue()
	}
	return errors.Join(clearErr, contErr)
}

// Drain forces a debugging session to run to completion so a Feed blocked
// at a breakpoint or the break_at pause can return. Unlike Unblock it is
// terminal: it sets the draining flag, so any break that fires afterwards -
// including the asynchronous step the break_at pause converts into, or one
// armed just as teardown begins - resumes immediately instead of re-parking
// the engine. Callers use it before waiting for the feed goroutine to exit.
//
// Safe to call when debug is disabled (no-op) or no breakpoint is set.
func (r *Runner) Drain() {
	if r.debug == nil {
		return
	}
	r.mu.Lock()
	r.draining = true
	r.breakAtStep = 0
	wasPaused := r.paused
	// Clear paused now we're resuming, so a later Paused() read (e.g.
	// Destroy -> Unblock) doesn't issue a second, spurious Continue.
	r.paused = false
	r.mu.Unlock()

	// Errors are discarded: Drain is terminal teardown, so there's nothing
	// actionable to report.
	_ = r.debug.Session.ClearBreakpoints()
	// Resume an engine already parked at a break. A break that lands after
	// this (the in-flight step from a break_at pause) is resumed by the
	// draining branch in the OnBreak handler instead.
	if wasPaused {
		_ = r.debug.Session.Continue()
	}
}

func (r *Runner) Destroy() {
	_ = r.Unblock()
	if r.session != nil {
		r.session.Destroy()
	}
	if r.history != nil {
		_ = r.history.Close()
	}
}

// Inspection methods - safe to call while paused at a breakpoint

func (r *Runner) Evaluate(expression string) (*gafferruntime.DebugVariable, error) {
	if r.debug == nil {
		return nil, fmt.Errorf("debug not enabled")
	}
	return r.debug.Session.Evaluate(expression)
}

func (r *Runner) GetCallStack() ([]gafferruntime.DebugCallFrame, error) {
	if r.debug == nil {
		return nil, fmt.Errorf("debug not enabled")
	}
	return r.debug.Session.GetCallStack()
}

func (r *Runner) GetScopes(frameID int) ([]gafferruntime.DebugScopeInfo, error) {
	if r.debug == nil {
		return nil, fmt.Errorf("debug not enabled")
	}
	return r.debug.Session.GetScopes(frameID)
}

func (r *Runner) GetVariables(variablesReference int) ([]gafferruntime.DebugVariable, error) {
	if r.debug == nil {
		return nil, fmt.Errorf("debug not enabled")
	}
	return r.debug.Session.GetVariables(variablesReference)
}

func (r *Runner) CollectState() StateSummary {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.collectStateLocked()
}

func (r *Runner) collectStateLocked() StateSummary {
	if r.session == nil {
		return StateSummary{}
	}
	return CollectState(r.session, r.info, r.partitions)
}

func (r *Runner) GetPartitionState(partition string) (state *string, result *string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.session == nil {
		return nil, nil
	}
	state, _ = r.session.GetState(&partition)
	if r.info.DefinesStateTransform {
		result, _ = r.session.GetResult(&partition)
	}
	return state, result
}
