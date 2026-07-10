package cmd

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"testing"

	gafferruntime "github.com/kurrent-io/gaffer/bindings/go"
	"github.com/kurrent-io/gaffer/cli/internal/config"
	"github.com/kurrent-io/gaffer/cli/internal/engine"
	"github.com/kurrent-io/gaffer/cli/internal/project"
	"github.com/kurrent-io/gaffer/cli/internal/prompt"
	"github.com/kurrent-io/gaffer/cli/internal/target"
	"github.com/kurrent-io/gaffer/cli/internal/telemetry"
	"github.com/kurrent-io/gaffer/cli/internal/testutil"
)

func TestOutcomeFor_NilIsSuccess(t *testing.T) {
	if got := outcomeFor(nil); got != telemetry.OutcomeSuccess {
		t.Errorf("outcomeFor(nil) = %q, want success", got)
	}
}

func TestOutcomeFor_StructuralSentinels(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want telemetry.Outcome
	}{
		{"not-in-project", project.ErrNotInProject, telemetry.OutcomeManifestNotFound},
		{"manifest-parse", fmt.Errorf("%w: bad toml", config.ErrManifestParse), telemetry.OutcomeManifestParseError},
		{"manifest-validate", fmt.Errorf("%w: bad config", config.ErrManifestValidate), telemetry.OutcomeManifestValidationError},
		{"db-connect", fmt.Errorf("%w: dns", engine.ErrDBConnect), telemetry.OutcomeDBConnectError},
		{"auth-required", &target.AuthRequiredError{Env: "prod"}, telemetry.OutcomeDBConnectError},
		{"db-disconnect", fmt.Errorf("%w: subscription dropped", engine.ErrDBDisconnect), telemetry.OutcomeDBDisconnect},
		{"prompt-cancelled", prompt.ErrCancelled, telemetry.OutcomeUserInterrupt},
		{"prompt-cancelled-wrapped", fmt.Errorf("%w", prompt.ErrCancelled), telemetry.OutcomeUserInterrupt},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := outcomeFor(tc.err); got != tc.want {
				t.Errorf("outcomeFor(%v) = %q, want %q", tc.err, got, tc.want)
			}
		})
	}
}

func TestOutcomeFor_GenericErrorFallsThroughToUserError(t *testing.T) {
	if got := outcomeFor(errors.New("anything else")); got != telemetry.OutcomeUserError {
		t.Errorf("outcomeFor(generic) = %q, want user_error", got)
	}
}

func TestOutcomeFor_ExitCodeDrivenOutcomes(t *testing.T) {
	// The deploy family carries a non-nil error to set the process exit code;
	// the classifier keys `refused` (3) and dry-run success (2) off the same
	// ExitCodeFor predicate so telemetry stays in lockstep with the CI contract.
	cases := []struct {
		name string
		err  error
		want telemetry.Outcome
	}{
		{"deploy-need-confirm", errNeedConfirm, telemetry.OutcomeRefused},
		{"operate-need-confirm", errOperateNeedsConfirm, telemetry.OutcomeRefused},
		{"need-confirm-wrapped", fmt.Errorf("deploy: %w", errNeedConfirm), telemetry.OutcomeRefused},
		{"prod-no-validate", refuseNoValidateOnProd("Deploy", "projections are", "prod"), telemetry.OutcomeRefused},
		{"dry-run-changes-pending", exitWith(2, silent(errors.New("changes pending"))), telemetry.OutcomeSuccess},
		{"dry-run-blocked", exitWith(1, silent(errors.New("blocked"))), telemetry.OutcomeUserError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := outcomeFor(tc.err); got != tc.want {
				t.Errorf("outcomeFor(%v) = %q, want %q", tc.err, got, tc.want)
			}
		})
	}
}

