package gafferruntime

/*
#include "gaffer.h"

// Forward declarations for Go callback trampolines
extern void goEmitCallback(const char* streamId, const char* eventType, const char* data, const char* metadata, int isJson, int isLink, void* userData);
extern void goLogCallback(const char* message, void* userData);
extern void goSlowHandlerCallback(const char* handlerName, int durationMs, void* userData);
extern void goStateChangedCallback(const char* partition, const char* stateJson, void* userData);
*/
import "C"

import (
	"sync"
	"unsafe"
)

// Callback function types.
type (
	EmitCallback         func(streamID, eventType, data, metadata string, isJson, isLink bool)
	LogCallback          func(message string)
	SlowHandlerCallback  func(handlerName string, durationMs int)
	StateChangedCallback func(partition string, stateJSON string)
)

// Global callback registry keyed by session pointer.
var (
	callbackMu       sync.RWMutex
	emitCallbacks    = make(map[uintptr]EmitCallback)
	logCallbacks     = make(map[uintptr]LogCallback)
	slowCallbacks    = make(map[uintptr]SlowHandlerCallback)
	changedCallbacks = make(map[uintptr]StateChangedCallback)
)

func sessionKey(session *Session) uintptr {
	return uintptr(unsafe.Pointer(session))
}

// SessionOnEmit registers a callback for emitted events (emit and linkTo).
func SessionOnEmit(session *Session, cb EmitCallback) {
	key := sessionKey(session)
	callbackMu.Lock()
	emitCallbacks[key] = cb
	callbackMu.Unlock()
	C.gaffer_on_emit(session, (*[0]byte)(C.goEmitCallback), unsafe.Pointer(session))
}

// SessionOnLog registers a callback for console.log output.
func SessionOnLog(session *Session, cb LogCallback) {
	key := sessionKey(session)
	callbackMu.Lock()
	logCallbacks[key] = cb
	callbackMu.Unlock()
	C.gaffer_on_log(session, (*[0]byte)(C.goLogCallback), unsafe.Pointer(session))
}

// SessionOnSlowHandler registers a callback for slow handler warnings.
func SessionOnSlowHandler(session *Session, cb SlowHandlerCallback) {
	key := sessionKey(session)
	callbackMu.Lock()
	slowCallbacks[key] = cb
	callbackMu.Unlock()
	C.gaffer_on_slow_handler(session, (*[0]byte)(C.goSlowHandlerCallback), unsafe.Pointer(session))
}

// SessionOnStateChanged registers a callback for state changes.
func SessionOnStateChanged(session *Session, cb StateChangedCallback) {
	key := sessionKey(session)
	callbackMu.Lock()
	changedCallbacks[key] = cb
	callbackMu.Unlock()
	C.gaffer_on_state_changed(session, (*[0]byte)(C.goStateChangedCallback), unsafe.Pointer(session))
}

// cleanupCallbacks removes all callbacks for a session.
func cleanupCallbacks(session *Session) {
	key := sessionKey(session)
	callbackMu.Lock()
	delete(emitCallbacks, key)
	delete(logCallbacks, key)
	delete(slowCallbacks, key)
	delete(changedCallbacks, key)
	callbackMu.Unlock()
}
