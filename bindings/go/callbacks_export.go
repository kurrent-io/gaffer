package gafferruntime

/*
#include "gaffer.h"
*/
import "C"

// The exported trampolines below receive the session handle through C's
// void* user_data as C.uintptr_t, not unsafe.Pointer. The runtime's handles
// are small integers (see Session.handle); received as a pointer they would
// occupy a pointer-typed parameter slot, and the GC's stack scan could abort
// the process ("invalid pointer found on stack") if growth hits the
// trampoline prologue while the value is live. An integer parameter keeps the
// handle out of the stack maps.

//export goEmitCallback
func goEmitCallback(streamID *C.char, eventType *C.char, data *C.char, metadata *C.char, isJson C.int, isLink C.int, userData C.uintptr_t) {
	key := uintptr(userData)
	callbackMu.RLock()
	cb := emitCallbacks[key]
	callbackMu.RUnlock()
	if cb != nil {
		var dataStr, metaStr string
		if data != nil {
			dataStr = C.GoString(data)
		}
		if metadata != nil {
			metaStr = C.GoString(metadata)
		}
		cb(C.GoString(streamID), C.GoString(eventType), dataStr, metaStr, isJson != 0, isLink != 0)
	}
}

//export goLogCallback
func goLogCallback(message *C.char, userData C.uintptr_t) {
	key := uintptr(userData)
	callbackMu.RLock()
	cb := logCallbacks[key]
	callbackMu.RUnlock()
	if cb != nil {
		cb(C.GoString(message))
	}
}

//export goDiagnosticCallback
func goDiagnosticCallback(code *C.char, message *C.char, severity C.int, userData C.uintptr_t) {
	key := uintptr(userData)
	callbackMu.RLock()
	cb := diagnosticCallbacks[key]
	callbackMu.RUnlock()
	if cb != nil {
		cb(Diagnostic{
			Code:     C.GoString(code),
			Message:  C.GoString(message),
			Severity: DiagnosticSeverity(severity),
		})
	}
}

//export goStateChangedCallback
func goStateChangedCallback(partition *C.char, stateJSON *C.char, userData C.uintptr_t) {
	key := uintptr(userData)
	callbackMu.RLock()
	cb := changedCallbacks[key]
	callbackMu.RUnlock()
	if cb != nil {
		var stateStr string
		if stateJSON != nil {
			stateStr = C.GoString(stateJSON)
		}
		cb(C.GoString(partition), stateStr)
	}
}

//export goBreakCallback
func goBreakCallback(reason *C.char, source *C.char, line C.int, column C.int, userData C.uintptr_t) {
	key := uintptr(userData)
	callbackMu.RLock()
	cb := breakCallbacks[key]
	callbackMu.RUnlock()
	if cb != nil {
		cb(BreakInfo{
			Reason: C.GoString(reason),
			Source: C.GoString(source),
			Line:   int(line),
			Column: int(column),
		})
	}
}
