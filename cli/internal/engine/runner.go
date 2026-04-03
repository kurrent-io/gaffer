package engine

import (
	"context"
	"encoding/json"

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
}

type Runner struct {
	feed       FeedFn
	writer     EventWriter
	history    *history.Store
	Stats      EventStats
	Partitions map[string]bool
	Faulted    bool
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
	return &Runner{
		feed:       cfg.Feed,
		writer:     cfg.Writer,
		history:    cfg.History,
		Partitions: make(map[string]bool),
	}
}

func (r *Runner) ProcessOne(eventJSON string) (stop bool) {
	if r.writer != nil {
		r.writer.OnEvent(eventJSON)
	}

	result, err := r.feed(eventJSON)
	if err != nil {
		fe := ClassifyError(err)
		if r.writer != nil {
			r.writer.OnError(eventID(eventJSON), fe.Code, fe.Description)
		}
		if r.history != nil {
			_, _ = r.history.Insert(eventJSON, `{"status":"error"}`)
		}
		r.Stats.Errors++
		r.Faulted = true
		return true
	}
	if result == nil {
		return false
	}

	if r.history != nil {
		resultJSON, _ := json.Marshal(result)
		_, _ = r.history.Insert(eventJSON, string(resultJSON))
	}

	if r.writer != nil {
		r.writer.OnResult(eventID(eventJSON), result)
	}
	if result.Status == "skipped" {
		r.Stats.Skipped++
	} else {
		r.Stats.Handled++
		if result.Partition != "" {
			r.Partitions[result.Partition] = true
		}
	}
	return false
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
