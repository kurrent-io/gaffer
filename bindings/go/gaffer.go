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
#include <stdint.h>
#include <stdlib.h>

// The runtime's session handles are small integers (1, 2, 3, ...) cast to
// gaffer_session*, never dereferenced. Go must not hold them in any
// pointer-typed value, even transiently: the GC's precise stack maps treat
// such slots as pointers, and a stack copy - growth, or a shrink during the
// mark phase - aborts the process when it finds a value below the first page
// ("invalid pointer found on stack"). The stack CAN move mid-FFI-call: a
// callback re-enters Go, and while it runs the goroutine is preemptible and
// its stack movable, with the call's argument slots still live below. These
// shims keep the handle uintptr_t on the Go side and cast in C, where the GC
// can't see it.

static uintptr_t go_session_create(const char* source, const char* options_json, const char** error_out) {
	return (uintptr_t)gaffer_session_create(source, options_json, error_out);
}
static void go_session_destroy(uintptr_t s) {
	gaffer_session_destroy((gaffer_session*)s);
}
static const char* go_session_feed(uintptr_t s, const char* event_json, const char** error_out) {
	return gaffer_session_feed((gaffer_session*)s, event_json, error_out);
}
static const char* go_session_get_state(uintptr_t s, const char* partition, const char** error_out) {
	return gaffer_session_get_state((gaffer_session*)s, partition, error_out);
}
static const char* go_session_get_shared_state(uintptr_t s, const char** error_out) {
	return gaffer_session_get_shared_state((gaffer_session*)s, error_out);
}
static void go_session_set_state(uintptr_t s, const char* partition, const char* state_json, const char** error_out) {
	gaffer_session_set_state((gaffer_session*)s, partition, state_json, error_out);
}
static const char* go_session_get_result(uintptr_t s, const char* partition, const char** error_out) {
	return gaffer_session_get_result((gaffer_session*)s, partition, error_out);
}
static const char* go_session_get_sources(uintptr_t s, const char** error_out) {
	return gaffer_session_get_sources((gaffer_session*)s, error_out);
}
static const char* go_session_get_partition_key(uintptr_t s, const char* event_json, const char** error_out) {
	return gaffer_session_get_partition_key((gaffer_session*)s, event_json, error_out);
}
static const char* go_debug_set_breakpoint(uintptr_t s, int line, int column, const char* condition, const char* hit_condition, const char* log_message, const char** error_out) {
	return gaffer_debug_set_breakpoint((gaffer_session*)s, line, column, condition, hit_condition, log_message, error_out);
}
static void go_debug_clear_breakpoints(uintptr_t s, const char** error_out) {
	gaffer_debug_clear_breakpoints((gaffer_session*)s, error_out);
}
static void go_debug_continue(uintptr_t s, const char** error_out) {
	gaffer_debug_continue((gaffer_session*)s, error_out);
}
static void go_debug_pause(uintptr_t s, const char** error_out) {
	gaffer_debug_pause((gaffer_session*)s, error_out);
}
static void go_debug_step_into(uintptr_t s, const char** error_out) {
	gaffer_debug_step_into((gaffer_session*)s, error_out);
}
static void go_debug_step_over(uintptr_t s, const char** error_out) {
	gaffer_debug_step_over((gaffer_session*)s, error_out);
}
static void go_debug_step_out(uintptr_t s, const char** error_out) {
	gaffer_debug_step_out((gaffer_session*)s, error_out);
}
static const char* go_debug_evaluate(uintptr_t s, const char* expression, const char** error_out) {
	return gaffer_debug_evaluate((gaffer_session*)s, expression, error_out);
}
static const char* go_debug_get_call_stack(uintptr_t s, const char** error_out) {
	return gaffer_debug_get_call_stack((gaffer_session*)s, error_out);
}
static const char* go_debug_get_scopes(uintptr_t s, int frame_index, const char** error_out) {
	return gaffer_debug_get_scopes((gaffer_session*)s, frame_index, error_out);
}
static const char* go_debug_get_variables(uintptr_t s, int variables_reference, const char** error_out) {
	return gaffer_debug_get_variables((gaffer_session*)s, variables_reference, error_out);
}
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
	// handle is the runtime session handle: a small integer (1, 2, 3, ...),
	// not a real pointer. It must stay integer-typed everywhere on the Go
	// side - even a transient *C.gaffer_session aborts the process if the
	// GC copies the stack while it's live (see the shim comment in the cgo
	// preamble). The go_* shims cast it in C at the FFI boundary.
	handle uintptr
	source string
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
	handle := C.go_session_create(cs, opts, &cErr)
	if err := consumeError(cErr, source); err != nil {
		return nil, err
	}
	if handle == 0 {
		return nil, &UnexpectedError{Code: "unexpected", Desc: "unknown error", Msg: "unknown error"}
	}
	return &Session{handle: uintptr(handle), source: source}, nil
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
	cleanupCallbacks(s.handle)
	C.go_session_destroy(C.uintptr_t(s.handle))
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
	result := C.go_session_feed(C.uintptr_t(s.handle), cs, &cErr)
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

