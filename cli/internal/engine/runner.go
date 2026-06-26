package engine

import (
	"context"
	"encoding/json"
	"errors"
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

type Runner struct {
	mu            sync.Mutex
	feed          FeedFn
	session       *gafferruntime.Session
	info          gafferruntime.ProjectionInfo
	engineVersion int
	writer        EventWriter
	history       *history.Store
	debug         *DebugConfig
	onDiagnostic  func(code string)

	stats       EventStats
	partitions  map[string]bool
	faulted     bool
	lastError   error
	step        atomic.Int64
	status      string
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
		partitions:    make(map[string]bool),
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
			if r.draining {
				r.paused = false
				r.mu.Unlock()
				go func() { _ = r.debug.Session.Continue() }()
				return
			}
			if info.Reason == "pause" && r.breakAtStep > 0 {
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
			r.paused = true
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
		r.pausedEvent = eventJSON
		if r.breakAtStep > 0 && step == r.breakAtStep {
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
		r.pausedEvent = ""
	}

	if err != nil {
		fe := ClassifyError(err)
		if r.history != nil {
			_, _ = r.history.Insert(eventJSON, `{"status":"error"}`)
		}
		r.stats.Errors++
		r.faulted = true
		r.lastError = err
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
		resultJSON, _ := json.Marshal(result)
		_, _ = r.history.Insert(eventJSON, string(resultJSON))
	}

	if result.Status == "skipped" {
		r.stats.Skipped++
		if r.stats.SkippedByReason == nil {
			r.stats.SkippedByReason = make(map[string]int)
		}
		reason := result.SkipReason
		if reason == "" {
			reason = "unknown"
		}
		r.stats.SkippedByReason[reason]++
	} else {
		r.stats.Handled++
		if result.Partition != "" {
			r.partitions[result.Partition] = true
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

// Read accessors

func (r *Runner) Stats() EventStats {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.stats
	// Snapshot the map so the caller can iterate without racing
	// against further ProcessOne increments.
	if len(r.stats.SkippedByReason) > 0 {
		cp := make(map[string]int, len(r.stats.SkippedByReason))
		for k, v := range r.stats.SkippedByReason {
			cp[k] = v
		}
		s.SkippedByReason = cp
	} else {
		s.SkippedByReason = nil
	}
	return s
}

func (r *Runner) Partitions() map[string]bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := make(map[string]bool, len(r.partitions))
	for k, v := range r.partitions {
		cp[k] = v
	}
	return cp
}

func (r *Runner) Faulted() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.faulted
}

func (r *Runner) LastError() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lastError
}

func (r *Runner) Step() int64 {
	return r.step.Load()
}

func (r *Runner) SetFaulted(v bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.faulted = v
}

func (r *Runner) Paused() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.paused
}

func (r *Runner) PausedEvent() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.pausedEvent
}

func (r *Runner) Status() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.status
}

func (r *Runner) SetStatus(s string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.status = s
}

func (r *Runner) SetBreakAtStep(step int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.breakAtStep = step
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
