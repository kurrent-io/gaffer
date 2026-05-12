package telemetry

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestEmitException_NilClientIsNoop(t *testing.T) {
	// Opt-out path: no Client on ctx. Must not panic, must not
	// reach into the emit path.
	EmitException(context.Background(), "boom", ExceptionPhaseEventProcessing)
}

func TestEmitException_NilRecoveredIsNoop(t *testing.T) {
	ctx, c, mock := emitTestSetup(t)
	// `recover()` returning nil means there wasn't a panic. Emit
	// would produce a degenerate envelope; we short-circuit
	// instead so the dataset only carries real exception events.
	EmitException(ctx, nil, ExceptionPhaseEventProcessing)
	if err := c.Flush(timeoutCtx(t, time.Second)); err != nil {
		t.Fatalf("flush: %v", err)
	}
	if got := mock.Len(); got != 0 {
		t.Errorf("envelopes = %d, want 0 (nil recover means no panic)", got)
	}
}

// triggerRuntimeError produces a real runtime.Error so the
// captureStack frames mirror the production shape. Trying to
// fabricate one with reflect would lose the runtime-error
// interface satisfaction.
func triggerRuntimeError() (r any) {
	defer func() {
		r = recover()
	}()
	var p *int
	_ = *p // nil-pointer deref - panics with runtime.Error
	return nil
}

func TestEmitException_StampsCurrentCommand(t *testing.T) {
	ctx, c, mock := emitTestSetup(t)
	// Simulate a Begin/Emit having fired before the panic.
	c.setCurrentCommand(CommandNameDev)

	EmitException(ctx, "boom", ExceptionPhaseEventProcessing)
	if err := c.Flush(timeoutCtx(t, time.Second)); err != nil {
		t.Fatalf("flush: %v", err)
	}
	envs := mock.Envelopes()
	if len(envs) != 1 {
		t.Fatalf("envelopes = %d, want 1", len(envs))
	}
	props := envs[0].Events[0].(Exception).Properties
	if props.Command == nil {
		t.Fatal("Command = nil, expected dev")
	}
	if *props.Command != CommandNameDev {
		t.Errorf("Command = %q, want dev", *props.Command)
	}
}

func TestEmitException_OmitsCommandWhenUnset(t *testing.T) {
	ctx, c, mock := emitTestSetup(t)
	// No Begin/Emit fired - panic happened before cobra dispatched.
	EmitException(ctx, "early boom", ExceptionPhaseStartup)
	if err := c.Flush(timeoutCtx(t, time.Second)); err != nil {
		t.Fatalf("flush: %v", err)
	}
	props := mock.Envelopes()[0].Events[0].(Exception).Properties
	if props.Command != nil {
		t.Errorf("Command = %q, want nil when no command in flight", *props.Command)
	}
}

func TestEmitException_RuntimeErrorGetsVerbatimMessage(t *testing.T) {
	ctx, c, mock := emitTestSetup(t)
	r := triggerRuntimeError()
	if _, ok := r.(runtime.Error); !ok {
		t.Fatalf("expected runtime.Error, got %T", r)
	}
	EmitException(ctx, r, ExceptionPhaseEventProcessing)
	if err := c.Flush(timeoutCtx(t, time.Second)); err != nil {
		t.Fatalf("flush: %v", err)
	}
	envs := mock.Envelopes()
	if len(envs) != 1 {
		t.Fatalf("envelopes = %d, want 1", len(envs))
	}
	ex := envs[0].Events[0].(Exception)
	if ex.Properties.Phase != ExceptionPhaseEventProcessing {
		t.Errorf("phase = %q, want event_processing", ex.Properties.Phase)
	}
	if len(ex.Properties.Exceptions) != 1 {
		t.Fatalf("exceptions = %d, want 1", len(ex.Properties.Exceptions))
	}
	entry := ex.Properties.Exceptions[0]
	if entry.Type != "RuntimeError" {
		t.Errorf("Type = %q, want RuntimeError", entry.Type)
	}
	if entry.Value == unsanitizedExceptionValue {
		t.Error("Value = unsanitized; runtime.Error message should pass through")
	}
	if !strings.Contains(entry.Value, "nil pointer") {
		t.Errorf("Value = %q, expected to mention nil pointer", entry.Value)
	}
}

func TestEmitException_StringPanicIsUnsanitized(t *testing.T) {
	// A raw string panic could embed anything ("user X did Y").
	// The plan's safety rule: only runtime errors pass verbatim;
	// strings get the placeholder.
	ctx, c, mock := emitTestSetup(t)
	EmitException(ctx, "user input was: alice@example.com", ExceptionPhaseEventProcessing)
	if err := c.Flush(timeoutCtx(t, time.Second)); err != nil {
		t.Fatalf("flush: %v", err)
	}
	entry := mock.Envelopes()[0].Events[0].(Exception).Properties.Exceptions[0]
	if entry.Type != "string" {
		t.Errorf("Type = %q, want string", entry.Type)
	}
	if entry.Value != unsanitizedExceptionValue {
		t.Errorf("Value = %q, want %q (string panics never leak through)", entry.Value, unsanitizedExceptionValue)
	}
	if strings.Contains(entry.Value, "alice") {
		t.Error("sanitization failed: leaked user-content fragment")
	}
}

