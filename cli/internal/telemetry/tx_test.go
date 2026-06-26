package telemetry

import (
	"context"
	"testing"
	"time"

	"github.com/kurrent-io/gaffer/cli/internal/testutil"
)

func TestBeginDev_NoClientReturnsNil(t *testing.T) {
	if tx := BeginDev(context.Background()); tx != nil {
		t.Errorf("BeginDev(empty ctx) = %v, want nil", tx)
	}
}

func TestDevTx_EndNilReceiverIsNoop(t *testing.T) {
	var tx *DevTx
	// Must not panic on nil receiver. We're not in a panicking
	// goroutine, so the recover() inside End is a no-op too.
	tx.End(context.Background())
}

// TestDevTx_EndMustBeDirectDeferShape locks in the load-bearing
// invariant that `defer tx.End(ctx)` is the only correct call shape
// for End under a panicking body. Wrapping End in a closure
// (`defer func() { tx.End(ctx) }()`) would put recover() one frame
// too deep and silently drop the panic - this test would then
// observe outcome=success (or user_interrupt) instead of
// internal_error. Any future refactor that breaks the direct-defer
// requirement fails here before reaching production.
func TestDevTx_EndMustBeDirectDeferShape(t *testing.T) {
	ctx, c, mock := emitTestSetup(t)
	defer func() { _ = recover() }()
	func() {
		tx := BeginDev(ctx)
		defer tx.End(ctx) // direct defer; production-shape
		panic("force-panic")
	}()
	if err := c.Flush(timeoutCtx(t, time.Second)); err != nil {
		t.Fatalf("flush: %v", err)
	}
	envs := mock.Envelopes()
	if len(envs) != 1 {
		t.Fatalf("envelopes = %d, want 1", len(envs))
	}
	props := testutil.MustType[DevCommandInvokedProperties](t, testutil.MustType[CommandInvoked](t, envs[0].Events[0]).Properties)
	if props.Outcome != OutcomeInternalError {
		t.Errorf("direct-defer shape: Outcome = %q, want internal_error", props.Outcome)
	}
}

// The remaining three long-running Tx variants share the same
// generated End body and therefore the same direct-defer
// requirement. These tests mirror TestDevTx_EndMustBeDirectDeferShape
// so a future regen that drifts any one variant's recover-frame
// shape fails CI rather than silently breaking the paired-events
// invariant for that command.
func TestMCPTx_EndMustBeDirectDeferShape(t *testing.T) {
	ctx, c, mock := emitTestSetup(t)
	defer func() { _ = recover() }()
	func() {
		tx := BeginMCP(ctx)
		defer tx.End(ctx)
		panic("force-panic")
	}()
	_ = c.Flush(timeoutCtx(t, time.Second))
	envs := mock.Envelopes()
	if len(envs) != 1 {
		t.Fatalf("envelopes = %d, want 1", len(envs))
	}
	props := testutil.MustType[MCPCommandInvokedProperties](t, testutil.MustType[CommandInvoked](t, envs[0].Events[0]).Properties)
	if props.Outcome != OutcomeInternalError {
		t.Errorf("direct-defer shape: Outcome = %q, want internal_error", props.Outcome)
	}
}

func TestLSPTx_EndMustBeDirectDeferShape(t *testing.T) {
	ctx, c, mock := emitTestSetup(t)
	defer func() { _ = recover() }()
	func() {
		tx := BeginLSP(ctx)
		defer tx.End(ctx)
		panic("force-panic")
	}()
	_ = c.Flush(timeoutCtx(t, time.Second))
	envs := mock.Envelopes()
	if len(envs) != 1 {
		t.Fatalf("envelopes = %d, want 1", len(envs))
	}
	props := testutil.MustType[LSPCommandInvokedProperties](t, testutil.MustType[CommandInvoked](t, envs[0].Events[0]).Properties)
	if props.Outcome != OutcomeInternalError {
		t.Errorf("direct-defer shape: Outcome = %q, want internal_error", props.Outcome)
	}
}

