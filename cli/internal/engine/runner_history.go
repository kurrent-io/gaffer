package engine

import (
	"fmt"

	"github.com/kurrent-io/gaffer/cli/internal/history"
)

// History accessors

func (r *Runner) GetStep(position int64) (*history.Step, error) {
	if r.history == nil {
		return nil, fmt.Errorf("no history store")
	}
	return r.history.Get(position)
}

func (r *Runner) Timeline(from, to int64) ([]history.TimelineEntry, error) {
	if r.history == nil {
		return nil, fmt.Errorf("no history store")
	}
	return r.history.Timeline(from, to)
}

func (r *Runner) TimelineFiltered(from, to int64, partition string) ([]history.TimelineEntry, error) {
	if r.history == nil {
		return nil, fmt.Errorf("no history store")
	}
	return r.history.TimelineFiltered(from, to, partition)
}

func (r *Runner) HistoryRange() (min, max int64, err error) {
	if r.history == nil {
		return 0, 0, fmt.Errorf("no history store")
	}
	return r.history.Range()
}

func (r *Runner) HistoryCount() (int64, error) {
	if r.history == nil {
		return 0, fmt.Errorf("no history store")
	}
	return r.history.Count()
}
