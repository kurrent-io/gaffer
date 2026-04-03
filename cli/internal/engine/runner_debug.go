package engine

import (
	"fmt"

	gafferruntime "github.com/kurrent-io/gaffer/bindings/go"
)

type DebugConfig struct {
	Session *gafferruntime.Session
	Info    gafferruntime.QuerySources
	OnBreak func(gafferruntime.BreakInfo) // must not call Runner methods
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
	r.debug.Session.ClearBreakpoints()
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

func (r *Runner) ClearBreakpoints() {
	if r.debug != nil {
		r.debug.Session.ClearBreakpoints()
	}
}

func (r *Runner) doStep(fn func()) {
	if r.debug == nil {
		return
	}
	r.mu.Lock()
	r.paused = false
	r.mu.Unlock()
	fn()
}

func (r *Runner) Continue() { r.doStep(func() { r.debug.Session.Continue() }) }
func (r *Runner) StepOver() { r.doStep(func() { r.debug.Session.StepOver() }) }
func (r *Runner) StepInto() { r.doStep(func() { r.debug.Session.StepInto() }) }
func (r *Runner) StepOut()  { r.doStep(func() { r.debug.Session.StepOut() }) }

func (r *Runner) Destroy() {
	if r.debug != nil {
		r.debug.Session.ClearBreakpoints()
		if r.Paused() {
			r.debug.Session.Continue()
		}
	}
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
	state = r.session.GetState(&partition)
	if r.info.DefinesStateTransform {
		result, _ = r.session.GetResult(&partition)
	}
	return state, result
}
