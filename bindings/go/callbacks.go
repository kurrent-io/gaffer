package gafferruntime

/*
#include "gaffer.h"

// Forward declarations for Go callback trampolines
extern void goEmitCallback(const char* streamId, const char* eventType, const char* data, const char* metadata, int isJson, int isLink, void* userData);
extern void goLogCallback(const char* message, void* userData);
extern void goDiagnosticCallback(const char* code, const char* message, int severity, void* userData);
extern void goStateChangedCallback(const char* partition, const char* stateJson, void* userData);
extern void goBreakCallback(const char* reason, const char* source, int line, int column, void* userData);
*/
import "C"

import (
	"sync"
	"sync/atomic"
	"unsafe"
)

// Callback function types.
type (
	EmitCallback         func(streamID, eventType, data, metadata string, isJson, isLink bool)
	LogCallback          func(message string)
	DiagnosticCallback   func(d Diagnostic)
	StateChangedCallback func(partition string, stateJSON string)
	BreakCallback        func(info BreakInfo)
)

// Global callback registry keyed by session pointer.
var (
	callbackMu          sync.RWMutex
	emitCallbacks       = make(map[uintptr]EmitCallback)
	logCallbacks        = make(map[uintptr]LogCallback)
	diagnosticCallbacks = make(map[uintptr]DiagnosticCallback)
	changedCallbacks    = make(map[uintptr]StateChangedCallback)
	breakCallbacks      = make(map[uintptr]BreakCallback)
)

func sessionKey(session *C.gaffer_session) uintptr {
	return uintptr(unsafe.Pointer(session))
}

func sessionOnEmit(session *C.gaffer_session, cb EmitCallback) {
	key := sessionKey(session)
	callbackMu.Lock()
	emitCallbacks[key] = cb
	callbackMu.Unlock()
	C.gaffer_on_emit(session, (*[0]byte)(C.goEmitCallback), unsafe.Pointer(session))
}

func sessionOnLog(session *C.gaffer_session, cb LogCallback) {
	key := sessionKey(session)
	callbackMu.Lock()
	logCallbacks[key] = cb
	callbackMu.Unlock()
	C.gaffer_on_log(session, (*[0]byte)(C.goLogCallback), unsafe.Pointer(session))
}

func sessionOnDiagnostic(session *C.gaffer_session, cb DiagnosticCallback) {
	key := sessionKey(session)
	callbackMu.Lock()
	diagnosticCallbacks[key] = cb
	callbackMu.Unlock()
	C.gaffer_on_diagnostic(session, (*[0]byte)(C.goDiagnosticCallback), unsafe.Pointer(session))
}

func sessionOnStateChanged(session *C.gaffer_session, cb StateChangedCallback) {
	key := sessionKey(session)
	callbackMu.Lock()
	changedCallbacks[key] = cb
	callbackMu.Unlock()
	C.gaffer_on_state_changed(session, (*[0]byte)(C.goStateChangedCallback), unsafe.Pointer(session))
}

func sessionOnBreak(session *C.gaffer_session, cb BreakCallback) {
	key := sessionKey(session)
	callbackMu.Lock()
	breakCallbacks[key] = cb
	callbackMu.Unlock()
	C.gaffer_on_break(session, (*[0]byte)(C.goBreakCallback), unsafe.Pointer(session))
}

func cleanupCallbacks(session *C.gaffer_session) {
	key := sessionKey(session)
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
