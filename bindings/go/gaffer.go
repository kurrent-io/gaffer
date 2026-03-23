// Package gafferruntime provides Go bindings for the Gaffer projection runtime.
//
// The runtime executes KurrentDB projections locally via Jint (a .NET JavaScript
// interpreter). Sessions are not thread-safe - do not call from multiple
// goroutines concurrently on the same session.
package gafferruntime

/*
#cgo CFLAGS: -I${SRCDIR}/../../runtime/include
#cgo LDFLAGS: ${SRCDIR}/../../runtime/Gaffer.Runtime/bin/Release/net10.0/linux-x64/publish/Gaffer.Runtime.so -Wl,-rpath,${SRCDIR}/../../runtime/Gaffer.Runtime/bin/Release/net10.0/linux-x64/publish
#include "gaffer.h"
#include <stdlib.h>
*/
import "C"
import "unsafe"

// Session is an opaque handle to a projection runtime session.
type Session = C.gaffer_session

// SessionCreate compiles a projection from JavaScript source and returns a session.
// Returns nil if the source is invalid. Pass optionsJSON for non-default settings
// (version, handlerTimeoutMs, compilationTimeoutMs, executionTimeoutMs, debug, enableContentTypeValidation).
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

// SessionDestroy frees all resources associated with a session.
func SessionDestroy(session *Session) {
	cleanupCallbacks(session)
	C.gaffer_session_destroy(session)
}

// SessionGetPartitionKey returns the partition key that would be computed for an event.
func SessionGetPartitionKey(session *Session, eventJSON string) *string {
	cs := C.CString(eventJSON)
	defer C.free(unsafe.Pointer(cs))
	result := C.gaffer_session_get_partition_key(session, cs)
	if result == nil {
		return nil
	}
	s := C.GoString(result)
	return &s
}

// SessionFeed sends a single event to the projection. Returns 0 on success,
// non-zero on error. Call SessionGetError for the error message.
// The eventJSON must contain at least "eventType" and "streamId" fields.
func SessionFeed(session *Session, eventJSON string) int {
	cs := C.CString(eventJSON)
	defer C.free(unsafe.Pointer(cs))
	return int(C.gaffer_session_feed(session, cs))
}

// SessionGetState returns the current state for a partition, or nil if the
// partition has not been seen. Pass nil for the default (unpartitioned) state.
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

// SessionGetSharedState returns the shared state for biState projections, or nil.
func SessionGetSharedState(session *Session) *string {
	result := C.gaffer_session_get_shared_state(session)
	if result == nil {
		return nil
	}
	s := C.GoString(result)
	return &s
}

// SessionSetState restores state for a partition (e.g. from a cache).
// Pass nil for the default partition.
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

// SessionGetResult returns the transformed result for a partition (applies
// transformBy/filterBy). Returns nil for unknown partitions or filtered results.
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

// SessionGetSources returns the projection's source definition as JSON
// (what streams/events it reads, partitioning mode, etc).
func SessionGetSources(session *Session) *string {
	result := C.gaffer_session_get_sources(session)
	if result == nil {
		return nil
	}
	s := C.GoString(result)
	return &s
}

// SessionGetError returns the last error message for a session, or nil if no error.
func SessionGetError(session *Session) *string {
	result := C.gaffer_session_get_error(session)
	if result == nil {
		return nil
	}
	s := C.GoString(result)
	return &s
}
