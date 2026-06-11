// Package gafferruntime provides Go bindings for the Gaffer projection runtime.
//
// The runtime executes KurrentDB projections locally via Jint (a .NET JavaScript
// interpreter). Sessions are not thread-safe - do not call from multiple
// goroutines concurrently on the same session.
package gafferruntime

/*
#cgo CFLAGS: -I${SRCDIR}/../../runtime/include

// Link the runtime AOT into the binary and set the runtime rpath to
// where it lives in shipped packages (next to the executable).
//   Linux:   $ORIGIN
//   macOS:   @loader_path (Apple's ld doesn't accept GNU ld's
//            `-l:filename` extension; pass the dylib by absolute path)
//   Windows: resolves co-located DLLs from the executable's directory
//            by default, so no rpath is needed
// Local dev usually doesn't have the runtime co-located - opt into a
// build-dir rpath via `go build -tags dev` (see gaffer_dev.go).

#cgo linux,amd64   LDFLAGS: -L${SRCDIR}/../../runtime/Gaffer.Runtime/bin/Release/net10.0/linux-x64/publish   -l:Gaffer.Runtime.so    -Wl,-rpath,\$ORIGIN
#cgo linux,arm64   LDFLAGS: -L${SRCDIR}/../../runtime/Gaffer.Runtime/bin/Release/net10.0/linux-arm64/publish -l:Gaffer.Runtime.so    -Wl,-rpath,\$ORIGIN
#cgo darwin,amd64  LDFLAGS: ${SRCDIR}/../../runtime/Gaffer.Runtime/bin/Release/net10.0/osx-x64/publish/Gaffer.Runtime.dylib          -Wl,-rpath,@loader_path
#cgo darwin,arm64  LDFLAGS: ${SRCDIR}/../../runtime/Gaffer.Runtime/bin/Release/net10.0/osx-arm64/publish/Gaffer.Runtime.dylib        -Wl,-rpath,@loader_path
#cgo windows,amd64 LDFLAGS: -L${SRCDIR}/../../runtime/Gaffer.Runtime/bin/Release/net10.0/win-x64/publish    -l:Gaffer.Runtime.dll

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
	// handle is the runtime session handle. The runtime returns small
	// integers (1, 2, 3, ...) cast to a pointer, not real pointers. Stored
	// in a *C.gaffer_session field, the GC's stack scan treats those as
	// pointers and aborts the process when one falls below the first page
	// ("invalid pointer found on stack"). A uintptr is invisible to the GC,
	// so the value is only reconstituted as a pointer transiently, at the
	// FFI call boundary, via c().
	handle uintptr
	source string
}

// c reconstitutes the C session pointer for an FFI call. The handle is an
// opaque integer the runtime never dereferences, so the conversion is safe;
// keeping it in one place keeps the uintptr->pointer cast out of every call
// site (see Session.handle).
func (s *Session) c() *C.gaffer_session {
	//nolint:govet // opaque integer handle, not a real pointer (see Session.handle)
	return (*C.gaffer_session)(unsafe.Pointer(s.handle))
}

// rethrowCallbackPanic re-raises, with its original value, a panic captured
// from a user callback during the FFI call that just returned (see
// recoverCallback). A callback panic surfaces to the caller instead of being
// swallowed at the FFI boundary. The original stack is lost; the value is not.
//
// Invariant: every Session method that can run user projection JS (and so fire
// a callback) must `defer s.rethrowCallbackPanic()`, or a panic stashed during
// its FFI call lingers and is mis-raised on a later call. Destroy is the one
// exception - it drops any stash rather than re-raising during teardown.
func (s *Session) rethrowCallbackPanic() {
	if r, ok := takePanic(s.handle); ok {
		panic(r)
	}
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
	var cErr *C.char
	handle := C.gaffer_session_create(cs, opts, &cErr)
	if err := consumeError(cErr, source); err != nil {
		return nil, err
	}
	if handle == nil {
		return nil, &UnexpectedError{Code: "unexpected", Desc: "unknown error", Msg: "unknown error"}
	}
	return &Session{handle: uintptr(unsafe.Pointer(handle)), source: source}, nil
}

// Destroy frees all resources associated with the session.
func (s *Session) Destroy() {
	if s.handle == 0 {
		return
	}
	// Unregister callbacks before destroying. Tearing down a paused session
	// resumes the engine, which can fire callbacks; with the maps already
	// cleared those are no-ops, so no user code (and no panic) runs during
	// teardown. cleanupCallbacks also drops any stash as a backstop.
	cleanupCallbacks(s.c())
	C.gaffer_session_destroy(s.c())
	s.handle = 0
}

func (s *Session) ensureAlive() {
	if s.handle == 0 {
		panic("use of destroyed session")
	}
}

// Feed sends a single event to the projection and returns the step result.
func (s *Session) Feed(eventJSON string) (*FeedResult, error) {
	s.ensureAlive()
	defer s.rethrowCallbackPanic()
	cs := C.CString(eventJSON)
	defer C.free(unsafe.Pointer(cs))
	var cErr *C.char
	result := C.gaffer_session_feed(s.c(), cs, &cErr)
	defer C.gaffer_free(unsafe.Pointer(result))
	if err := consumeError(cErr, s.source); err != nil {
		return nil, err
	}
	if result == nil {
		return nil, &UnexpectedError{Code: "unexpected", Desc: "unknown error", Msg: "unknown error"}
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
	result := C.gaffer_session_get_state(s.c(), cp, nil)
	defer C.gaffer_free(unsafe.Pointer(result))
	if result == nil {
		return nil
	}
	str := C.GoString(result)
	return &str
}

// GetSharedState returns the shared state for biState projections, or nil.
func (s *Session) GetSharedState() *string {
	s.ensureAlive()
	result := C.gaffer_session_get_shared_state(s.c(), nil)
	defer C.gaffer_free(unsafe.Pointer(result))
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
	C.gaffer_session_set_state(s.c(), cp, cs, nil)
}

// GetResult returns the result for a partition. Under V1, applies any
// registered transformBy/filterBy functions; under V2, returns the
// post-handler state directly (V2 doesn't iterate transforms). Returns nil
// for unknown partitions, or for V1 filtered-out results.
func (s *Session) GetResult(partition *string) (*string, error) {
	s.ensureAlive()
	defer s.rethrowCallbackPanic()
	var cp *C.char
	if partition != nil {
		cp = C.CString(*partition)
		defer C.free(unsafe.Pointer(cp))
	}
	var cErr *C.char
	result := C.gaffer_session_get_result(s.c(), cp, &cErr)
	defer C.gaffer_free(unsafe.Pointer(result))
	if err := consumeError(cErr, s.source); err != nil {
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
	result := C.gaffer_session_get_sources(s.c(), nil)
	defer C.gaffer_free(unsafe.Pointer(result))
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
	defer s.rethrowCallbackPanic()
	cs := C.CString(eventJSON)
	defer C.free(unsafe.Pointer(cs))
	result := C.gaffer_session_get_partition_key(s.c(), cs, nil)
	defer C.gaffer_free(unsafe.Pointer(result))
	if result == nil {
		return nil
	}
	str := C.GoString(result)
	return &str
}

// OnEmit registers a callback for emitted events (emit and linkTo).
func (s *Session) OnEmit(cb EmitCallback) {
	sessionOnEmit(s.c(), cb)
}

// OnLog registers a callback for console.log output.
func (s *Session) OnLog(cb LogCallback) {
	sessionOnLog(s.c(), cb)
}

// OnDiagnostic registers a callback for quirks that fire while processing an
// event, invoked at the point each fires. The quirk is also included in the
// feed result's Diagnostics.
func (s *Session) OnDiagnostic(cb DiagnosticCallback) {
	sessionOnDiagnostic(s.c(), cb)
}

// OnStateChanged registers a callback for state changes.
func (s *Session) OnStateChanged(cb StateChangedCallback) {
	sessionOnStateChanged(s.c(), cb)
}

// OnBreak registers a callback for debug pause notifications.
func (s *Session) OnBreak(cb BreakCallback) {
	sessionOnBreak(s.c(), cb)
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
	var cErr *C.char
	result := C.gaffer_debug_set_breakpoint(s.c(), C.int(line), C.int(column), cond, hitCond, logMsg, &cErr)
	defer C.gaffer_free(unsafe.Pointer(result))
	if err := consumeError(cErr, s.source); err != nil {
		return nil, err
	}
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
	C.gaffer_debug_pause(s.c(), nil)
}

// StepInto steps into the next function call. Only valid while paused.
func (s *Session) StepInto() {
	s.ensureAlive()
	defer s.rethrowCallbackPanic()
	C.gaffer_debug_step_into(s.c(), nil)
}

// StepOver steps over the next statement. Only valid while paused.
func (s *Session) StepOver() {
	s.ensureAlive()
	defer s.rethrowCallbackPanic()
	C.gaffer_debug_step_over(s.c(), nil)
}

// StepOut steps out of the current function. Only valid while paused.
func (s *Session) StepOut() {
	s.ensureAlive()
	defer s.rethrowCallbackPanic()
	C.gaffer_debug_step_out(s.c(), nil)
}

// Evaluate evaluates an expression in the current debug context. Only valid while paused.
func (s *Session) Evaluate(expression string) (*DebugVariable, error) {
	s.ensureAlive()
	defer s.rethrowCallbackPanic()
	cs := C.CString(expression)
	defer C.free(unsafe.Pointer(cs))
	var cErr *C.char
	result := C.gaffer_debug_evaluate(s.c(), cs, &cErr)
	defer C.gaffer_free(unsafe.Pointer(result))
	if err := consumeError(cErr, s.source); err != nil {
		return nil, err
	}
	if result == nil {
		return nil, &UnexpectedError{Code: "unexpected", Desc: "unknown error", Msg: "unknown error"}
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
	C.gaffer_debug_clear_breakpoints(s.c(), nil)
}

// Continue resumes execution after a debug pause.
func (s *Session) Continue() {
	s.ensureAlive()
	defer s.rethrowCallbackPanic()
	C.gaffer_debug_continue(s.c(), nil)
}

// GetCallStack returns the call stack while paused.
func (s *Session) GetCallStack() ([]DebugCallFrame, error) {
	s.ensureAlive()
	defer s.rethrowCallbackPanic()
	var cErr *C.char
	result := C.gaffer_debug_get_call_stack(s.c(), &cErr)
	defer C.gaffer_free(unsafe.Pointer(result))
	if err := consumeError(cErr, s.source); err != nil {
		return nil, err
	}
	if result == nil {
		return nil, &UnexpectedError{Code: "unexpected", Desc: "unknown error", Msg: "unknown error"}
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
	defer s.rethrowCallbackPanic()
	var cErr *C.char
	result := C.gaffer_debug_get_scopes(s.c(), C.int(frameIndex), &cErr)
	defer C.gaffer_free(unsafe.Pointer(result))
	if err := consumeError(cErr, s.source); err != nil {
		return nil, err
	}
	if result == nil {
		return nil, &UnexpectedError{Code: "unexpected", Desc: "unknown error", Msg: "unknown error"}
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
	defer s.rethrowCallbackPanic()
	var cErr *C.char
	result := C.gaffer_debug_get_variables(s.c(), C.int(variablesReference), &cErr)
	defer C.gaffer_free(unsafe.Pointer(result))
	if err := consumeError(cErr, s.source); err != nil {
		return nil, err
	}
	if result == nil {
		return nil, &UnexpectedError{Code: "unexpected", Desc: "unknown error", Msg: "unknown error"}
	}
	var vars []DebugVariable
	if err := json.Unmarshal([]byte(C.GoString(result)), &vars); err != nil {
		return nil, fmt.Errorf("failed to parse variables: %w", err)
	}
	return vars, nil
}