func TestProjectionOutcomeFor_FFICategories(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want telemetry.ProjectionOutcome
		ok   bool
	}{
		{"invalid-projection", &gafferruntime.InvalidProjectionError{}, telemetry.ProjectionOutcomeProjectionCompileError, true},
		{"compilation-timeout", &gafferruntime.CompilationTimeoutError{}, telemetry.ProjectionOutcomeProjectionCompileError, true},
		{"handler-error", &gafferruntime.ProjectionHandlerError{}, telemetry.ProjectionOutcomeProjectionUserThrow, true},
		{"transform-error", &gafferruntime.ProjectionTransformError{}, telemetry.ProjectionOutcomeProjectionUserThrow, true},
		{"execution-timeout", &gafferruntime.ExecutionTimeoutError{}, telemetry.ProjectionOutcomeProjectionUnknownError, true},
		{"malformed-event", &gafferruntime.MalformedEventError{}, telemetry.ProjectionOutcomeProjectionUnknownError, true},
		{"state-serialization", &gafferruntime.StateSerializationError{}, telemetry.ProjectionOutcomeProjectionUnknownError, true},
		{"unexpected", &gafferruntime.UnexpectedError{}, telemetry.ProjectionOutcomeProjectionUnknownError, true},
		{"non-projection", errors.New("other"), "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := projectionOutcomeFor(tc.err)
			if got != tc.want || ok != tc.ok {
				t.Errorf("projectionOutcomeFor(%T) = (%q, %v), want (%q, %v)", tc.err, got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestProjErrTracker_DedupeAndSort(t *testing.T) {
	tr := newProjErrTracker()
	tr.Record(&gafferruntime.ProjectionHandlerError{})
	tr.Record(&gafferruntime.ProjectionHandlerError{}) // dupe
	tr.Record(&gafferruntime.InvalidProjectionError{})

	got := tr.Sorted()
	want := []telemetry.ProjectionOutcome{
		telemetry.ProjectionOutcomeProjectionCompileError,
		telemetry.ProjectionOutcomeProjectionUserThrow,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Sorted() = %v, want %v", got, want)
	}
}

func TestProjErrTracker_EmptyReturnsNil(t *testing.T) {
	tr := newProjErrTracker()
	if got := tr.Sorted(); got != nil {
		t.Errorf("Sorted() on empty = %v, want nil", got)
	}
	if got := tr.last(); got != "" {
		t.Errorf("last() on empty = %q, want empty", got)
	}
}

func TestClassifyOutcome_StructuralWinsOverProjection(t *testing.T) {
	tr := newProjErrTracker()
	tr.Record(&gafferruntime.ProjectionHandlerError{})

	got, ok := classifyOutcome(outcomeInputs{err: project.ErrNotInProject, tracker: tr})
	if !ok {
		t.Fatal("ok = false; expected structural match")
	}
	if got != telemetry.OutcomeManifestNotFound {
		t.Errorf("structural+projection: got %q, want manifest_not_found", got)
	}
}

func TestClassifyOutcome_ProjectionFault(t *testing.T) {
	tr := newProjErrTracker()
	tr.Record(&gafferruntime.ProjectionHandlerError{})

	got, ok := classifyOutcome(outcomeInputs{err: errors.New("projection faulted"), tracker: tr})
	if !ok {
		t.Fatal("ok = false; expected projection match via tracker")
	}
	if got != telemetry.Outcome(telemetry.ProjectionOutcomeProjectionUserThrow) {
		t.Errorf("projection fault: got %q, want projection_user_throw", got)
	}
}

func TestClassifyOutcome_DAPProtocolErrorPlumbing(t *testing.T) {
	got, ok := classifyOutcome(outcomeInputs{
		dapProtocolErr: errors.New("dap: read: unexpected EOF"),
	})
	if !ok {
		t.Fatal("ok = false; expected dap match")
	}
	if got != telemetry.OutcomeDAPProtocolError {
		t.Errorf("dap proto error only: got %q, want dap_protocol_error", got)
	}
}

func TestClassifyOutcome_ProjectionFaultBeatsDAPProtocolError(t *testing.T) {
	// If a projection failed AND the DAP connection got messy on
	// the way out, the primary signal is the projection failure -
	// dap_protocol_error would mask the real cause.
	tr := newProjErrTracker()
	tr.Record(&gafferruntime.ProjectionHandlerError{})

	got, ok := classifyOutcome(outcomeInputs{
		err:            errors.New("projection faulted"),
		tracker:        tr,
		dapProtocolErr: errors.New("dap: read: closed"),
	})
	if !ok {
		t.Fatal("ok = false; expected projection match")
	}
	if got != telemetry.Outcome(telemetry.ProjectionOutcomeProjectionUserThrow) {
		t.Errorf("projection+dap: got %q, want projection_user_throw", got)
	}
}

func TestClassifyOutcome_BothCleanReportsSuccess(t *testing.T) {
	got, ok := classifyOutcome(outcomeInputs{})
	if !ok {
		t.Fatal("ok = false; nil signals should still match (success)")
	}
	if got != telemetry.OutcomeSuccess {
		t.Errorf("clean exit: got %q, want success", got)
	}
}

func TestClassifyOutcome_UnclassifiedNonNilErrorReportsNotOk(t *testing.T) {
	// A non-nil err that doesn't match structural / projection /
	// dap signals returns ok=false so the caller picks its fallback
	// (user_error for dev, mcp_protocol_error for mcp).
	got, ok := classifyOutcome(outcomeInputs{err: errors.New("some other failure")})
	if ok {
		t.Errorf("ok = true; got %q, want ok=false for unclassified err", got)
	}
}

func TestProjErrTracker_UnknownErrorMapsToUnknown(t *testing.T) {
	// Reviewer-caught gap: a runner returning a plain error (rather
	// than one of the typed FFI categories) was previously dropped
	// silently. Adding a new FFI type without updating the
	// classifier would silently lose telemetry; the unknown bucket
	// keeps that visible.
	tr := newProjErrTracker()
	tr.Record(errors.New("brand-new FFI error type"))
	got := tr.Sorted()
	want := []telemetry.ProjectionOutcome{telemetry.ProjectionOutcomeProjectionUnknownError}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Sorted() = %v, want %v", got, want)
	}
}

func TestProjErrTracker_NilIsNoop(t *testing.T) {
	tr := newProjErrTracker()
	tr.Record(nil)
	if got := tr.Sorted(); got != nil {
		t.Errorf("Record(nil) shouldn't add anything; got %v", got)
	}
}

func TestProjectionOutcomeFor_HandlesWrappedErrors(t *testing.T) {
	// errors.As over type switch means a wrapped FFI error (e.g.
	// fmt.Errorf("during init: %w", &ProjectionHandlerError{})) is
	// still classified correctly.
	wrapped := fmt.Errorf("during init: %w", &gafferruntime.ProjectionHandlerError{})
	got, ok := projectionOutcomeFor(wrapped)
	if !ok {
		t.Fatal("ok = false, want true")
	}
	if got != telemetry.ProjectionOutcomeProjectionUserThrow {
		t.Errorf("got %q, want projection_user_throw", got)
	}
}

func TestClassifyMCPOutcome_StructuralBeatsProtocolError(t *testing.T) {
	// A manifest_not_found that surfaced through MCP startup should
	// stay manifest_not_found, not get mis-classified as
	// mcp_protocol_error - this was the regression the audit found.
	got := classifyMCPOutcome(project.ErrNotInProject, newProjErrTracker())
	if got != telemetry.OutcomeManifestNotFound {
		t.Errorf("got %q, want manifest_not_found", got)
	}
}

func TestClassifyMCPOutcome_ProjectionFaultBeatsProtocolError(t *testing.T) {
	tr := newProjErrTracker()
	tr.Record(&gafferruntime.ProjectionHandlerError{})

	got := classifyMCPOutcome(errors.New("session ended"), tr)
	if got != telemetry.Outcome(telemetry.ProjectionOutcomeProjectionUserThrow) {
		t.Errorf("got %q, want projection_user_throw", got)
	}
}

func TestClassifyMCPOutcome_FallbackIsProtocolError(t *testing.T) {
	// Generic non-nil runErr with no structural / projection signal
	// is the legitimate mcp_protocol_error case.
	got := classifyMCPOutcome(errors.New("mcp framing went wrong"), newProjErrTracker())
	if got != telemetry.OutcomeMCPProtocolError {
		t.Errorf("got %q, want mcp_protocol_error", got)
	}
}

func TestClassifyMCPOutcome_NilIsSuccess(t *testing.T) {
	if got := classifyMCPOutcome(nil, newProjErrTracker()); got != telemetry.OutcomeSuccess {
		t.Errorf("got %q, want success", got)
	}
}

func TestClassifyLSPOutcome_StructuralBeatsProtocolError(t *testing.T) {
	// Same regression class as MCP: manifest_not_found surfacing
	// from the LSP startup path should stay manifest_not_found, not
	// get mis-classified as lsp_protocol_error.
	got := classifyLSPOutcome(project.ErrNotInProject)
	if got != telemetry.OutcomeManifestNotFound {
		t.Errorf("got %q, want manifest_not_found", got)
	}
}

func TestClassifyLSPOutcome_FallbackIsProtocolError(t *testing.T) {
	got := classifyLSPOutcome(errors.New("jsonrpc framing went wrong"))
	if got != telemetry.OutcomeLSPProtocolError {
		t.Errorf("got %q, want lsp_protocol_error", got)
	}
}

func TestClassifyLSPOutcome_NilIsSuccess(t *testing.T) {
	if got := classifyLSPOutcome(nil); got != telemetry.OutcomeSuccess {
		t.Errorf("got %q, want success", got)
	}
}

func TestOneShotDefer_PanicEmitsInternalErrorAndRePanics(t *testing.T) {
	var seen telemetry.Outcome
	emit := func(o telemetry.Outcome) { seen = o }

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic to propagate; got nil")
		}
		if seen != telemetry.OutcomeInternalError {
			t.Errorf("outcome = %q, want internal_error", seen)
		}
	}()

	func() {
		var retErr error
		defer oneShotDefer(&retErr, emit)
		panic("boom")
	}()
}

func TestOneShotDefer_NilErrorEmitsSuccess(t *testing.T) {
	var seen telemetry.Outcome
	func() {
		var retErr error
		defer oneShotDefer(&retErr, func(o telemetry.Outcome) { seen = o })
	}()
	if seen != telemetry.OutcomeSuccess {
		t.Errorf("outcome = %q, want success", seen)
	}
}

func TestOneShotDefer_NonNilErrorClassifies(t *testing.T) {
	var seen telemetry.Outcome
	func() {
		retErr := project.ErrNotInProject
		defer oneShotDefer(&retErr, func(o telemetry.Outcome) { seen = o })
	}()
	if seen != telemetry.OutcomeManifestNotFound {
		t.Errorf("outcome = %q, want manifest_not_found", seen)
	}
}

func TestOneShotDefer_PanicBeatsNonNilError(t *testing.T) {
	// Precedence rule: a recovered panic wins over any non-nil
	// retErr - the panic is the bigger signal and would have left
	// retErr stale anyway. Without this, a future refactor that
	// swaps the if/else ordering would silently misclassify
	// recovered panics as user_error.
	var seen telemetry.Outcome

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic to propagate")
		}
		if seen != telemetry.OutcomeInternalError {
			t.Errorf("outcome = %q, want internal_error (panic > retErr)", seen)
		}
	}()

	func() {
		retErr := project.ErrNotInProject
		defer oneShotDefer(&retErr, func(o telemetry.Outcome) { seen = o })
		panic("boom")
	}()
}