func TestEmitException_ErrorPanicIsUnsanitized(t *testing.T) {
	// Same containment for an arbitrary error: even though it's
	// a typed value, the message could embed anything. Keep the
	// safety rail.
	ctx, c, mock := emitTestSetup(t)
	EmitException(ctx, errors.New("token=secret-123"), ExceptionPhaseEventProcessing)
	if err := c.Flush(timeoutCtx(t, time.Second)); err != nil {
		t.Fatalf("flush: %v", err)
	}
	entry := mock.Envelopes()[0].Events[0].(Exception).Properties.Exceptions[0]
	if entry.Type != "error" {
		t.Errorf("Type = %q, want error", entry.Type)
	}
	if entry.Value != unsanitizedExceptionValue {
		t.Errorf("Value = %q, want unsanitized", entry.Value)
	}
}

func TestEmitException_PhasePropagates(t *testing.T) {
	ctx, c, mock := emitTestSetup(t)
	EmitException(ctx, "p", ExceptionPhaseStartup)
	if err := c.Flush(timeoutCtx(t, time.Second)); err != nil {
		t.Fatalf("flush: %v", err)
	}
	got := mock.Envelopes()[0].Events[0].(Exception).Properties.Phase
	if got != ExceptionPhaseStartup {
		t.Errorf("phase = %q, want startup", got)
	}
}

func TestScrubFrames_BasenameAndInApp(t *testing.T) {
	frames := []runtime.Frame{
		{File: "/workspaces/gaffer/cli/cmd/dev.go", Function: "github.com/kurrent-io/gaffer/cli/cmd.runDev", Line: 42},
		{File: "/usr/local/go/src/runtime/panic.go", Function: "runtime.gopanic", Line: 838},
		{File: "/home/x/go/pkg/mod/github.com/charmbracelet/cobra@v1.0.0/cobra.go", Function: "github.com/spf13/cobra.(*Command).ExecuteContext", Line: 100},
	}
	got := scrubFrames(frames)

	if got[0].Filename != "dev.go" {
		t.Errorf("frame[0].Filename = %q, want dev.go (basename only)", got[0].Filename)
	}
	if !got[0].InApp {
		t.Error("frame[0].InApp = false; gaffer-owned frame should be true")
	}
	if got[0].Lineno == nil || *got[0].Lineno != 42 {
		t.Errorf("frame[0].Lineno = %v, want &42", got[0].Lineno)
	}
	if got[1].InApp {
		t.Error("frame[1].InApp = true; runtime frame should be false")
	}
	if got[1].Filename != "panic.go" {
		t.Errorf("frame[1].Filename = %q, want panic.go", got[1].Filename)
	}
	if got[2].InApp {
		t.Error("frame[2].InApp = true; cobra is a vendored dep, not gaffer-owned")
	}
}

func TestScrubFrames_EmptyReturnsNil(t *testing.T) {
	// Empty input -> nil slice so JSON omits `frames` rather
	// than emitting [].
	if got := scrubFrames(nil); got != nil {
		t.Errorf("scrubFrames(nil) = %v, want nil", got)
	}
}

// triggerTypeAssertionError forces a *runtime.TypeAssertionError so
// the privacy-relevant denylist branch in exceptionValue is exercised
// against a real panic shape. Fabricating one with reflect would lose
// the message-format the test asserts on.
func triggerTypeAssertionError() (r any) {
	defer func() {
		r = recover()
	}()
	var i interface{} = "hello"
	_ = i.(int) // panics with *runtime.TypeAssertionError
	return nil
}

func TestEmitException_TypeAssertionErrorIsUnsanitized(t *testing.T) {
	// Privacy regression guard. *runtime.TypeAssertionError
	// satisfies the runtime.Error interface but its .Error()
	// message embeds the concrete and interface type names,
	// which can come from user code. The denylist in
	// exceptionValue must catch this case.
	ctx, c, mock := emitTestSetup(t)
	r := triggerTypeAssertionError()
	if _, ok := r.(*runtime.TypeAssertionError); !ok {
		t.Fatalf("expected *runtime.TypeAssertionError, got %T", r)
	}
	EmitException(ctx, r, ExceptionPhaseEventProcessing)
	if err := c.Flush(timeoutCtx(t, time.Second)); err != nil {
		t.Fatalf("flush: %v", err)
	}
	entry := mock.Envelopes()[0].Events[0].(Exception).Properties.Exceptions[0]
	if entry.Type != "RuntimeError" {
		t.Errorf("Type = %q, want RuntimeError (runtime.Error interface still flags Type)", entry.Type)
	}
	if entry.Value != unsanitizedExceptionValue {
		t.Errorf("Value = %q, want unsanitized; TypeAssertionError can embed user type names", entry.Value)
	}
}

