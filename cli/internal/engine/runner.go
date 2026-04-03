package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

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
	Writer  EventWriter
	History *history.Store
	Debug   *DebugConfig
}

type DebugConfig struct {
	Session *gafferruntime.Session
	Info    gafferruntime.QuerySources
	OnBreak func(gafferruntime.BreakInfo) // must not call Runner methods
}

type Breakpoint struct {
	Line      int
	Column    int
	Condition string
}

type Runner struct {
	mu      sync.Mutex
	feed    FeedFn
	writer  EventWriter
	history *history.Store
	debug   *DebugConfig

	stats           EventStats
	partitions      map[string]bool
	faulted         bool
	lastError       error
	position        int64
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
	r.position++
	if r.debug != nil {
		r.pausedEvent = eventJSON
		if r.breakAtPosition > 0 && r.position == r.breakAtPosition {
			r.debug.Session.Pause()
		}
	}
	if r.writer != nil {
		r.writer.OnEvent(eventJSON)
	}
	r.mu.Unlock()

	result, err := r.feed(eventJSON)

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.debug != nil {
		r.pausedEvent = ""
	}

	if err != nil {
		fe := ClassifyError(err)
		if r.writer != nil {
			r.writer.OnError(eventID(eventJSON), fe.Code, fe.Description)
		}
		if r.history != nil {
			_, _ = r.history.Insert(eventJSON, `{"status":"error"}`)
		}
		r.stats.Errors++
		r.faulted = true
		r.lastError = err
		return true
	}

	if result == nil {
		result = &gafferruntime.FeedResult{Status: "skipped", SkipReason: "no-handler"}
	}

	if r.history != nil {
		resultJSON, _ := json.Marshal(result)
		_, _ = r.history.Insert(eventJSON, string(resultJSON))
	}

	if r.writer != nil {
		r.writer.OnResult(eventID(eventJSON), result)
	}
	if result.Status == "skipped" {
		r.stats.Skipped++
	} else {
		r.stats.Handled++
		if result.Partition != "" {
			r.partitions[result.Partition] = true
		}
	}
	return false
}

// Read accessors - safe for concurrent use

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
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.position
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

// SetBreakpoints clears existing breakpoints and sets new ones.
func (r *Runner) SetBreakpoints(breakpoints []Breakpoint) ([]*gafferruntime.SnappedBreakpoint, error) {
	if r.debug == nil {
		return nil, fmt.Errorf("debug not enabled")
	}
	r.debug.Session.ClearBreakpoints()
	snapped := make([]*gafferruntime.SnappedBreakpoint, len(breakpoints))
	for i, bp := range breakpoints {
		var opts *gafferruntime.BreakpointOptions
		if bp.Condition != "" {
			opts = &gafferruntime.BreakpointOptions{Condition: bp.Condition}
		}
		s, err := r.debug.Session.SetBreakpoint(bp.Line, bp.Column, opts)
		if err != nil {
			return nil, fmt.Errorf("setting breakpoint at line %d: %w", bp.Line, err)
		}
		snapped[i] = s
	}
	return snapped, nil
}

func (r *Runner) ClearBreakpoints() {
	if r.debug != nil {
		r.debug.Session.ClearBreakpoints()
	}
}

func (r *Runner) Continue() {
	if r.debug == nil {
		return
	}
	r.mu.Lock()
	r.paused = false
	r.mu.Unlock()
	r.debug.Session.Continue()
}

func (r *Runner) StepOver() {
	if r.debug == nil {
		return
	}
	r.mu.Lock()
	r.paused = false
	r.mu.Unlock()
	r.debug.Session.StepOver()
}

func (r *Runner) StepInto() {
	if r.debug == nil {
		return
	}
	r.mu.Lock()
	r.paused = false
	r.mu.Unlock()
	r.debug.Session.StepInto()
}

func (r *Runner) StepOut() {
	if r.debug == nil {
		return
	}
	r.mu.Lock()
	r.paused = false
	r.mu.Unlock()
	r.debug.Session.StepOut()
}

func (r *Runner) Destroy() {
	if r.debug == nil {
		return
	}
	r.debug.Session.ClearBreakpoints()
	if r.Paused() {
		r.debug.Session.Continue()
	}
}

func eventID(eventJSON string) string {
	var e struct {
		SequenceNumber int64  `json:"sequenceNumber"`
		StreamID       string `json:"streamId"`
	}
	_ = json.Unmarshal([]byte(eventJSON), &e)
	return formatEventID(e.SequenceNumber, e.StreamID)
}

func formatEventID(seq int64, streamID string) string {
	buf := make([]byte, 0, 20+len(streamID))
	buf = appendInt(buf, seq)
	buf = append(buf, '@')
	buf = append(buf, streamID...)
	return string(buf)
}

func appendInt(buf []byte, n int64) []byte {
	if n < 0 {
		buf = append(buf, '-')
		n = -n
	}
	if n == 0 {
		return append(buf, '0')
	}
	var digits [20]byte
	i := len(digits)
	for n > 0 {
		i--
		digits[i] = byte('0' + n%10)
		n /= 10
	}
	return append(buf, digits[i:]...)
}
