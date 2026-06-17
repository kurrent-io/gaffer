package cmd

import (
	"errors"
	"sort"

	gafferruntime "github.com/kurrent-io/gaffer/bindings/go"
	"github.com/kurrent-io/gaffer/cli/internal/config"
	"github.com/kurrent-io/gaffer/cli/internal/engine"
	"github.com/kurrent-io/gaffer/cli/internal/project"
	"github.com/kurrent-io/gaffer/cli/internal/prompt"
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
// internal_error doesn't pass through this helper - it's set by the
// global panic-recover handler in main. user_interrupt arrives two
// ways: dev's Tx outcome cascade (SIGINT mid-run), and one-shot
// commands whose interactive prompt was aborted, which surfaces as
// prompt.ErrCancelled and classifies here via classifyStructural.
type outcomeInputs struct {
	err            error
	tracker        *projErrTracker
	dapProtocolErr error
}

// classifyOutcome decides the final Outcome for a command_invoked
// event from the set of signals the wrapper hands it. Returns
// (outcome, true) when a signal matched; (zero, false) when nothing
// matched and the caller should pick a fallback (one-shots and dev
// default to user_error; mcp to mcp_protocol_error).
//
// Precedence when signals are present:
//
//   - nil err + nil protocol err -> success
//   - structural sentinel match  -> manifest_*, db_*, project
//   - tracked projection fault   -> projection_*
//   - dap protocol error         -> dap_protocol_error
//
// Structural beats projection because a manifest-load failure that
// happened to trigger a projection-iteration fault on the way down
// is still primarily a manifest problem. Projection beats DAP
// protocol error because a session that died because user code
// threw is "user friction", not "our DAP layer is broken" - DAP
// failures get their own observability via the exception event.
//
// Caller-picked fallback (rather than baking user_error in here) so
// classifyMCPOutcome can promote unclassified errors to
// mcp_protocol_error without inspecting a sentinel return value -
// avoids the magic-value coupling where "got back user_error" had
// to mean "no signal matched".
func classifyOutcome(in outcomeInputs) (telemetry.Outcome, bool) {
	if in.err == nil && in.dapProtocolErr == nil {
		return telemetry.OutcomeSuccess, true
	}
	if in.err != nil {
		if out, ok := classifyStructural(in.err); ok {
			return out, true
		}
		if last := in.tracker.last(); last != "" {
			return telemetry.Outcome(last), true
		}
	}
	if in.dapProtocolErr != nil {
		return telemetry.OutcomeDAPProtocolError, true
	}
	return "", false
}

// outcomeFor is the shorthand for one-shot commands that only have a
// final err to classify. Falls back to user_error for unclassified
// non-nil errors.
func outcomeFor(err error) telemetry.Outcome {
	if out, ok := classifyOutcome(outcomeInputs{err: err}); ok {
		return out
	}
	return telemetry.OutcomeUserError
}

// classifyMCPOutcome picks the command_invoked outcome for `gaffer
// mcp`. Falls through classifyOutcome's structural / projection
// ladder first; only if no signal matched and runErr is non-nil
// does it map to mcp_protocol_error. Previously the wrapper
// unconditionally stamped mcp_protocol_error on any non-nil runErr,
// which mis-classified manifest / db errors that surfaced through
// the MCP server's startup path as protocol failures.
func classifyMCPOutcome(runErr error, tracker *projErrTracker) telemetry.Outcome {
	if out, ok := classifyOutcome(outcomeInputs{err: runErr, tracker: tracker}); ok {
		return out
	}
	return telemetry.OutcomeMCPProtocolError
}

// classifyLSPOutcome is the LSP-side equivalent of classifyMCPOutcome:
// route runErr through classifyOutcome (so manifest / db / user_interrupt
// classify correctly), fall back to lsp_protocol_error only when no
// signal matched. LSP has no projection-error tracker - the LSP server
// doesn't run projections.
func classifyLSPOutcome(runErr error) telemetry.Outcome {
	if out, ok := classifyOutcome(outcomeInputs{err: runErr}); ok {
		return out
	}
	return telemetry.OutcomeLSPProtocolError
}

// oneShotDefer is the panic-safe wrapper around the deferred emit on
// every one-shot cobra command (version / init / scaffold / info /
// manifest). Three jobs:
//
//   - recover any panic from the cobra body so the deferred emit
//     fires before the panic re-propagates (Go only writes named
//     returns on explicit `return`; without this, a panic would leave
//     retErr nil and the wrapper would ship outcome=success
//     contradicting the exception envelope main.go later emits)
//   - classify the outcome - recovered panic wins as
//     internal_error; otherwise route retErr through outcomeFor
//   - re-panic so main.go's global recover catches it, fires the
//     exception envelope, and the process exits with the original
//     panic stack
//
// MUST be called as `defer oneShotDefer(&retErr, ...)` directly -
// the recover-frame contract is the same load-bearing one as the
// long-running Tx.End() (see cli/internal/telemetry/doc.go).
func oneShotDefer(retErr *error, emit func(telemetry.Outcome)) {
	r := recover()
	defer func() {
		if r != nil {
			panic(r)
		}
	}()
	outcome := telemetry.OutcomeSuccess
	if r != nil {
		outcome = telemetry.OutcomeInternalError
	} else if retErr != nil {
		outcome = outcomeFor(*retErr)
	}
	emit(outcome)
}

// classifyStructural maps known-shape errors to specific outcomes via
// errors.Is on package-level sentinels. Returns ok=false when none
// match so callers can fall through to a more specific classifier
// (e.g. projection-error mapping) or to user_error.
func classifyStructural(err error) (telemetry.Outcome, bool) {
	// An auth-required failure is a typed error (no longer wrapped as
	// ErrDBConnect), but it's still a failure to connect, so it keeps that
	// bucket - the classification it had before becoming distinct. A dedicated
	// outcome would need a telemetry schema change.
	var authErr *engine.AuthRequiredError
	if errors.As(err, &authErr) {
		return telemetry.OutcomeDBConnectError, true
	}
	switch {
	case errors.Is(err, prompt.ErrCancelled):
		return telemetry.OutcomeUserInterrupt, true
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
// dev / debug / mcp session has seen. Backed by a map for dedupe and
// drained as a sorted slice at End time. Single-goroutine at Record
// time:
//
//   - dev / debug: the inner runner invokes Record synchronously via
//     source.Run on the cobra wrapper's goroutine.
//   - mcp: tool-call goroutines append to mcpserver.Server's
//     mutex-guarded slice; the cobra wrapper drains that slice in a
//     for-loop and feeds each entry to Record on the wrapper's
//     goroutine.
//
// Either way the tracker itself sees one goroutine, no locking.
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

// diagSeenTracker records the distinct diagnostic codes (quirk.* / usage.*) a
// dev / debug session surfaced - the compile-time set from ProjectionInfo plus
// the runtime quirks the runner reports off each FeedResult / faulting error.
// Backed by a map for dedupe, drained sorted at End time. Single-goroutine at
// Record time, same ownership as projErrTracker: the runner fires it via
// source.Run, which processes events synchronously on the cobra wrapper's
// goroutine (the DAP serve goroutine drives protocol, not feeds).
type diagSeenTracker struct {
	seen map[string]struct{}
}

func newDiagSeenTracker() *diagSeenTracker {
	return &diagSeenTracker{seen: map[string]struct{}{}}
}

// Record adds a diagnostic code to the set. Empty codes are ignored. Nil-safe.
func (t *diagSeenTracker) Record(code string) {
	if t == nil || code == "" {
		return
	}
	t.seen[code] = struct{}{}
}

// Sorted returns the recorded codes in stable lexical order, ready for
// SetDiagnosticsSeen. Returns nil (not an empty slice) when none were seen so
// the Tx setter's nil-check omits the field entirely.
func (t *diagSeenTracker) Sorted() []string {
	if t == nil || len(t.seen) == 0 {
		return nil
	}
	out := make([]string, 0, len(t.seen))
	for k := range t.seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
