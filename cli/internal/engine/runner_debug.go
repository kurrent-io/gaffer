package engine

import (
	"errors"
	"fmt"
	"maps"

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
		return nil, errors.New("debug not enabled")
	}
	if !r.beginSessionOp() {
		return nil, gafferruntime.ErrSessionDestroyed
	}
	defer r.ops.Done()
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
	if !r.beginSessionOp() {
		return nil
	}
	defer r.ops.Done()
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
	if r.closed {
		// Teardown has begun: the session may already be freed, and there is
		// nothing left to step. No-op, like a step on a never-paused engine.
		r.mu.Unlock()
		return nil
	}
	r.ops.Add(1)
	r.control.paused = false
	r.mu.Unlock()
	defer r.ops.Done()
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

// Pause requests the engine pause at the next step. Like the step verbs it
// funnels through the Runner so the paused bookkeeping has a single owner: the
// request itself does not park the engine, so paused stays false until the
// pause actually lands and the OnBreak handler flips it true. Routing through
// here (rather than the raw session) keeps the DAP/MCP control verbs uniform.
//
// Safe to call when debug is disabled (no-op).
func (r *Runner) Pause() error {
	if r.debug == nil {
		return nil
	}
	if !r.beginSessionOp() {
		return nil
	}
	defer r.ops.Done()
	return r.debug.Session.Pause()
}

// Drain releases a Feed blocked at a breakpoint or the break_at pause so it
// can run to completion and the feed goroutine can exit. It is terminal: it
// sets the draining flag under the lock, so any break that fires afterwards -
// including the asynchronous step the break_at pause converts into, or one
// armed concurrently just as teardown begins - resumes immediately instead of
// re-parking the engine. That closes the window a plain "clear + continue if
// paused" leaves open. Callers use it on cancellation/teardown paths, before
// waiting for the feed goroutine to exit; the session stays readable (state,
// summary) afterwards, it just no longer pauses.
//
// Safe to call when debug is disabled (no-op) or no breakpoint is set.
func (r *Runner) Drain() {
	if !r.beginSessionOp() {
		// Destroy has begun, and its own drain covers the teardown.
		return
	}
	defer r.ops.Done()
	r.drain()
}

// drain is Drain without the session-op registration: Destroy calls it after
// setting closed (which makes beginSessionOp refuse), sequenced on its own
// goroutine before the session is freed.
func (r *Runner) drain() {
	if r.debug == nil {
		return
	}
	r.mu.Lock()
	r.control.draining = true
	r.control.breakAtStep = 0
	wasPaused := r.control.paused
	// Clear paused now we're resuming, so a later Paused() read (e.g. the DAP
	// adapter's ensurePaused guard, or a follow-up Drain) doesn't see a stale
	// true and act on an already running engine.
	r.control.paused = false
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

// Destroy frees the session after draining and waiting out every in-flight
// session-crossing call, an in-flight ProcessOne included (see the Runner
// doc comment's teardown guarantee). Idempotent; concurrent and repeat calls
// no-op. Front-ends should still stop their source loop first as a matter of
// hygiene, but a straggling feed no longer races the teardown.
func (r *Runner) Destroy() {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}
	r.closed = true
	r.mu.Unlock()

	r.drain()
	r.ops.Wait()
	if r.session != nil {
		r.session.Destroy()
	}
	if r.history != nil {
		_ = r.history.Close()
	}
}

// Inspection methods. These cross into the runtime session, which is not
// thread-safe, so they are safe only while the engine is paused at a
// breakpoint (the feed goroutine parked inside Feed). They deliberately take
// no lock - r.mu would not make the FFI call safe and could deadlock against
// the parked feed goroutine. See the Runner doc comment for the full invariant.

func (r *Runner) Evaluate(expression string) (*gafferruntime.DebugVariable, error) {
	if r.debug == nil {
		return nil, errors.New("debug not enabled")
	}
	if !r.beginSessionOp() {
		return nil, gafferruntime.ErrSessionDestroyed
	}
	defer r.ops.Done()
	return r.debug.Session.Evaluate(expression)
}

func (r *Runner) GetCallStack() ([]gafferruntime.DebugCallFrame, error) {
	if r.debug == nil {
		return nil, errors.New("debug not enabled")
	}
	if !r.beginSessionOp() {
		return nil, gafferruntime.ErrSessionDestroyed
	}
	defer r.ops.Done()
	return r.debug.Session.GetCallStack()
}

func (r *Runner) GetScopes(frameID int) ([]gafferruntime.DebugScopeInfo, error) {
	if r.debug == nil {
		return nil, errors.New("debug not enabled")
	}
	if !r.beginSessionOp() {
		return nil, gafferruntime.ErrSessionDestroyed
	}
	defer r.ops.Done()
	return r.debug.Session.GetScopes(frameID)
}

func (r *Runner) GetVariables(variablesReference int) ([]gafferruntime.DebugVariable, error) {
	if r.debug == nil {
		return nil, errors.New("debug not enabled")
	}
	if !r.beginSessionOp() {
		return nil, gafferruntime.ErrSessionDestroyed
	}
	defer r.ops.Done()
	return r.debug.Session.GetVariables(variablesReference)
}

// CollectState reads the per-partition state out of the session. Safe only
// while the engine is paused (see the Runner doc comment): it snapshots the
// partition set under r.mu, then releases the lock before the session FFI call
// so a slow GetResult (which can run V1 transform JS) doesn't block the trivial
// Runner accessors on the feed goroutine's path.
func (r *Runner) CollectState() (StateSummary, error) {
	if r.session == nil {
		return StateSummary{}, nil
	}
	r.mu.Lock()
	if r.closed {
		// Mirrors the nil-session return: after teardown there is no state
		// left to read.
		r.mu.Unlock()
		return StateSummary{}, nil
	}
	r.ops.Add(1)
	partitions := maps.Clone(r.run.partitions)
	r.mu.Unlock()
	defer r.ops.Done()
	return CollectState(r.session, r.info, partitions)
}

// GetPartitionState reads a single partition's state and (if defined) its
// transform result. Safe only while the engine is paused (see the Runner doc
// comment). session and info are immutable after construction, so no lock is
// needed; the call crosses into the session under the pause invariant.
func (r *Runner) GetPartitionState(partition string) (state *string, result *string, err error) {
	if r.session == nil {
		return nil, nil, nil
	}
	if !r.beginSessionOp() {
		// Mirrors the nil-session return, like CollectState.
		return nil, nil, nil
	}
	defer r.ops.Done()
	state, err = r.session.GetState(&partition)
	if err != nil {
		return nil, nil, fmt.Errorf("reading state for partition %q: %w", partition, err)
	}
	if r.info.DefinesStateTransform {
		result, err = r.session.GetResult(&partition)
		if err != nil {
			return nil, nil, fmt.Errorf("reading result for partition %q: %w", partition, err)
		}
	}
	return state, result, nil
}
