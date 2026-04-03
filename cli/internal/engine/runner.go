package engine

import (
	"context"
	"encoding/json"
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
	Feed    FeedFn
	Session *gafferruntime.Session
	Info    gafferruntime.QuerySources
	Engine  string
	Writer  EventWriter
	History *history.Store
	Debug   *DebugConfig
}

type Runner struct {
	mu      sync.Mutex
	feed    FeedFn
	session *gafferruntime.Session
	info    gafferruntime.QuerySources
	engine  string
	writer  EventWriter
	history *history.Store
	debug   *DebugConfig

	stats           EventStats
	partitions      map[string]bool
	faulted         bool
	lastError       error
	position        atomic.Int64
	status          string
	paused          bool
	pausedEvent     string
	breakAtPosition int64
}

type EventStats struct {
	Handled int
	Skipped int
	Errors  int
}

func (s EventStats) Total() int {
	return s.Handled + s.Skipped + s.Errors
}

func NewRunner(cfg RunnerConfig) *Runner {
	r := &Runner{
		feed:       cfg.Feed,
		session:    cfg.Session,
		info:       cfg.Info,
		engine:     cfg.Engine,
		writer:     cfg.Writer,
		history:    cfg.History,
		debug:      cfg.Debug,
		partitions: make(map[string]bool),
	}
	if r.debug != nil && r.debug.OnBreak != nil {
		r.debug.Session.OnBreak(func(info gafferruntime.BreakInfo) {
			r.mu.Lock()
			breakAt := r.breakAtPosition
			r.mu.Unlock()
			if info.Reason == "pause" && breakAt > 0 {
				go r.debug.Session.StepInto()
				return
			}
			r.mu.Lock()
			r.paused = true
			r.mu.Unlock()
			r.debug.OnBreak(info)
		})
	}
	return r
}

func (r *Runner) ProcessOne(eventJSON string) (stop bool) {
	r.mu.Lock()
	pos := r.position.Add(1)
	if r.debug != nil {
		r.pausedEvent = eventJSON
		if r.breakAtPosition > 0 && pos == r.breakAtPosition {
			r.debug.Session.Pause()
		}
	}
	r.mu.Unlock()

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
		if r.writer != nil {
			r.writer.OnError(eventID(eventJSON), fe.Code, fe.Description)
		}
		return true
	}

	if result == nil {
		result = &gafferruntime.FeedResult{Status: "skipped", SkipReason: "no-handler"}
	}

	if r.history != nil {
		resultJSON, _ := json.Marshal(result)
		_, _ = r.history.Insert(eventJSON, string(resultJSON))
	}

	if result.Status == "skipped" {
		r.stats.Skipped++
	} else {
		r.stats.Handled++
		if result.Partition != "" {
			r.partitions[result.Partition] = true
		}
	}
	r.mu.Unlock()

	if r.writer != nil {
		r.writer.OnResult(eventID(eventJSON), result)
	}
	return false
}

// Read accessors

func (r *Runner) Stats() EventStats {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.stats
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

func (r *Runner) Position() int64 {
	return r.position.Load()
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

func (r *Runner) SetBreakAtPosition(pos int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.breakAtPosition = pos
}

func (r *Runner) Info() gafferruntime.QuerySources {
	return r.info
}

func (r *Runner) Engine() string {
	return r.engine
}

func eventID(eventJSON string) string {
	return ParseEvent(eventJSON).ID()
}
