package gafferruntime

import "C"
import "unsafe"

//export goEmitCallback
func goEmitCallback(streamID *C.char, eventType *C.char, data *C.char, metadata *C.char, userData unsafe.Pointer) {
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
		cb(C.GoString(streamID), C.GoString(eventType), dataStr, metaStr)
	}
}

//export goLogCallback
func goLogCallback(message *C.char, userData unsafe.Pointer) {
	key := uintptr(userData)
	callbackMu.RLock()
	cb := logCallbacks[key]
	callbackMu.RUnlock()
	if cb != nil {
		cb(C.GoString(message))
	}
}

//export goSlowHandlerCallback
func goSlowHandlerCallback(handlerName *C.char, durationMs C.int, userData unsafe.Pointer) {
	key := uintptr(userData)
	callbackMu.RLock()
	cb := slowCallbacks[key]
	callbackMu.RUnlock()
	if cb != nil {
		cb(C.GoString(handlerName), int(durationMs))
	}
}

//export goStateChangedCallback
func goStateChangedCallback(partition *C.char, stateJSON *C.char, userData unsafe.Pointer) {
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