func TestDebugTx_EndMustBeDirectDeferShape(t *testing.T) {
	ctx, c, mock := emitTestSetup(t)
	defer func() { _ = recover() }()
	func() {
		tx := BeginDebug(ctx)
		defer tx.End(ctx)
		panic("force-panic")
	}()
	_ = c.Flush(timeoutCtx(t, time.Second))
	envs := mock.Envelopes()
	if len(envs) != 1 {
		t.Fatalf("envelopes = %d, want 1", len(envs))
	}
	props := testutil.MustType[DebugCommandInvokedProperties](t, testutil.MustType[CommandInvoked](t, envs[0].Events[0]).Properties)
	if props.Outcome != OutcomeInternalError {
		t.Errorf("direct-defer shape: Outcome = %q, want internal_error", props.Outcome)
	}
}

func TestDevTx_EndRePanicsRecoveredValue(t *testing.T) {
	defer func() {
		r := recover()
		if r != "boom" {
			t.Errorf("recovered = %v, want \"boom\"", r)
		}
	}()
	// Drive the same RunE-style flow End is designed for: defer
	// End in a function that panics. End must re-panic so the
	// caller's recover (this test's defer above) sees it.
	func() {
		ctx, _, _ := emitTestSetup(t)
		tx := BeginDev(ctx)
		defer tx.End(ctx)
		panic("boom")
	}()
}

func TestDevTx_EndStampsBaseAndEmitsOnHappyPath(t *testing.T) {
	ctx, c, mock := emitTestSetup(t)
	tx := BeginDev(ctx)
	if tx == nil {
		t.Fatal("BeginDev returned nil")
	}
	tx.End(ctx)
	if err := c.Flush(timeoutCtx(t, time.Second)); err != nil {
		t.Fatalf("flush: %v", err)
	}
	envs := mock.Envelopes()
	if len(envs) != 1 {
		t.Fatalf("envelopes = %d, want 1", len(envs))
	}
	props := testutil.MustType[DevCommandInvokedProperties](t, testutil.MustType[CommandInvoked](t, envs[0].Events[0]).Properties)
	if props.Command != CommandNameDev {
		t.Errorf("Command = %q, want dev", props.Command)
	}
	if props.Outcome != OutcomeSuccess {
		t.Errorf("Outcome = %q, want success", props.Outcome)
	}
	if props.InvokedBy != InvokedByDirect {
		t.Errorf("InvokedBy = %q, want direct", props.InvokedBy)
	}
	if props.InvokedVia != nil {
		t.Errorf("InvokedVia = %v, want nil (no flag, no default)", *props.InvokedVia)
	}
}

func TestDevTx_EndCtxCancelledMapsToUserInterrupt(t *testing.T) {
	parent, c, mock := emitTestSetup(t)
	ctx, cancel := context.WithCancel(parent)
	tx := BeginDev(ctx)
	cancel() // simulate SIGINT propagation
	tx.End(ctx)
	if err := c.Flush(timeoutCtx(t, time.Second)); err != nil {
		t.Fatalf("flush: %v", err)
	}
	props := testutil.MustType[DevCommandInvokedProperties](t, testutil.MustType[CommandInvoked](t, mock.Envelopes()[0].Events[0]).Properties)
	if props.Outcome != OutcomeUserInterrupt {
		t.Errorf("Outcome = %q, want user_interrupt", props.Outcome)
	}
}

func TestDevTx_EndPanicMapsToInternalError(t *testing.T) {
	ctx, c, mock := emitTestSetup(t)
	defer func() { _ = recover() }() // swallow the re-panic so the test reads the envelope
	func() {
		tx := BeginDev(ctx)
		defer tx.End(ctx)
		panic("kaboom")
	}()
	if err := c.Flush(timeoutCtx(t, time.Second)); err != nil {
		t.Fatalf("flush: %v", err)
	}
	envs := mock.Envelopes()
	if len(envs) != 1 {
		t.Fatalf("envelopes = %d, want 1", len(envs))
	}
	props := testutil.MustType[DevCommandInvokedProperties](t, testutil.MustType[CommandInvoked](t, envs[0].Events[0]).Properties)
	if props.Outcome != OutcomeInternalError {
		t.Errorf("Outcome = %q, want internal_error", props.Outcome)
	}
}