// GetState returns the current state for a partition, or nil if the partition
// has not been seen. Pass nil for the default (unpartitioned) state. A non-nil
// error means the lookup failed - distinct from a not-seen nil result.
func (s *Session) GetState(partition *string) (*string, error) {
	s.ensureAlive()
	var cp *C.char
	if partition != nil {
		cp = C.CString(*partition)
		defer C.free(unsafe.Pointer(cp))
	}
	var cErr *C.char
	result := C.go_session_get_state(C.uintptr_t(s.handle), cp, &cErr)
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

// GetSharedState returns the shared state for biState projections, or nil if
// none. A non-nil error means the lookup failed - distinct from a nil result.
func (s *Session) GetSharedState() (*string, error) {
	s.ensureAlive()
	var cErr *C.char
	result := C.go_session_get_shared_state(C.uintptr_t(s.handle), &cErr)
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

// SetState restores state for a partition (e.g. from a cache). Pass nil for
// the default partition. Returns an error if the runtime rejects the state -
// previously a failed restore was silent, leaving the projection to compute
// from $init state undetected.
func (s *Session) SetState(partition *string, stateJSON string) error {
	s.ensureAlive()
	var cp *C.char
	if partition != nil {
		cp = C.CString(*partition)
		defer C.free(unsafe.Pointer(cp))
	}
	cs := C.CString(stateJSON)
	defer C.free(unsafe.Pointer(cs))
	var cErr *C.char
	C.go_session_set_state(C.uintptr_t(s.handle), cp, cs, &cErr)
	return consumeError(cErr, s.source)
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
	result := C.go_session_get_result(C.uintptr_t(s.handle), cp, &cErr)
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
	result := C.go_session_get_sources(C.uintptr_t(s.handle), nil)
	defer C.gaffer_free(unsafe.Pointer(result))
	if result == nil {
		return ProjectionInfo{}
	}
	str := C.GoString(result)
	var info ProjectionInfo
	_ = json.Unmarshal([]byte(str), &info)
	return info
}

// GetPartitionKey returns the partition key that would be computed for an
// event, or nil if the projection is unpartitioned. A non-nil error means the
// computation failed (e.g. a throwing partitionBy) - distinct from a nil key.
func (s *Session) GetPartitionKey(eventJSON string) (*string, error) {
	s.ensureAlive()
	defer s.rethrowCallbackPanic()
	cs := C.CString(eventJSON)
	defer C.free(unsafe.Pointer(cs))
	var cErr *C.char
	result := C.go_session_get_partition_key(C.uintptr_t(s.handle), cs, &cErr)
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

// OnEmit registers a callback for emitted events (emit and linkTo).
func (s *Session) OnEmit(cb EmitCallback) {
	sessionOnEmit(s.handle, cb)
}

// OnLog registers a callback for console.log output.
func (s *Session) OnLog(cb LogCallback) {
	sessionOnLog(s.handle, cb)
}

// OnDiagnostic registers a callback for quirks that fire while processing an
// event, invoked at the point each fires. The quirk is also included in the
// feed result's Diagnostics.
func (s *Session) OnDiagnostic(cb DiagnosticCallback) {
	sessionOnDiagnostic(s.handle, cb)
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
	var cErr *C.char
	result := C.go_debug_set_breakpoint(C.uintptr_t(s.handle), C.int(line), C.int(column), cond, hitCond, logMsg, &cErr)
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

// Pause requests a pause before the next event is processed. Returns an error
// if the runtime rejects the request. Panics on a destroyed session, like the
// other methods.
func (s *Session) Pause() error {
	s.ensureAlive()
	var cErr *C.char
	C.go_debug_pause(C.uintptr_t(s.handle), &cErr)
	return consumeError(cErr, s.source)
}

// StepInto steps into the next function call. Only valid while paused; returns
// an error otherwise.
func (s *Session) StepInto() error {
	s.ensureAlive()
	defer s.rethrowCallbackPanic()
	var cErr *C.char
	C.go_debug_step_into(C.uintptr_t(s.handle), &cErr)
	return consumeError(cErr, s.source)
}

// StepOver steps over the next statement. Only valid while paused; returns an
// error otherwise.
func (s *Session) StepOver() error {
	s.ensureAlive()
	defer s.rethrowCallbackPanic()
	var cErr *C.char
	C.go_debug_step_over(C.uintptr_t(s.handle), &cErr)
	return consumeError(cErr, s.source)
}

// StepOut steps out of the current function. Only valid while paused; returns
// an error otherwise.
func (s *Session) StepOut() error {
	s.ensureAlive()
	defer s.rethrowCallbackPanic()
	var cErr *C.char
	C.go_debug_step_out(C.uintptr_t(s.handle), &cErr)
	return consumeError(cErr, s.source)
}

// Evaluate evaluates an expression in the current debug context. Only valid while paused.
func (s *Session) Evaluate(expression string) (*DebugVariable, error) {
	s.ensureAlive()
	defer s.rethrowCallbackPanic()
	cs := C.CString(expression)
	defer C.free(unsafe.Pointer(cs))
	var cErr *C.char
	result := C.go_debug_evaluate(C.uintptr_t(s.handle), cs, &cErr)
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

// ClearBreakpoints removes all breakpoints. Returns an error if the runtime
// rejects the request. Panics on a destroyed session, like the other methods.
func (s *Session) ClearBreakpoints() error {
	s.ensureAlive()
	var cErr *C.char
	C.go_debug_clear_breakpoints(C.uintptr_t(s.handle), &cErr)
	return consumeError(cErr, s.source)
}

// Continue resumes execution after a debug pause. Only valid while paused;
// returns an error otherwise.
func (s *Session) Continue() error {
	s.ensureAlive()
	defer s.rethrowCallbackPanic()
	var cErr *C.char
	C.go_debug_continue(C.uintptr_t(s.handle), &cErr)
	return consumeError(cErr, s.source)
}

// GetCallStack returns the call stack while paused.
func (s *Session) GetCallStack() ([]DebugCallFrame, error) {
	s.ensureAlive()
	defer s.rethrowCallbackPanic()
	var cErr *C.char
	result := C.go_debug_get_call_stack(C.uintptr_t(s.handle), &cErr)
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
	result := C.go_debug_get_scopes(C.uintptr_t(s.handle), C.int(frameIndex), &cErr)
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
	result := C.go_debug_get_variables(C.uintptr_t(s.handle), C.int(variablesReference), &cErr)
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
