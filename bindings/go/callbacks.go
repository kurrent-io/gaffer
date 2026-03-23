package gafferruntime

/*
#include "gaffer.h"

// Forward declarations for Go callback trampolines
extern void goEmitCallback(const char* streamId, const char* eventType, const char* data, const char* metadata, int isJson, int isLink, void* userData);
extern void goLogCallback(const char* message, void* userData);
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
	StateChangedCallback func(partition string, stateJSON string)
)

// Global callback registry keyed by session pointer.
var (
	callbackMu       sync.RWMutex
	emitCallbacks    = make(map[uintptr]EmitCallback)
	logCallbacks     = make(map[uintptr]LogCallback)
	changedCallbacks = make(map[uintptr]StateChangedCallback)
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

func sessionOnStateChanged(session *C.gaffer_session, cb StateChangedCallback) {
	key := sessionKey(session)
	callbackMu.Lock()
	changedCallbacks[key] = cb
	callbackMu.Unlock()
	C.gaffer_on_state_changed(session, (*[0]byte)(C.goStateChangedCallback), unsafe.Pointer(session))
}

func cleanupCallbacks(session *C.gaffer_session) {
	key := sessionKey(session)
	callbackMu.Lock()
	delete(emitCallbacks, key)
	delete(logCallbacks, key)
	delete(changedCallbacks, key)
	callbackMu.Unlock()
}
