package engine

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"sync"
	"sync/atomic"

	gafferruntime "github.com/kurrent-io/gaffer/bindings/go"
	"github.com/kurrent-io/gaffer/cli/internal/history"
)

type FeedFn func(string) (*gafferruntime.FeedResult, error)

type EventSource interface {
	Run(ctx context.Context, process func(string) bool) error
}

type EventWriter interface {
	OnEvent(eventJSON string)
	OnResult(eventID string, result *gafferruntime.FeedResult)
	OnError(eventID string, code, description string)
}

type RunnerConfig struct {
	Feed          FeedFn
	Session       *gafferruntime.Session
	Info          gafferruntime.ProjectionInfo
	EngineVersion int
	Writer        EventWriter
	History       *history.Store
	Debug         *DebugConfig
	// OnDiagnostic, if set, fires for each diagnostic that fires while processing
	// an event - both the quirks on a successful FeedResult and those carried on a
	// throwing quirk's error. It's a stream of occurrences, not distinct codes: a
	// code can repeat within one event, so dedupe if you need a distinct set.
	// Independent of the writer, so collection doesn't depend on the output format.
	// Called inline from ProcessOne (i.e. the event source's goroutine), like the
	// writer callbacks.
	OnDiagnostic func(code string)
}

// Runner drives a projection session over an event source and exposes the
// run-state and debug-control surface the CLI, MCP and DAP front-ends consume.
//
// Two distinct guarantees protect concurrent access, and they are not the same
// mechanism:
//
//   - r.mu guards the Runner's own mutable fields (stats, partitions, faulted,
//     status, paused, ...). It does NOT make the underlying runtime session
//     safe to touch from another goroutine.
//
//   - The pause invariant guards the session. The runtime session is not
//     thread-safe (see bindings/go), so cross-goroutine inspection is only
//     safe while the engine is paused: the feed goroutine is parked inside
//     Feed at a breakpoint and a second goroutine (DAP/MCP command loop) reads
//     state. The inspection methods (Evaluate, GetCallStack, GetScopes,
//     GetVariables, CollectState, GetPartitionState) rely on this - they do not
//     hold r.mu across the FFI call, and could not make the FFI safe if they
//     did. Callers must only invoke them while Paused() is true. r.mu is used
//     only to snapshot Runner fields the FFI call needs (e.g. the partition
//     set), then released before crossing into the session.
//
// The mutable state is split into two concerns, both guarded by r.mu: run
// holds what processing produces (stats, partitions, fault, status), control
// holds the debug pause/break wiring. step is an atomic.Int64 read lock-free
// (see Step), deliberately outside the mutex.
type Runner struct {
	feed          FeedFn
	session       *gafferruntime.Session
	info          gafferruntime.ProjectionInfo
	engineVersion int
	writer        EventWriter
	history       *history.Store
	debug         *DebugConfig
	onDiagnostic  func(code string)

	step atomic.Int64

	mu      sync.Mutex
	run     runState
	control debugControl
}

// runState is the Runner's per-run progress, mutated as events are processed
// and read back by the front-ends. Guarded by Runner.mu.
type runState struct {
	stats      EventStats
	partitions map[string]bool
	faulted    bool
	lastError  error
	// status is externally driven: the MCP and dev front-ends set it from the
	// source lifecycle (running, caught_up, completed, ...), which the Runner
	// does not observe itself. Stored here only so a single reader sees a
	// consistent value.
	status string
}

// debugControl is the Runner's debug pause/break wiring. Guarded by Runner.mu.
type debugControl struct {
	paused      bool
	pausedEvent string
	breakAtStep int64
	// draining is set by Drain() on teardown. Once set, the OnBreak
	// handler resumes the engine instead of parking it, so a blocked
	// Feed can run to completion and the feed goroutine can exit.
	draining bool
}

type EventStats struct {
	Handled int
	Skipped int
	Errors  int
	// SkippedByReason breaks Skipped down by the FeedResult.SkipReason
	// the runtime returned (link, no-handler, no-partition, etc).
	// Only consumers that explicitly want the breakdown look at this -
	// most callers just read Skipped.
	SkippedByReason map[string]int
}

func (s EventStats) Total() int {
	return s.Handled + s.Skipped + s.Errors
}

func NewRunner(cfg RunnerConfig) *Runner {
	r := &Runner{
		feed:          cfg.Feed,
		session:       cfg.Session,
		info:          cfg.Info,
		engineVersion: cfg.EngineVersion,
		writer:        cfg.Writer,
		history:       cfg.History,
		debug:         cfg.Debug,
		onDiagnostic:  cfg.OnDiagnostic,
		run:           runState{partitions: make(map[string]bool)},
	}
	if r.debug != nil && r.debug.OnBreak != nil {
		r.debug.Session.OnBreak(func(info gafferruntime.BreakInfo) {
			// Hold the lock across the whole decision: reading draining
			// and setting paused must be atomic with respect to Drain,
			// or Drain could observe paused=false (and skip its resume)
			// in the window before we park, deadlocking the feed goroutine.
			r.mu.Lock()
			// During teardown, resume rather than park - including the
			// step the break_at pause converts into - so the blocked
			// Feed returns and the feed goroutine can exit. The resume
			// error is irrelevant: the session is going away.
			if r.control.draining {
				r.control.paused = false
				r.mu.Unlock()
				go func() { _ = r.debug.Session.Continue() }()
				return
			}
			if info.Reason == "pause" && r.control.breakAtStep > 0 {
				r.mu.Unlock()
				// Auto-step off the pause-breakpoint to land at the break_at
				// target with full context. Must run on its own goroutine -
				// StepInto blocks on the engine thread that's currently running
				// this callback. A returned error has no caller here, so route
				// it through OnError (e.g. MCP's error channel).
				go func() {
					if err := r.debug.Session.StepInto(); err != nil && r.debug.OnError != nil {
						r.debug.OnError(err)
					}
				}()
				return
			}
			r.control.paused = true
			r.mu.Unlock()
			r.debug.OnBreak(info)
		})
	}
	return r
}

