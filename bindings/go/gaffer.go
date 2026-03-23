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

// Session wraps a projection runtime session.
// Not thread-safe - do not use from multiple goroutines concurrently.
type Session struct {
	handle *C.gaffer_session
	source string
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
	handle := C.gaffer_session_create(cs, opts)
	if handle == nil {
		return nil, getLastError(source)
	}
	return &Session{handle: handle, source: source}, nil
}

// Destroy frees all resources associated with the session.
func (s *Session) Destroy() {
	if s.handle == nil {
		return
	}
	cleanupCallbacks(s.handle)
	C.gaffer_session_destroy(s.handle)
	s.handle = nil
}

func (s *Session) ensureAlive() {
	if s.handle == nil {
		panic("use of destroyed session")
	}
}

// Feed sends a single event to the projection.
func (s *Session) Feed(eventJSON string) error {
	s.ensureAlive()
	cs := C.CString(eventJSON)
	defer C.free(unsafe.Pointer(cs))
	result := int(C.gaffer_session_feed(s.handle, cs))
	if result != 0 {
		return getLastError(s.source)
	}
	return nil
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
	result := C.gaffer_session_get_state(s.handle, cp)
	if result == nil {
		return nil
	}
	str := C.GoString(result)
	return &str
}

// GetSharedState returns the shared state for biState projections, or nil.
func (s *Session) GetSharedState() *string {
	s.ensureAlive()
	result := C.gaffer_session_get_shared_state(s.handle)
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
	C.gaffer_session_set_state(s.handle, cp, cs)
}

// GetResult returns the transformed result for a partition (applies
// transformBy/filterBy). Returns nil for unknown partitions or filtered results.
func (s *Session) GetResult(partition *string) (*string, error) {
	s.ensureAlive()
	var cp *C.char
	if partition != nil {
		cp = C.CString(*partition)
		defer C.free(unsafe.Pointer(cp))
	}
	result := C.gaffer_session_get_result(s.handle, cp)
	if err := checkLastError(s.source); err != nil {
		return nil, err
	}
	if result == nil {
		return nil, nil
	}
	str := C.GoString(result)
	return &str, nil
}

// GetSources returns the projection's source definition as JSON.
func (s *Session) GetSources() *string {
	s.ensureAlive()
	result := C.gaffer_session_get_sources(s.handle)
	if result == nil {
		return nil
	}
	str := C.GoString(result)
	return &str
}

// GetPartitionKey returns the partition key that would be computed for an event.
func (s *Session) GetPartitionKey(eventJSON string) *string {
	s.ensureAlive()
	cs := C.CString(eventJSON)
	defer C.free(unsafe.Pointer(cs))
	result := C.gaffer_session_get_partition_key(s.handle, cs)
	if result == nil {
		return nil
	}
	str := C.GoString(result)
	return &str
}

// OnEmit registers a callback for emitted events (emit and linkTo).
func (s *Session) OnEmit(cb EmitCallback) {
	sessionOnEmit(s.handle, cb)
}

// OnLog registers a callback for console.log output.
func (s *Session) OnLog(cb LogCallback) {
	sessionOnLog(s.handle, cb)
}

// OnStateChanged registers a callback for state changes.
func (s *Session) OnStateChanged(cb StateChangedCallback) {
	sessionOnStateChanged(s.handle, cb)
}
