package gafferruntime

/*
#include "gaffer.h"
#include <stdlib.h>
*/
import "C"

import (
	"encoding/json"
	"errors"
	"unsafe"
)

// ErrSessionDestroyed is returned when calling methods on a destroyed session.
var ErrSessionDestroyed = errors.New("session has been destroyed")

// ProjectionError is the interface implemented by all gaffer error types.
type ProjectionError interface {
	error
	ErrorCode() string
	ErrorDescription() string
}

// EventContext holds event information attached to errors during Feed.
type EventContext struct {
	EventType      string `json:"eventType"`
	StreamID       string `json:"streamId"`
	SequenceNumber int64  `json:"sequenceNumber"`
	Partition      string `json:"partition,omitempty"`
}

// JsLocation holds a line/column position in JavaScript source.
type JsLocation struct {
	Line   int `json:"line"`
	Column int `json:"column"`
}

type InvalidProjectionError struct {
	Desc              string
	Location          *JsLocation
	Source            string
	CompatCode        string
	CompatDescription string
	CompatFixedIn     string
	Diagnostics       []Diagnostic
	Msg               string
}

func (e *InvalidProjectionError) Error() string            { return e.Msg }
func (e *InvalidProjectionError) ErrorCode() string        { return "invalid-projection" }
func (e *InvalidProjectionError) ErrorDescription() string { return e.Desc }

type CompilationTimeoutError struct {
	Desc              string
	ElapsedMs         int
	AllowedMs         int
	CompatCode        string
	CompatDescription string
	CompatFixedIn     string
	Diagnostics       []Diagnostic
	Msg               string
}

func (e *CompilationTimeoutError) Error() string            { return e.Msg }
func (e *CompilationTimeoutError) ErrorCode() string        { return "compilation-timeout" }
func (e *CompilationTimeoutError) ErrorDescription() string { return e.Desc }

type InvalidArgumentError struct {
	Desc              string
	Field             string
	CompatCode        string
	CompatDescription string
	CompatFixedIn     string
	Diagnostics       []Diagnostic
	Msg               string
}

func (e *InvalidArgumentError) Error() string            { return e.Msg }
func (e *InvalidArgumentError) ErrorCode() string        { return "invalid-argument" }
func (e *InvalidArgumentError) ErrorDescription() string { return e.Desc }

type ProjectionHandlerError struct {
	Desc              string
	JsStack           string
	Location          *JsLocation
	Event             EventContext
	Source            string
	CompatCode        string
	CompatDescription string
	CompatFixedIn     string
	Diagnostics       []Diagnostic
	Msg               string
}

func (e *ProjectionHandlerError) Error() string            { return e.Msg }
func (e *ProjectionHandlerError) ErrorCode() string        { return "handler-error" }
func (e *ProjectionHandlerError) ErrorDescription() string { return e.Desc }

type ExecutionTimeoutError struct {
	Desc              string
	ElapsedMs         int
	AllowedMs         int
	Event             EventContext
	CompatCode        string
	CompatDescription string
	CompatFixedIn     string
	Diagnostics       []Diagnostic
	Msg               string
}

func (e *ExecutionTimeoutError) Error() string            { return e.Msg }
func (e *ExecutionTimeoutError) ErrorCode() string        { return "execution-timeout" }
func (e *ExecutionTimeoutError) ErrorDescription() string { return e.Desc }

type MalformedEventError struct {
	Desc              string
	Event             EventContext
	CompatCode        string
	CompatDescription string
	CompatFixedIn     string
	Diagnostics       []Diagnostic
	Msg               string
}

func (e *MalformedEventError) Error() string            { return e.Msg }
func (e *MalformedEventError) ErrorCode() string        { return "malformed-event" }
func (e *MalformedEventError) ErrorDescription() string { return e.Desc }

type StateSerializationError struct {
	Desc              string
	Event             EventContext
	CompatCode        string
	CompatDescription string
	CompatFixedIn     string
	Diagnostics       []Diagnostic
	Msg               string
}

func (e *StateSerializationError) Error() string            { return e.Msg }
func (e *StateSerializationError) ErrorCode() string        { return "state-serialization-error" }
func (e *StateSerializationError) ErrorDescription() string { return e.Desc }

type ProjectionTransformError struct {
	Desc              string
	JsStack           string
	Location          *JsLocation
	Source            string
	CompatCode        string
	CompatDescription string
	CompatFixedIn     string
	Diagnostics       []Diagnostic
	Msg               string
}

func (e *ProjectionTransformError) Error() string            { return e.Msg }
func (e *ProjectionTransformError) ErrorCode() string        { return "projection-transform-error" }
func (e *ProjectionTransformError) ErrorDescription() string { return e.Desc }

// UnexpectedError represents an unknown or internal error from the runtime.
type UnexpectedError struct {
	Code string
	Desc string
	Msg  string
}

func (e *UnexpectedError) Error() string            { return e.Msg }
func (e *UnexpectedError) ErrorCode() string        { return e.Code }
func (e *UnexpectedError) ErrorDescription() string { return e.Desc }