func (r *Runner) ProcessOne(eventJSON string) (stop bool) {
	r.mu.Lock()
	step := r.step.Add(1)
	var pauseErr error
	if r.debug != nil {
		r.control.pausedEvent = eventJSON
		if r.control.breakAtStep > 0 && step == r.control.breakAtStep {
			pauseErr = r.debug.Session.Pause()
		}
	}
	r.mu.Unlock()
	// Route a failed break_at pause through OnError, like the auto-step path:
	// ProcessOne runs on the feed goroutine with no caller to return to, and a
	// swallowed failure would leave the consumer waiting for a break that never
	// arrives. (Pause only fails on a dead session, so this is a safety net.)
	if pauseErr != nil && r.debug.OnError != nil {
		r.debug.OnError(pauseErr)
	}

	if r.writer != nil {
		r.writer.OnEvent(eventJSON)
	}

	result, err := r.feed(eventJSON)

	r.mu.Lock()
	if r.debug != nil {
		r.control.pausedEvent = ""
	}

	if err != nil {
		fe := ClassifyError(err)
		if r.history != nil {
			_, _ = r.history.Insert(eventJSON, `{"status":"error"}`)
		}
		r.run.stats.Errors++
		r.run.faulted = true
		r.run.lastError = err
		r.mu.Unlock()
		// A throwing quirk (e.g. event.body cast, non-finite serialize) faults the
		// event but still carries its diagnostics on the error - surface them too.
		if r.onDiagnostic != nil {
			var pe gafferruntime.ProjectionError
			if errors.As(err, &pe) {
				for _, d := range pe.ErrorDiagnostics() {
					r.onDiagnostic(d.Code)
				}
			}
		}
		if r.writer != nil {
			r.writer.OnError(eventID(eventJSON), fe.Code, fe.Description)
		}
		return true
	}

	// Defensive against a binding contract violation: Feed must return
	// (non-nil result, nil err) on success and (nil, err) on failure.
	// The current C# runtime always builds a FeedResult so this branch
	// is unreachable in practice. Kept so a future binding rewrite
	// can't silently nil-deref through the result fields below.
	if result == nil {
		result = &gafferruntime.FeedResult{Status: "skipped", SkipReason: "no-handler"}
	}

	if r.history != nil {
		if resultJSON, err := json.Marshal(result); err == nil {
			_, _ = r.history.Insert(eventJSON, string(resultJSON))
		}
	}

	if result.Status == "skipped" {
		r.run.stats.Skipped++
		if r.run.stats.SkippedByReason == nil {
			r.run.stats.SkippedByReason = make(map[string]int)
		}
		reason := result.SkipReason
		if reason == "" {
			reason = "unknown"
		}
		r.run.stats.SkippedByReason[reason]++
	} else {
		r.run.stats.Handled++
		if result.Partition != "" {
			r.run.partitions[result.Partition] = true
		}
	}
	r.mu.Unlock()

	if r.onDiagnostic != nil {
		for _, d := range result.Diagnostics {
			r.onDiagnostic(d.Code)
		}
	}
	if r.writer != nil {
		r.writer.OnResult(eventID(eventJSON), result)
	}
	return false
}

// Run-state accessors. These read back what processing has produced; the run
// state is driven internally by ProcessOne, with the exception of status (set
// by the front-ends from the source lifecycle) and the fault clear on a
// no-op interrupt.

func (r *Runner) Stats() EventStats {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.run.stats
	// Snapshot the map so the caller can iterate without racing against
	// further ProcessOne increments. Clone returns nil for a nil/empty
	// source, preserving the nil-vs-empty distinction callers may check.
	s.SkippedByReason = maps.Clone(r.run.stats.SkippedByReason)
	return s
}

func (r *Runner) Partitions() map[string]bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return maps.Clone(r.run.partitions)
}

func (r *Runner) Faulted() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.run.faulted
}

// ClearFault clears the recorded fault flag. Used on a no-op interrupt (a
// cancelled run that never caught up) so the teardown doesn't report a failure
// the user chose. The fault is only ever set internally by ProcessOne.
func (r *Runner) ClearFault() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.run.faulted = false
}

func (r *Runner) LastError() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.run.lastError
}

func (r *Runner) Step() int64 {
	return r.step.Load()
}

// Status returns the externally-driven run status (see runState.status).
func (r *Runner) Status() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.run.status
}

// SetStatus records the run status. The Runner doesn't observe the source
// lifecycle, so the front-ends drive this from their run loop.
func (r *Runner) SetStatus(s string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.run.status = s
}

// Debug-control accessors.

func (r *Runner) Paused() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.control.paused
}

func (r *Runner) PausedEvent() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.control.pausedEvent
}

// SetBreakAtStep arms a one-shot pause at the given 1-based step. The break is
// handled internally by ProcessOne and the OnBreak wiring.
func (r *Runner) SetBreakAtStep(step int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.control.breakAtStep = step
}

func (r *Runner) Info() gafferruntime.ProjectionInfo {
	return r.info
}

func (r *Runner) EngineVersion() int {
	return r.engineVersion
}

func eventID(eventJSON string) string {
	return ParseEvent(eventJSON).ID()
}
