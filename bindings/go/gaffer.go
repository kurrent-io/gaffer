// Package gafferruntime provides Go bindings for the Gaffer projection runtime.
//
// The runtime executes KurrentDB projections locally via Jint (a .NET JavaScript
// interpreter). Sessions are not thread-safe - do not call from multiple
// goroutines concurrently on the same session.
package gafferruntime

/*
#cgo CFLAGS: -I${SRCDIR}/../../runtime/include
#cgo LDFLAGS: -L${SRCDIR}/../../runtime/Gaffer.Runtime/bin/Release/net10.0/linux-x64/publish -l:Gaffer.Runtime.so -Wl,-rpath,${SRCDIR}/../../runtime/Gaffer.Runtime/bin/Release/net10.0/linux-x64/publish
#include "gaffer.h"
#include <stdlib.h>
*/
import "C"

import (
	"encoding/json"
	"fmt"
	"unsafe"
)

// Session wraps a projection runtime session.
// Not thread-safe - do not use from multiple goroutines concurrently.
type Session struct {
	handle *C.gaffer_session
	source string
}

// NewSession compiles a projection from JavaScript source and returns a session.
func NewSession(source string, optionsJSON *string) (*Session, error) {
	cs := C.CString(source)
	defer C.free(unsafe.Pointer(cs))
	var opts *C.char
	if optionsJSON != nil {
		opts = C.CString(*optionsJSON)
		defer C.free(unsafe.Pointer(opts))
	}
	handle := C.gaffer_session_create(cs, opts)
	if handle == nil {
		return nil, getLastError(source)
	}
	return &Session{handle: handle, source: source}, nil
}

// Destroy frees all resources associated with the session.
func (s *Session) Destroy() {
	if s.handle == nil {
		return
	}
	cleanupCallbacks(s.handle)
	C.gaffer_session_destroy(s.handle)
	s.handle = nil
}

func (s *Session) ensureAlive() {
	if s.handle == nil {
		panic("use of destroyed session")
	}
}

// Feed sends a single event to the projection and returns the step result.
func (s *Session) Feed(eventJSON string) (*FeedResult, error) {
	s.ensureAlive()
	cs := C.CString(eventJSON)
	defer C.free(unsafe.Pointer(cs))
	result := C.gaffer_session_feed(s.handle, cs)
	if result == nil {
		return nil, getLastError(s.source)
	}
	var fr FeedResult
	if err := json.Unmarshal([]byte(C.GoString(result)), &fr); err != nil {
		return nil, fmt.Errorf("failed to parse feed result: %w", err)
	}
	return &fr, nil
}

// GetState returns the current state for a partition, or nil if the
// partition has not been seen. Pass nil for the default (unpartitioned) state.
func (s *Session) GetState(partition *string) *string {
	s.ensureAlive()
	var cp *C.char
	if partition != nil {
		cp = C.CString(*partition)
		defer C.free(unsafe.Pointer(cp))
	}
	result := C.gaffer_session_get_state(s.handle, cp)
	if result == nil {
		return nil
	}
	str := C.GoString(result)
	return &str
}

// GetSharedState returns the shared state for biState projections, or nil.
func (s *Session) GetSharedState() *string {
	s.ensureAlive()
	result := C.gaffer_session_get_shared_state(s.handle)
	if result == nil {
		return nil
	}
	str := C.GoString(result)
	return &str
}

// SetState restores state for a partition (e.g. from a cache).
// Pass nil for the default partition.
func (s *Session) SetState(partition *string, stateJSON string) {
	s.ensureAlive()
	var cp *C.char
	if partition != nil {
		cp = C.CString(*partition)
		defer C.free(unsafe.Pointer(cp))
	}
	cs := C.CString(stateJSON)
	defer C.free(unsafe.Pointer(cs))
	C.gaffer_session_set_state(s.handle, cp, cs)
}

// GetResult returns the transformed result for a partition (applies
// transformBy/filterBy). Returns nil for unknown partitions or filtered results.
func (s *Session) GetResult(partition *string) (*string, error) {
	s.ensureAlive()
	var cp *C.char
	if partition != nil {
		cp = C.CString(*partition)
		defer C.free(unsafe.Pointer(cp))
	}
	result := C.gaffer_session_get_result(s.handle, cp)
	if err := checkLastError(s.source); err != nil {
		return nil, err
	}
	if result == nil {
		return nil, nil
	}
	str := C.GoString(result)
	return &str, nil
}

// GetSources returns the projection's source configuration and features.
func (s *Session) GetSources() ProjectionInfo {
	s.ensureAlive()
	result := C.gaffer_session_get_sources(s.handle)
	if result == nil {
		return ProjectionInfo{}
	}
	str := C.GoString(result)
	var info ProjectionInfo
	_ = json.Unmarshal([]byte(str), &info)
	return info
}

// GetPartitionKey returns the partition key that would be computed for an event.
func (s *Session) GetPartitionKey(eventJSON string) *string {
	s.ensureAlive()
	cs := C.CString(eventJSON)
	defer C.free(unsafe.Pointer(cs))
	result := C.gaffer_session_get_partition_key(s.handle, cs)
	if result == nil {
		return nil
	}
	str := C.GoString(result)
	return &str
}

// OnEmit registers a callback for emitted events (emit and linkTo).
func (s *Session) OnEmit(cb EmitCallback) {
	sessionOnEmit(s.handle, cb)
}

// OnLog registers a callback for console.log output.
func (s *Session) OnLog(cb LogCallback) {
	sessionOnLog(s.handle, cb)
}

