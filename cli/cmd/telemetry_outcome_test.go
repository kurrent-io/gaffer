package cmd

import (
	"errors"
	"fmt"
	"reflect"
	"testing"

	gafferruntime "github.com/kurrent-io/gaffer/bindings/go"
	"github.com/kurrent-io/gaffer/cli/internal/config"
	"github.com/kurrent-io/gaffer/cli/internal/engine"
	"github.com/kurrent-io/gaffer/cli/internal/project"
	"github.com/kurrent-io/gaffer/cli/internal/telemetry"
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
		{"db-disconnect", fmt.Errorf("%w: subscription dropped", engine.ErrDBDisconnect), telemetry.OutcomeDBDisconnect},
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

	got := classifyOutcome(outcomeInputs{err: project.ErrNotInProject, tracker: tr})
	if got != telemetry.OutcomeManifestNotFound {
		t.Errorf("structural+projection: got %q, want manifest_not_found", got)
	}
}

func TestClassifyOutcome_ProjectionFault(t *testing.T) {
	tr := newProjErrTracker()
	tr.Record(&gafferruntime.ProjectionHandlerError{})

	got := classifyOutcome(outcomeInputs{err: errors.New("projection faulted"), tracker: tr})
	if got != telemetry.Outcome(telemetry.ProjectionOutcomeProjectionUserThrow) {
		t.Errorf("projection fault: got %q, want projection_user_throw", got)
	}
}

func TestClassifyOutcome_DAPProtocolErrorPlumbing(t *testing.T) {
	got := classifyOutcome(outcomeInputs{
		dapProtocolErr: errors.New("dap: read: unexpected EOF"),
	})
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

	got := classifyOutcome(outcomeInputs{
		err:            errors.New("projection faulted"),
		tracker:        tr,
		dapProtocolErr: errors.New("dap: read: closed"),
	})
	if got != telemetry.Outcome(telemetry.ProjectionOutcomeProjectionUserThrow) {
		t.Errorf("projection+dap: got %q, want projection_user_throw", got)
	}
}

func TestClassifyOutcome_BothClean(t *testing.T) {
	if got := classifyOutcome(outcomeInputs{}); got != telemetry.OutcomeSuccess {
		t.Errorf("clean exit: got %q, want success", got)
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