type errorJSON struct {
	Code              string       `json:"code"`
	Description       string       `json:"description"`
	Message           string       `json:"message,omitempty"`
	CompatCode        string       `json:"compatCode,omitempty"`
	CompatDescription string       `json:"compatDescription,omitempty"`
	CompatFixedIn     string       `json:"compatFixedIn,omitempty"`
	Diagnostics       []Diagnostic `json:"diagnostics,omitempty"`
	Line              *int         `json:"line,omitempty"`
	Column            *int         `json:"column,omitempty"`
	Elapsed           *int         `json:"elapsed,omitempty"`
	Allowed           *int         `json:"allowed,omitempty"`
	Field             string       `json:"field,omitempty"`
	JsStack           string       `json:"jsStack,omitempty"`
	EventType         string       `json:"eventType,omitempty"`
	StreamID          string       `json:"streamId,omitempty"`
	SequenceNumber    int64        `json:"sequenceNumber,omitempty"`
	Partition         string       `json:"partition,omitempty"`
}

// consumeError decodes and frees a runtime-allocated error pointer.
// Returns nil if cErr is nil. Caller must not free cErr afterwards.
func consumeError(cErr *C.char, source string) error {
	if cErr == nil {
		return nil
	}
	defer C.gaffer_free(unsafe.Pointer(cErr))
	return parseErrorJSON(C.GoString(cErr), source)
}

func parseErrorJSON(jsonStr string, source string) error {
	var e errorJSON
	if err := json.Unmarshal([]byte(jsonStr), &e); err != nil {
		return &UnexpectedError{Code: "unexpected", Desc: jsonStr, Msg: jsonStr}
	}

	msg := e.Message
	if msg == "" {
		msg = e.Description
	}

	switch e.Code {
	case "invalid-projection":
		var loc *JsLocation
		if e.Line != nil && e.Column != nil {
			loc = &JsLocation{Line: *e.Line, Column: *e.Column}
		}
		return &InvalidProjectionError{Desc: e.Description, Location: loc, Source: source, CompatCode: e.CompatCode, CompatDescription: e.CompatDescription, CompatFixedIn: e.CompatFixedIn, Diagnostics: e.Diagnostics, Msg: msg}

	case "compilation-timeout":
		return &CompilationTimeoutError{Desc: e.Description, ElapsedMs: deref(e.Elapsed), AllowedMs: deref(e.Allowed), CompatCode: e.CompatCode, CompatDescription: e.CompatDescription, CompatFixedIn: e.CompatFixedIn, Diagnostics: e.Diagnostics, Msg: msg}

	case "invalid-argument":
		return &InvalidArgumentError{Desc: e.Description, Field: e.Field, CompatCode: e.CompatCode, CompatDescription: e.CompatDescription, CompatFixedIn: e.CompatFixedIn, Diagnostics: e.Diagnostics, Msg: msg}

	case "handler-error":
		var loc *JsLocation
		if e.Line != nil && e.Column != nil {
			loc = &JsLocation{Line: *e.Line, Column: *e.Column}
		}
		return &ProjectionHandlerError{
			Desc: e.Description, JsStack: e.JsStack, Location: loc,
			Event:             EventContext{EventType: e.EventType, StreamID: e.StreamID, SequenceNumber: e.SequenceNumber, Partition: e.Partition},
			Source:            source,
			CompatCode:        e.CompatCode,
			CompatDescription: e.CompatDescription,
			CompatFixedIn:     e.CompatFixedIn,
			Diagnostics:       e.Diagnostics,
			Msg:               msg,
		}

	case "execution-timeout":
		return &ExecutionTimeoutError{
			Desc: e.Description, ElapsedMs: deref(e.Elapsed), AllowedMs: deref(e.Allowed),
			Event:             EventContext{EventType: e.EventType, StreamID: e.StreamID, SequenceNumber: e.SequenceNumber, Partition: e.Partition},
			CompatCode:        e.CompatCode,
			CompatDescription: e.CompatDescription,
			CompatFixedIn:     e.CompatFixedIn,
			Diagnostics:       e.Diagnostics,
			Msg:               msg,
		}

	case "malformed-event":
		return &MalformedEventError{
			Desc:              e.Description,
			Event:             EventContext{EventType: e.EventType, StreamID: e.StreamID, SequenceNumber: e.SequenceNumber, Partition: e.Partition},
			CompatCode:        e.CompatCode,
			CompatDescription: e.CompatDescription,
			CompatFixedIn:     e.CompatFixedIn,
			Diagnostics:       e.Diagnostics,
			Msg:               msg,
		}

	case "state-serialization-error":
		return &StateSerializationError{
			Desc:              e.Description,
			Event:             EventContext{EventType: e.EventType, StreamID: e.StreamID, SequenceNumber: e.SequenceNumber, Partition: e.Partition},
			CompatCode:        e.CompatCode,
			CompatDescription: e.CompatDescription,
			CompatFixedIn:     e.CompatFixedIn,
			Diagnostics:       e.Diagnostics,
			Msg:               msg,
		}

	case "projection-transform-error":
		var loc *JsLocation
		if e.Line != nil && e.Column != nil {
			loc = &JsLocation{Line: *e.Line, Column: *e.Column}
		}
		return &ProjectionTransformError{Desc: e.Description, JsStack: e.JsStack, Location: loc, Source: source, CompatCode: e.CompatCode, CompatDescription: e.CompatDescription, CompatFixedIn: e.CompatFixedIn, Diagnostics: e.Diagnostics, Msg: msg}

	default:
		return &UnexpectedError{Code: e.Code, Desc: e.Description, Msg: msg}
	}
}

func deref(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}