func TestDevTx_ExplicitSetOutcomeWins(t *testing.T) {
	// When the RunE body sets outcome explicitly (e.g.
	// db_disconnect on a partial subscription), End must NOT
	// override it with the ctx.Err / success default.
	parent, c, mock := emitTestSetup(t)
	ctx, cancel := context.WithCancel(parent)
	tx := BeginDev(ctx)
	tx.SetOutcome(OutcomeDBDisconnect) // explicit
	cancel()                           // would otherwise yield user_interrupt
	tx.End(ctx)
	if err := c.Flush(timeoutCtx(t, time.Second)); err != nil {
		t.Fatalf("flush: %v", err)
	}
	props := testutil.MustType[DevCommandInvokedProperties](t, testutil.MustType[CommandInvoked](t, mock.Envelopes()[0].Events[0]).Properties)
	if props.Outcome != OutcomeDBDisconnect {
		t.Errorf("Outcome = %q, want db_disconnect (explicit wins)", props.Outcome)
	}
}

func TestDevTx_SettersAccumulateThroughEnd(t *testing.T) {
	ctx, c, mock := emitTestSetup(t)
	tx := BeginDev(ctx)
	tx.SetProjectionCount(3)
	tx.SetFixtureCount(12)
	tx.SetConnectedToDB(true)
	tx.SetDBVersion("26.1")
	tx.End(ctx)
	if err := c.Flush(timeoutCtx(t, time.Second)); err != nil {
		t.Fatalf("flush: %v", err)
	}
	props := testutil.MustType[DevCommandInvokedProperties](t, testutil.MustType[CommandInvoked](t, mock.Envelopes()[0].Events[0]).Properties)
	if props.ProjectionCount == nil || *props.ProjectionCount != 3 {
		t.Errorf("ProjectionCount = %v, want &3", props.ProjectionCount)
	}
	if props.FixtureCount == nil || *props.FixtureCount != 12 {
		t.Errorf("FixtureCount = %v, want &12", props.FixtureCount)
	}
	if props.ConnectedToDB == nil || !*props.ConnectedToDB {
		t.Errorf("ConnectedToDB = %v, want &true", props.ConnectedToDB)
	}
	if props.DBVersion == nil || *props.DBVersion != "26.1" {
		t.Errorf("DBVersion = %v, want \"26.1\"", props.DBVersion)
	}
}

func TestTxBegin_AllVariantsReturnNilWithoutClient(t *testing.T) {
	// Sanity: every Begin* must guard on ClientFromContext.
	bg := context.Background()
	if BeginDev(bg) != nil {
		t.Error("BeginDev returned non-nil without client")
	}
	if BeginMCP(bg) != nil {
		t.Error("BeginMCP returned non-nil without client")
	}
	if BeginLSP(bg) != nil {
		t.Error("BeginLSP returned non-nil without client")
	}
	if BeginDebug(bg) != nil {
		t.Error("BeginDebug returned non-nil without client")
	}
}

func TestTxSetters_NilReceiverIsNoop(t *testing.T) {
	// Generator emits a nil-guard at the top of every setter so
	// the cobra RunE can SetX on Begin's return without first
	// checking whether telemetry is off. Cover all four variants
	// + a representative field kind for each so a generator
	// regression that breaks one variant's nil-guard fails CI.
	var dev *DevTx
	dev.SetOutcome(OutcomeSuccess)
	dev.SetProjectionCount(3)                            // kindRawCount
	dev.SetConnectedToDB(true)                           // kindBool
	dev.SetDBVersion("26.1")                             // kindString
	dev.SetManifestFeaturesUsed([]string{"projections"}) // kindArrayOfString

	var mcp *MCPTx
	mcp.SetOutcome(OutcomeSuccess)
	mcp.SetToolCallCount(42)

	var lsp *LSPTx
	lsp.SetOutcome(OutcomeSuccess)
	lsp.SetCodeLensRequestCount(7)

	var debug *DebugTx
	debug.SetOutcome(OutcomeSuccess)
	debug.SetBreakpointCount(3)

	// None of those should have panicked.
}

func TestTxEnd_AllVariantsNilReceiverIsNoop(t *testing.T) {
	// Every End must tolerate a nil *Tx receiver.
	bg := context.Background()
	(*DevTx)(nil).End(bg)
	(*MCPTx)(nil).End(bg)
	(*LSPTx)(nil).End(bg)
	(*DebugTx)(nil).End(bg)
}
