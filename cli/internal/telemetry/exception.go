package telemetry

import (
	"context"
	"path/filepath"
	"runtime"
	"strings"
)

// gafferModulePrefix is the import-path prefix that marks a frame
// as gaffer-owned for the `in_app` flag. Stdlib frames (just
// `runtime.X`, `os.X`) and vendored frames (other modules) lack
// this prefix and serialize as in_app=false.
const gafferModulePrefix = "github.com/kurrent-io/gaffer/"

// unsanitizedExceptionValue is the placeholder we emit when the
// recovered panic value could embed user-controlled content
// (anything that isn't a runtime.Error). The structurally-scrubbed
// stack still ships; only the value text is suppressed.
const unsanitizedExceptionValue = "<unsanitized>"

// EmitException fires an `exception` envelope for a recovered
// Go panic. The structured stack is captured via captureStack
// (which itself uses a fixed-size buffer so a deep panic doesn't
// allocate megabytes). The panic value is sanitized: only
// runtime.Error (nil deref, slice OOB, type-assertion failure -
// no user content possible) gets through verbatim; everything
// else (string panics, error wrappers, typed user errors) is
// emitted as "<unsanitized>" so a user message can't leak via
// `exception.value`.
//
// Frame scrubbing: filename keeps the basename only (never the
// full path); function name keeps the qualified Go name; the
// in_app flag is true for gaffer-owned frames and false for
// stdlib / vendored deps.
//
// MUST be called from a deferred function (`defer func() {
// if r := recover(); r != nil { telemetry.EmitException(...) }
// }()`) so the recovered value is the panic in flight. Calling
// it outside that context emits a degenerate envelope with no
// stack.
//
// No-op when ctx carries no Client (opt-out).
func EmitException(ctx context.Context, recovered any, phase ExceptionPhase) {
	c := ClientFromContext(ctx)
	if c == nil || recovered == nil {
		return
	}
	_, frames := captureStack(recovered)
	entry := ExceptionEntry{
		Type:       exceptionTypeName(recovered),
		Value:      exceptionValue(recovered),
		InApp:      true,
		Stacktrace: ExceptionStacktrace{Type: "go", Frames: scrubFrames(frames)},
	}
	props := ExceptionProperties{
		Exceptions: []ExceptionEntry{entry},
		Phase:      phase,
	}
	if cmd := c.currentCommandName(); cmd != "" {
		props.Command = &cmd
	}
	c.emit(c.buildEnvelope(Exception{
		Name:       "exception",
		Timestamp:  nowTimestamp(),
		Properties: props,
	}))
}

// exceptionTypeName returns a short type label for the recovered
// value. For runtime errors we emit "RuntimeError" (a generic tag
// that aggregates nil-derefs, slice-OOB, etc - the worker doesn't
// need the Go-specific concrete type to triage). For everything
// else the label is a coarse Go-shape marker - "string", "error",
// or "panic" - chosen so even typed user-code panic values
// (panic(&pkg.SecretError{})) don't leak the user's chosen type
// name onto the wire.
func exceptionTypeName(r any) string {
	if _, ok := r.(runtime.Error); ok {
		return "RuntimeError"
	}
	switch r.(type) {
	case string:
		return "string"
	case error:
		// User-supplied error wrappers fall here. Type label
		// is the universal Go interface; the concrete type
		// name (`*acmecorp.TokenError`, etc.) never crosses
		// the FFI.
		return "error"
	default:
		// `panic(struct{...}{})` and other non-error typed
		// values land here. Emit a fixed label rather than
		// reflect.TypeOf(r).String() so user-chosen type
		// identifiers don't leak via the Type field.
		return "panic"
	}
}

// exceptionValue returns the message string to put in
// exception.value. Only runtime.Error implementations with
// guaranteed-safe messages pass verbatim:
//
//   - *runtime.errorString family (nil deref, slice OOB, integer
//     divide by zero, nil-map assignment, ...) - fixed message
//     templates from the Go runtime that may include numeric
//     indices but no user-controlled identifiers.
//   - *runtime.PanicNilError - fixed message "panic called with
//     nil argument".
//
// Everything else - including *runtime.TypeAssertionError, which
// satisfies the runtime.Error interface but embeds user-defined
// type names in its message ("interface conversion: T is not U:
// missing method M") - falls to the unsanitized placeholder.
// The structurally-scrubbed stack still ships either way.
func exceptionValue(r any) string {
	rErr, ok := r.(runtime.Error)
	if !ok {
		return unsanitizedExceptionValue
	}
	// Explicit denylist for runtime.Error implementations whose
	// .Error() includes user-controlled content. Inverting this
	// to an allowlist would require enumerating the unexported
	// *runtime.errorString family by reflection - brittle when
	// the Go runtime grows new error types. The denylist of
	// known-leaky kinds is the safer long-run shape.
	if _, isAssertion := r.(*runtime.TypeAssertionError); isAssertion {
		return unsanitizedExceptionValue
	}
	return rErr.Error()
}

// scrubFrames translates runtime.Frame values into the wire-form
// Frame slice with basename-only filenames, in_app flagging, and
// optional pointers honoured. Empty input returns nil so the
// JSON omits the `frames` field cleanly.
func scrubFrames(frames []runtime.Frame) []Frame {
	if len(frames) == 0 {
		return nil
	}
	out := make([]Frame, 0, len(frames))
	for _, f := range frames {
		out = append(out, Frame{
			Filename: filepath.Base(f.File),
			Function: optionalString(f.Function),
			Lineno:   optionalInt(f.Line),
			InApp:    strings.HasPrefix(f.Function, gafferModulePrefix),
		})
	}
	return out
}

func optionalString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func optionalInt(n int) *int {
	if n == 0 {
		return nil
	}
	return &n
}
