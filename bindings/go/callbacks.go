package gafferruntime

/*
#include "gaffer.h"
#include <stdint.h>

// Forward declarations for Go callback trampolines. user_data is declared
// uintptr_t to match the //export signatures: it carries the session handle,
// a small integer that must never appear pointer-typed on the Go side (see
// the shim comment in gaffer.go's preamble).
extern void goEmitCallback(const char* streamId, const char* eventType, const char* data, const char* metadata, int isJson, int isLink, uintptr_t userData);
extern void goLogCallback(const char* message, uintptr_t userData);
extern void goDiagnosticCallback(const char* code, const char* message, int severity, uintptr_t userData);
extern void goStateChangedCallback(const char* partition, const char* stateJson, uintptr_t userData);
extern void goBreakCallback(const char* reason, const char* source, int line, int column, uintptr_t userData);

// Thunks matching the gaffer_*_cb typedefs exactly. The void* -> uintptr_t
// conversion happens here on the user_data VALUE; casting the Go trampolines
// (uintptr_t last parameter) to the void*-taking typedefs instead would call
// through an incompatible function-pointer type, which is undefined behavior.
static void go_emit_thunk(const char* streamId, const char* eventType, const char* data, const char* metadata, int isJson, int isLink, void* userData) {
	goEmitCallback(streamId, eventType, data, metadata, isJson, isLink, (uintptr_t)userData);
}
static void go_log_thunk(const char* message, void* userData) {
	goLogCallback(message, (uintptr_t)userData);
}
static void go_diagnostic_thunk(const char* code, const char* message, int severity, void* userData) {
	goDiagnosticCallback(code, message, severity, (uintptr_t)userData);
}
static void go_state_changed_thunk(const char* partition, const char* stateJson, void* userData) {
	goStateChangedCallback(partition, stateJson, (uintptr_t)userData);
}
static void go_break_thunk(const char* reason, const char* source, int line, int column, void* userData) {
	goBreakCallback(reason, source, line, column, (uintptr_t)userData);
}

// Registration shims: cast the integer handle to gaffer_session* (and back
// into the user_data slot) in C, so no fake pointer transits Go stack slots.
static void go_on_emit(uintptr_t s) {
	gaffer_on_emit((gaffer_session*)s, go_emit_thunk, (void*)s);
}
static void go_on_log(uintptr_t s) {
	gaffer_on_log((gaffer_session*)s, go_log_thunk, (void*)s);
}
static void go_on_diagnostic(uintptr_t s) {
	gaffer_on_diagnostic((gaffer_session*)s, go_diagnostic_thunk, (void*)s);
}
static void go_on_state_changed(uintptr_t s) {
	gaffer_on_state_changed((gaffer_session*)s, go_state_changed_thunk, (void*)s);
}
static void go_on_break(uintptr_t s) {
	gaffer_on_break((gaffer_session*)s, go_break_thunk, (void*)s);
}
*/
import "C"

import (
	"sync"
	"sync/atomic"
)

// Callback function types.
type (
	EmitCallback         func(streamID, eventType, data, metadata string, isJson, isLink bool)
	LogCallback          func(message string)
	DiagnosticCallback   func(d Diagnostic)
	StateChangedCallback func(partition string, stateJSON string)
	BreakCallback        func(info BreakInfo)
)

// Global callback registry keyed by session handle.
var (
	callbackMu          sync.RWMutex
	emitCallbacks       = make(map[uintptr]EmitCallback)
	logCallbacks        = make(map[uintptr]LogCallback)
	diagnosticCallbacks = make(map[uintptr]DiagnosticCallback)
	changedCallbacks    = make(map[uintptr]StateChangedCallback)
	breakCallbacks      = make(map[uintptr]BreakCallback)
)

func sessionOnEmit(handle uintptr, cb EmitCallback) {
	callbackMu.Lock()
	emitCallbacks[handle] = cb
	callbackMu.Unlock()
	C.go_on_emit(C.uintptr_t(handle))
}

func sessionOnLog(handle uintptr, cb LogCallback) {
	callbackMu.Lock()
	logCallbacks[handle] = cb
	callbackMu.Unlock()
	C.go_on_log(C.uintptr_t(handle))
}

func sessionOnDiagnostic(handle uintptr, cb DiagnosticCallback) {
	callbackMu.Lock()
	diagnosticCallbacks[handle] = cb
	callbackMu.Unlock()
	C.go_on_diagnostic(C.uintptr_t(handle))
}

func sessionOnStateChanged(handle uintptr, cb StateChangedCallback) {
	callbackMu.Lock()
	changedCallbacks[handle] = cb
	callbackMu.Unlock()
	C.go_on_state_changed(C.uintptr_t(handle))
}

func sessionOnBreak(handle uintptr, cb BreakCallback) {
	callbackMu.Lock()
	breakCallbacks[handle] = cb
	callbackMu.Unlock()
	C.go_on_break(C.uintptr_t(handle))
}

func cleanupCallbacks(key uintptr) {
	callbackMu.Lock()
	delete(emitCallbacks, key)
	delete(logCallbacks, key)
	delete(diagnosticCallbacks, key)
	delete(changedCallbacks, key)
	delete(breakCallbacks, key)
	callbackMu.Unlock()
	takePanic(key)
}

// Panic handoff from callback trampolines.
//
// User callbacks run synchronously inside an FFI call, on the runtime's .NET
// NativeAOT frames. A panic must not unwind through those frames: it would skip
// the cleanup in their finally blocks and abandon the reverse-pinvoke frame
// mid-flight (undefined behaviour for the .NET runtime). Each trampoline defers
// recoverCallback to capture the panic and stash it under the session handle;
// the Session method that drove the FFI call re-raises it on the Go side once
// the call has returned (see Session.rethrowCallbackPanic).
var (
	panicMu       sync.Mutex
	pendingPanics = make(map[uintptr]any)
	pendingPanicN atomic.Int64 // fast path: skip the lock when no panic is stashed
)

// recoverCallback captures a panic from the user callback that was just
// invoked. Deferred at the top of each trampoline. The first panic per session
// wins; later callbacks in the same FFI call still run but cannot overwrite it.
func recoverCallback(key uintptr) {
	r := recover()
	if r == nil {
		return
	}
	panicMu.Lock()
	if _, exists := pendingPanics[key]; !exists {
		pendingPanics[key] = r
		pendingPanicN.Add(1)
	}
	panicMu.Unlock()
}

// takePanic removes and returns the stashed callback panic for the session.
func takePanic(key uintptr) (any, bool) {
	if pendingPanicN.Load() == 0 {
		return nil, false
	}
	panicMu.Lock()
	r, ok := pendingPanics[key]
	if ok {
		delete(pendingPanics, key)
		pendingPanicN.Add(-1)
	}
	panicMu.Unlock()
	return r, ok
}