func TestDiagSeenTracker_DedupeAndSort(t *testing.T) {
	tr := newDiagSeenTracker()
	tr.Record("usage.handler.async")
	tr.Record("quirk.serialize.rawString")
	tr.Record("usage.handler.async") // dupe
	tr.Record("quirk.log.multiParam")

	got := tr.Sorted()
	want := []string{
		"quirk.log.multiParam",
		"quirk.serialize.rawString",
		"usage.handler.async",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Sorted() = %v, want %v", got, want)
	}
}

func TestDiagSeenTracker_EmptyReturnsNil(t *testing.T) {
	tr := newDiagSeenTracker()
	if got := tr.Sorted(); got != nil {
		t.Errorf("Sorted() on empty = %v, want nil", got)
	}
}

func TestDiagSeenTracker_IgnoresEmptyCode(t *testing.T) {
	tr := newDiagSeenTracker()
	tr.Record("")
	if got := tr.Sorted(); got != nil {
		t.Errorf(`Record("") shouldn't add anything; got %v`, got)
	}
}

func TestDiagnosticsSeen_CompileTimeAndRuntimeUnion(t *testing.T) {
	// The full set diagnostics_seen reports: compile-time usage.options.duplicate
	// (two options() calls, off ProjectionInfo via recordCompileDiagnostics) plus
	// runtime quirk.log.multiParam (multi-arg log per event, off the runner's
	// FeedResult via RunnerConfig.OnDiagnostic).
	source := `options({});
options({});
fromAll().when({ $any: function (s, e) { log("a", "b"); return s; } });`
	opts := `{"engineVersion":2}`

	session, err := gafferruntime.NewSession(source, &opts)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	defer session.Destroy()
	info := session.GetSources()

	tr := newDiagSeenTracker()
	recordCompileDiagnostics(info, tr.Record)

	r := engine.NewRunner(engine.RunnerConfig{
		Feed:         engine.FeedFn(session.Feed),
		Session:      session,
		Info:         info,
		OnDiagnostic: tr.Record,
	})
	r.ProcessOne(testutil.Event("X", "s-1", 0))

	got := tr.Sorted()
	if !slices.Contains(got, "usage.options.duplicate") {
		t.Errorf("want usage.options.duplicate (compile-time) in %v", got)
	}
	if !slices.Contains(got, "quirk.log.multiParam") {
		t.Errorf("want quirk.log.multiParam (runtime) in %v", got)
	}
}

func TestDiagnosticsSeen_ThrowingQuirkViaError(t *testing.T) {
	// A throwing quirk faults the event but still carries its code on the error;
	// the runner reports it off err.ErrorDiagnostics so it lands in the set too.
	source := `fromAll().when({ $any: function (s, e) { linkStreamTo("a", e.streamId, { x: 1 }); return s; } });`
	opts := `{"engineVersion":2}`

	session, err := gafferruntime.NewSession(source, &opts)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	defer session.Destroy()
	info := session.GetSources()

	tr := newDiagSeenTracker()
	r := engine.NewRunner(engine.RunnerConfig{
		Feed:         engine.FeedFn(session.Feed),
		Session:      session,
		Info:         info,
		OnDiagnostic: tr.Record,
	})
	r.ProcessOne(testutil.Event("X", "s-1", 0))

	if !slices.Contains(tr.Sorted(), "quirk.linkStreamTo.outOfBoundsParameters") {
		t.Errorf("want quirk.linkStreamTo.outOfBoundsParameters from throwing quirk; got %v", tr.Sorted())
	}
}
