// Package gafferruntime provides Go bindings for the Gaffer projection runtime.
package gafferruntime

/*
#cgo CFLAGS: -I${SRCDIR}/../../runtime/include
#cgo LDFLAGS: ${SRCDIR}/../../runtime/Gaffer.Runtime/bin/Release/net9.0/linux-x64/publish/Gaffer.Runtime.so -Wl,-rpath,${SRCDIR}/../../runtime/Gaffer.Runtime/bin/Release/net9.0/linux-x64/publish
#include "gaffer.h"
#include <stdlib.h>
*/
import "C"
import "unsafe"

type Session = C.gaffer_session

func SessionCreate(source string, optionsJSON *string) *Session {
	cs := C.CString(source)
	defer C.free(unsafe.Pointer(cs))
	var opts *C.char
	if optionsJSON != nil {
		opts = C.CString(*optionsJSON)
		defer C.free(unsafe.Pointer(opts))
	}
	return C.gaffer_session_create(cs, opts)
}

func SessionDestroy(session *Session) {
	C.gaffer_session_destroy(session)
}

func SessionFeed(session *Session, eventJSON string) int {
	cs := C.CString(eventJSON)
	defer C.free(unsafe.Pointer(cs))
	return int(C.gaffer_session_feed(session, cs))
}

func SessionGetState(session *Session, partition *string) *string {
	var cp *C.char
	if partition != nil {
		cp = C.CString(*partition)
		defer C.free(unsafe.Pointer(cp))
	}
	result := C.gaffer_session_get_state(session, cp)
	if result == nil {
		return nil
	}
	s := C.GoString(result)
	return &s
}

func SessionGetSharedState(session *Session) *string {
	result := C.gaffer_session_get_shared_state(session)
	if result == nil {
		return nil
	}
	s := C.GoString(result)
	return &s
}

func SessionSetState(session *Session, partition *string, stateJSON string) {
	var cp *C.char
	if partition != nil {
		cp = C.CString(*partition)
		defer C.free(unsafe.Pointer(cp))
	}
	cs := C.CString(stateJSON)
	defer C.free(unsafe.Pointer(cs))
	C.gaffer_session_set_state(session, cp, cs)
}

func SessionGetResult(session *Session, partition *string) *string {
	var cp *C.char
	if partition != nil {
		cp = C.CString(*partition)
		defer C.free(unsafe.Pointer(cp))
	}
	result := C.gaffer_session_get_result(session, cp)
	if result == nil {
		return nil
	}
	s := C.GoString(result)
	return &s
}

func SessionGetSources(session *Session) *string {
	result := C.gaffer_session_get_sources(session)
	if result == nil {
		return nil
	}
	s := C.GoString(result)
	return &s
}

func SessionGetError(session *Session) *string {
	result := C.gaffer_session_get_error(session)
	if result == nil {
		return nil
	}
	s := C.GoString(result)
	return &s
}
