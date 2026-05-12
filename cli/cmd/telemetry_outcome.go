package cmd

import (
	"errors"
	"sort"

	gafferruntime "github.com/kurrent-io/gaffer/bindings/go"
	"github.com/kurrent-io/gaffer/cli/internal/config"
	"github.com/kurrent-io/gaffer/cli/internal/engine"
	"github.com/kurrent-io/gaffer/cli/internal/project"
	"github.com/kurrent-io/gaffer/cli/internal/telemetry"
)

// outcomeInputs aggregates every signal that feeds into the
// command_invoked Outcome decision. Zero-value fields mean "no
// signal" - the classifier handles them gracefully. Callers fill in
// only what's relevant for their command:
//
//   - one-shot (version, init, ...): just err
//   - dev: err + tracker
//   - debug: err + tracker + dapProtocolErr
//
// internal_error / user_interrupt outcomes don't pass through this
// helper - they're set by the global panic-recover handler in main
// and the Tx outcome cascade respectively.
type outcomeInputs struct {
	err            error
	tracker        *projErrTracker
	dapProtocolErr error
}

// classifyOutcome decides the final Outcome for a command_invoked
// event. Precedence (highest first):
//
//   - nil err + nil protocol err -> success
//   - structural sentinel match  -> manifest_*, db_*, project
//   - tracked projection fault   -> projection_*
//   - dap protocol error         -> dap_protocol_error
//   - generic non-nil err        -> user_error
//
// Structural beats projection because a manifest-load failure that
// happened to trigger a projection-iteration fault on the way down
// is still primarily a manifest problem. Projection beats DAP
// protocol error because a session that died because user code
// threw is "user friction", not "our DAP layer is broken" - DAP
// failures get their own observability via the exception event.
func classifyOutcome(in outcomeInputs) telemetry.Outcome {
	if in.err == nil && in.dapProtocolErr == nil {
		return telemetry.OutcomeSuccess
	}
	if in.err != nil {
		if out, ok := classifyStructural(in.err); ok {
			return out
		}
		if last := in.tracker.last(); last != "" {
			return telemetry.Outcome(last)
		}
	}
	if in.dapProtocolErr != nil {
		return telemetry.OutcomeDAPProtocolError
	}
	return telemetry.OutcomeUserError
}

// outcomeFor is the shorthand for one-shot commands that only have a
// final err to classify. Equivalent to classifyOutcome{err: err}.
func outcomeFor(err error) telemetry.Outcome {
	return classifyOutcome(outcomeInputs{err: err})
}

// classifyStructural maps known-shape errors to specific outcomes via
// errors.Is on package-level sentinels. Returns ok=false when none
// match so callers can fall through to a more specific classifier
// (e.g. projection-error mapping) or to user_error.
func classifyStructural(err error) (telemetry.Outcome, bool) {
	switch {
	case errors.Is(err, project.ErrNotInProject):
		return telemetry.OutcomeManifestNotFound, true
	case errors.Is(err, config.ErrManifestParse):
		return telemetry.OutcomeManifestParseError, true
	case errors.Is(err, config.ErrManifestValidate):
		return telemetry.OutcomeManifestValidationError, true
	case errors.Is(err, engine.ErrDBDisconnect):
		return telemetry.OutcomeDBDisconnect, true
	case errors.Is(err, engine.ErrDBConnect):
		return telemetry.OutcomeDBConnectError, true
	}
	return "", false
}

// projectionOutcomeFor maps an FFI projection error to its
// ProjectionOutcome bucket. Coarse on purpose - three buckets cover
// compile-time failures, runtime user throws, and the catch-all -
// matching the schema's intentionally coarse #ProjectionOutcome. The
// JS error type granularity that the runtime doesn't expose today
// isn't useful for the product-analytics question this enum answers
// ("where do users hit friction").
//
// errors.As (not type switch) so a future wrapping at the FFI
// boundary doesn't silently break the classifier.
func projectionOutcomeFor(err error) (telemetry.ProjectionOutcome, bool) {
	var (
		invalidProj *gafferruntime.InvalidProjectionError
		compileTO   *gafferruntime.CompilationTimeoutError
		handlerErr  *gafferruntime.ProjectionHandlerError
		transformEr *gafferruntime.ProjectionTransformError
		execTO      *gafferruntime.ExecutionTimeoutError
		malformed   *gafferruntime.MalformedEventError
		stateSer    *gafferruntime.StateSerializationError
		invalidArg  *gafferruntime.InvalidArgumentError
		unexpected  *gafferruntime.UnexpectedError
	)
	switch {
	case errors.As(err, &invalidProj), errors.As(err, &compileTO):
		return telemetry.ProjectionOutcomeProjectionCompileError, true
	case errors.As(err, &handlerErr), errors.As(err, &transformEr):
		return telemetry.ProjectionOutcomeProjectionUserThrow, true
	case errors.As(err, &execTO),
		errors.As(err, &malformed),
		errors.As(err, &stateSer),
		errors.As(err, &invalidArg),
		errors.As(err, &unexpected):
		return telemetry.ProjectionOutcomeProjectionUnknownError, true
	}
	return "", false
}

// projErrTracker records the distinct projection_* outcomes a
// dev / debug session has seen. Backed by a map for dedupe and
// drained as a sorted slice at End time. Single-goroutine-owned -
// the cobra wrapper owns the Tx and the tracker; the inner runner
// invokes Record synchronously via source.Run on the same goroutine.
type projErrTracker struct {
	seen map[telemetry.ProjectionOutcome]struct{}
}

func newProjErrTracker() *projErrTracker {
	return &projErrTracker{seen: map[telemetry.ProjectionOutcome]struct{}{}}
}

// Record classifies err and adds the resulting bucket to the set.
// Anything that looks like a projection error but doesn't match a
// known FFI category lands in projection_unknown_error so adding a
// new FFI type doesn't silently drop telemetry.
//
// Callers should only invoke this with errors known to come from a
// projection-iteration fault; calling with a manifest or db error
// would mis-bucket as projection_unknown_error.
func (t *projErrTracker) Record(err error) {
	if err == nil {
		return
	}
	out, ok := projectionOutcomeFor(err)
	if !ok {
		out = telemetry.ProjectionOutcomeProjectionUnknownError
	}
	t.seen[out] = struct{}{}
}

// Sorted returns the recorded outcomes in stable lexical order, ready
// for SetProjectionErrorsSeen. Returns nil (not an empty slice) when
// no projection errors were observed so the Tx setter's nil-check
// can omit the field entirely.
func (t *projErrTracker) Sorted() []telemetry.ProjectionOutcome {
	if t == nil || len(t.seen) == 0 {
		return nil
	}
	out := make([]telemetry.ProjectionOutcome, 0, len(t.seen))
	for k := range t.seen {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// last returns the lexically-last recorded outcome, used by
// classifyOutcome to pick a representative final outcome when a
// projection fault is the command's exit cause. Multiple distinct
// faults are surfaced via projection_errors_seen; the final outcome
// is one bucket by definition.
func (t *projErrTracker) last() telemetry.ProjectionOutcome {
	out := t.Sorted()
	if len(out) == 0 {
		return ""
	}
	return out[len(out)-1]
}