func TestEmitException_TypedNonErrorPanicGetsPanicLabel(t *testing.T) {
	// `panic(MyStruct{})` where MyStruct doesn't implement error
	// used to leak the type name via reflect.TypeOf. Now collapses
	// to "panic" so the Type field can't carry a user-chosen
	// identifier.
	type SecretError struct{ Token string }
	ctx, c, mock := emitTestSetup(t)
	EmitException(ctx, SecretError{Token: "leak-me"}, ExceptionPhaseEventProcessing)
	if err := c.Flush(timeoutCtx(t, time.Second)); err != nil {
		t.Fatalf("flush: %v", err)
	}
	entry := mock.Envelopes()[0].Events[0].(Exception).Properties.Exceptions[0]
	if entry.Type != "panic" {
		t.Errorf("Type = %q, want panic (user-chosen type name must not leak)", entry.Type)
	}
	if entry.Value != unsanitizedExceptionValue {
		t.Errorf("Value = %q, want unsanitized", entry.Value)
	}
}

// TestPairedEventsInvariant_PanicEmitsBothCommandInvokedAndException
// is the integration-shape test for cli-plan.md:106's paired-events
// invariant. Simulates the production shape:
//   - a cobra-RunE-equivalent body that panics inside a tx
//   - inner `defer tx.End(ctx)` (recovers, emits command_invoked
//     with outcome=internal_error, re-panics)
//   - outer recover that fires EmitException
//
// Asserts BOTH envelopes arrive after the chain unwinds. Order
// on the mock is non-deterministic because c.emit spawns one
// goroutine per envelope and the sink records on goroutine
// completion - that's a property of the transport, not a
// violation. The invariant the test pins is "panic produces both
// envelopes," not "in any particular sink-order."
func TestPairedEventsInvariant_PanicEmitsBothCommandInvokedAndException(t *testing.T) {
	ctx, c, mock := emitTestSetup(t)

	// Outer recover mirrors main.go's runMain panic-recover.
	defer func() {
		r := recover()
		if r != nil {
			EmitException(ctx, r, ExceptionPhaseEventProcessing)
		}
		// Don't re-panic - we want the test to continue and
		// assert on the captured envelopes.
		_ = c.Flush(timeoutCtx(t, time.Second))

		envs := mock.Envelopes()
		if len(envs) != 2 {
			t.Fatalf("envelopes = %d, want 2 (paired command_invoked + exception)", len(envs))
		}
		var (
			seenCommandInvoked bool
			seenException      bool
			ciOutcome          Outcome
		)
		for _, env := range envs {
			switch ev := env.Events[0].(type) {
			case CommandInvoked:
				seenCommandInvoked = true
				if props, ok := ev.Properties.(DevCommandInvokedProperties); ok {
					ciOutcome = props.Outcome
				}
			case Exception:
				seenException = true
			}
		}
		if !seenCommandInvoked {
			t.Error("missing command_invoked envelope from the paired pair")
		}
		if !seenException {
			t.Error("missing exception envelope from the paired pair")
		}
		if ciOutcome != OutcomeInternalError {
			t.Errorf("command_invoked.outcome = %q, want internal_error (panic path)", ciOutcome)
		}
	}()

	// Cobra-RunE-equivalent body. The direct `defer tx.End(ctx)`
	// is load-bearing: any closure-wrap would put recover() one
	// frame too deep (see TestDevTx_EndMustBeDirectDeferShape).
	func() {
		tx := BeginDev(ctx)
		defer tx.End(ctx)
		panic("simulated user-cmd panic")
	}()
	// Unreachable - the panic propagates to the outer defer.
}

func TestEmitException_EnvelopeShape(t *testing.T) {
	// End-to-end shape check: emitted envelope has the right
	// event name, the Exceptions slice with one entry, a
	// scrubbed stack, no Command set (main's outer recover
	// doesn't know which command).
	ctx, c, mock := emitTestSetup(t)
	r := triggerRuntimeError()
	EmitException(ctx, r, ExceptionPhaseEventProcessing)
	if err := c.Flush(timeoutCtx(t, time.Second)); err != nil {
		t.Fatalf("flush: %v", err)
	}
	env := mock.Envelopes()[0]
	ex, ok := env.Events[0].(Exception)
	if !ok {
		t.Fatalf("event = %T, want Exception", env.Events[0])
	}
	if ex.Name != "exception" {
		t.Errorf("Name = %q, want exception", ex.Name)
	}
	if ex.Properties.Command != nil {
		t.Errorf("Command = %v, want nil (main's outer recover doesn't know which command)", ex.Properties.Command)
	}
	if len(ex.Properties.Exceptions) != 1 {
		t.Fatalf("exceptions = %d, want 1", len(ex.Properties.Exceptions))
	}
	if len(ex.Properties.Exceptions[0].Stacktrace.Frames) == 0 {
		t.Error("Stacktrace.Frames empty; expected captured stack")
	}
}