// OnStateChanged registers a callback for state changes.
func (s *Session) OnStateChanged(cb StateChangedCallback) {
	sessionOnStateChanged(s.handle, cb)
}

// OnBreak registers a callback for debug pause notifications.
func (s *Session) OnBreak(cb BreakCallback) {
	sessionOnBreak(s.handle, cb)
}

// SnappedBreakpoint is the actual position where a breakpoint was set after snapping.
type SnappedBreakpoint struct {
	Line   int `json:"line"`
	Column int `json:"column"`
}

// SetBreakpoint sets a breakpoint, snapping to the nearest breakable position.
// Column is accepted for future column-level breakpoints but currently only line is used for snapping.
// Returns the actual position (1-based) or nil if no breakable position was found.
// BreakpointOptions configures a breakpoint.
type BreakpointOptions struct {
	Condition    string // JS expression; only pauses if truthy. Empty for unconditional.
	HitCondition string // Hit count condition like ">= 5", "% 3". Empty to ignore.
	LogMessage   string // Log template with {expr}. When set, logs instead of pausing.
}

func (s *Session) SetBreakpoint(line, column int, opts *BreakpointOptions) (*SnappedBreakpoint, error) {
	s.ensureAlive()
	var cond, hitCond, logMsg *C.char
	if opts != nil {
		if opts.Condition != "" {
			cond = C.CString(opts.Condition)
			defer C.free(unsafe.Pointer(cond))
		}
		if opts.HitCondition != "" {
			hitCond = C.CString(opts.HitCondition)
			defer C.free(unsafe.Pointer(hitCond))
		}
		if opts.LogMessage != "" {
			logMsg = C.CString(opts.LogMessage)
			defer C.free(unsafe.Pointer(logMsg))
		}
	}
	result := C.gaffer_debug_set_breakpoint(s.handle, C.int(line), C.int(column), cond, hitCond, logMsg)
	if result == nil {
		return nil, nil
	}
	var snapped SnappedBreakpoint
	if err := json.Unmarshal([]byte(C.GoString(result)), &snapped); err != nil {
		return nil, fmt.Errorf("failed to parse snapped breakpoint: %w", err)
	}
	return &snapped, nil
}

// Pause requests a pause before the next event is processed.
func (s *Session) Pause() {
	s.ensureAlive()
	C.gaffer_debug_pause(s.handle)
}

// StepInto steps into the next function call. Only valid while paused.
func (s *Session) StepInto() {
	s.ensureAlive()
	C.gaffer_debug_step_into(s.handle)
}

// StepOver steps over the next statement. Only valid while paused.
func (s *Session) StepOver() {
	s.ensureAlive()
	C.gaffer_debug_step_over(s.handle)
}

// StepOut steps out of the current function. Only valid while paused.
func (s *Session) StepOut() {
	s.ensureAlive()
	C.gaffer_debug_step_out(s.handle)
}

// Evaluate evaluates an expression in the current debug context. Only valid while paused.
func (s *Session) Evaluate(expression string) (*DebugVariable, error) {
	s.ensureAlive()
	cs := C.CString(expression)
	defer C.free(unsafe.Pointer(cs))
	result := C.gaffer_debug_evaluate(s.handle, cs)
	if result == nil {
		return nil, getLastError(s.source)
	}
	var v DebugVariable
	if err := json.Unmarshal([]byte(C.GoString(result)), &v); err != nil {
		return nil, fmt.Errorf("failed to parse evaluate result: %w", err)
	}
	return &v, nil
}

// ClearBreakpoints removes all breakpoints.
func (s *Session) ClearBreakpoints() {
	s.ensureAlive()
	C.gaffer_debug_clear_breakpoints(s.handle)
}

// Continue resumes execution after a debug pause.
func (s *Session) Continue() {
	s.ensureAlive()
	C.gaffer_debug_continue(s.handle)
}

// GetCallStack returns the call stack while paused.
func (s *Session) GetCallStack() ([]DebugCallFrame, error) {
	s.ensureAlive()
	result := C.gaffer_debug_get_call_stack(s.handle)
	if result == nil {
		return nil, getLastError(s.source)
	}
	var frames []DebugCallFrame
	if err := json.Unmarshal([]byte(C.GoString(result)), &frames); err != nil {
		return nil, fmt.Errorf("failed to parse call stack: %w", err)
	}
	return frames, nil
}

// GetScopes returns the scopes for a call frame while paused.
func (s *Session) GetScopes(frameIndex int) ([]DebugScopeInfo, error) {
	s.ensureAlive()
	result := C.gaffer_debug_get_scopes(s.handle, C.int(frameIndex))
	if result == nil {
		return nil, getLastError(s.source)
	}
	var scopes []DebugScopeInfo
	if err := json.Unmarshal([]byte(C.GoString(result)), &scopes); err != nil {
		return nil, fmt.Errorf("failed to parse scopes: %w", err)
	}
	return scopes, nil
}

// GetVariables returns the variables for a scope or object reference while paused.
func (s *Session) GetVariables(variablesReference int) ([]DebugVariable, error) {
	s.ensureAlive()
	result := C.gaffer_debug_get_variables(s.handle, C.int(variablesReference))
	if result == nil {
		return nil, getLastError(s.source)
	}
	var vars []DebugVariable
	if err := json.Unmarshal([]byte(C.GoString(result)), &vars); err != nil {
		return nil, fmt.Errorf("failed to parse variables: %w", err)
	}
	return vars, nil
}
