package engine

import (
	"context"
	"fmt"
	"testing"

	gafferruntime "github.com/kurrent-io/gaffer/bindings/go"
	"github.com/kurrent-io/gaffer/cli/internal/history"
)

type recordingWriter struct {
	events  []string
	results []string
	errors  []string
}

func (w *recordingWriter) OnEvent(eventJSON string) {
	w.events = append(w.events, eventJSON)
}

func (w *recordingWriter) OnResult(eventID string, _ *gafferruntime.FeedResult) {
	w.results = append(w.results, eventID)
}

func (w *recordingWriter) OnError(eventID, code, desc string) {
	w.errors = append(w.errors, eventID+":"+code+":"+desc)
}

func processedResult(partition string) *gafferruntime.FeedResult {
	return &gafferruntime.FeedResult{Status: "processed", Partition: partition}
}

func skippedResult() *gafferruntime.FeedResult {
	return &gafferruntime.FeedResult{Status: "skipped", SkipReason: "unhandled"}
}

func TestRunner_ProcessOne_Handled(t *testing.T) {
	w := &recordingWriter{}
	r := NewRunner(RunnerConfig{
		Feed:    func(string) (*gafferruntime.FeedResult, error) { return processedResult("p-1"), nil },
		Writer:  w,
		History: nil,
	})

	stop := r.ProcessOne(testEvent("ItemAdded", "s-1", 0))

	if stop {
		t.Error("expected stop=false")
	}
	if r.Stats().Handled != 1 {
		t.Errorf("handled: got %d, want 1", r.Stats().Handled)
	}
	if !r.Partitions()["p-1"] {
		t.Error("expected partition p-1 tracked")
	}
	if len(w.events) != 1 {
		t.Errorf("writer events: got %d, want 1", len(w.events))
	}
	if len(w.results) != 1 {
		t.Errorf("writer results: got %d, want 1", len(w.results))
	}
}

func TestRunner_ProcessOne_Skipped(t *testing.T) {
	r := NewRunner(RunnerConfig{
		Feed:    func(string) (*gafferruntime.FeedResult, error) { return skippedResult(), nil },
		Writer:  nil,
		History: nil,
	})

	stop := r.ProcessOne(testEvent("Unknown", "s-1", 0))

	if stop {
		t.Error("expected stop=false")
	}
	if r.Stats().Skipped != 1 {
		t.Errorf("skipped: got %d, want 1", r.Stats().Skipped)
	}
}

func TestRunner_ProcessOne_Error(t *testing.T) {
	w := &recordingWriter{}
	r := NewRunner(RunnerConfig{
		Feed:    func(string) (*gafferruntime.FeedResult, error) { return nil, fmt.Errorf("boom") },
		Writer:  w,
		History: nil,
	})

	stop := r.ProcessOne(testEvent("Bad", "s-1", 0))

	if !stop {
		t.Error("expected stop=true")
	}
	if !r.Faulted() {
		t.Error("expected faulted")
	}
	if r.Stats().Errors != 1 {
		t.Errorf("errors: got %d, want 1", r.Stats().Errors)
	}
	if len(w.errors) != 1 {
		t.Errorf("writer errors: got %d, want 1", len(w.errors))
	}
	if len(w.results) != 0 {
		t.Error("expected no results on error")
	}
}

func TestRunner_ProcessOne_NilWriter(t *testing.T) {
	r := NewRunner(RunnerConfig{
		Feed:    func(string) (*gafferruntime.FeedResult, error) { return processedResult(""), nil },
		Writer:  nil,
		History: nil,
	})

	stop := r.ProcessOne(testEvent("A", "s-1", 0))

	if stop {
		t.Error("expected stop=false")
	}
	if r.Stats().Handled != 1 {
		t.Errorf("handled: got %d, want 1", r.Stats().Handled)
	}
}

func TestRunner_ProcessOne_NilResult(t *testing.T) {
	w := &recordingWriter{}
	r := NewRunner(RunnerConfig{
		Feed:    func(string) (*gafferruntime.FeedResult, error) { return nil, nil },
		Writer:  w,
		History: nil,
	})

	stop := r.ProcessOne(testEvent("A", "s-1", 0))

	if stop {
		t.Error("expected stop=false")
	}
	if r.Stats().Skipped != 1 {
		t.Errorf("skipped: got %d, want 1 (nil results recorded as skipped)", r.Stats().Skipped)
	}
	if len(w.results) != 1 {
		t.Errorf("writer results: got %d, want 1 (nil result notified as skipped)", len(w.results))
	}
}

func TestRunner_ProcessOne_RecordsHistory(t *testing.T) {
	store, err := history.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()

	r := NewRunner(RunnerConfig{
		Feed:    func(string) (*gafferruntime.FeedResult, error) { return processedResult("p-1"), nil },
		Writer:  nil,
		History: store,
	})

	r.ProcessOne(testEvent("ItemAdded", "s-1", 0))

	count, err := store.Count()
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("history count: got %d, want 1", count)
	}
}

func TestRunner_ProcessOne_RecordsErrorInHistory(t *testing.T) {
	store, err := history.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()

	r := NewRunner(RunnerConfig{
		Feed:    func(string) (*gafferruntime.FeedResult, error) { return nil, fmt.Errorf("boom") },
		Writer:  nil,
		History: store,
	})

	r.ProcessOne(testEvent("Bad", "s-1", 0))

	count, err := store.Count()
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("history count: got %d, want 1", count)
	}

	step, err := store.Get(1)
	if err != nil {
		t.Fatal(err)
	}
	if step == nil {
		t.Fatal("expected history entry")
	}
	if step.Status != "error" {
		t.Errorf("status: got %q, want %q", step.Status, "error")
	}
}

func TestEventID(t *testing.T) {
	id := eventID(`{"sequenceNumber":42,"streamId":"order-1"}`)
	if id != "42@order-1" {
		t.Errorf("got %q, want %q", id, "42@order-1")
	}
}

func TestFixtureSource_Run(t *testing.T) {
	var processed []string
	source := NewFixtureSource([]string{"a", "b", "c"})

	err := source.Run(context.Background(), func(evt string) bool {
		processed = append(processed, evt)
		return false
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(processed) != 3 {
		t.Errorf("processed: got %d, want 3", len(processed))
	}
}

func TestFixtureSource_Run_StopsOnTrue(t *testing.T) {
	var processed []string
	source := NewFixtureSource([]string{"a", "b", "c"})

	err := source.Run(context.Background(), func(evt string) bool {
		processed = append(processed, evt)
		return evt == "b"
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(processed) != 2 {
		t.Errorf("processed: got %d, want 2", len(processed))
	}
}

func TestFixtureSource_Run_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	source := NewFixtureSource([]string{"a", "b"})
	called := false

	err := source.Run(ctx, func(string) bool {
		called = true
		return false
	})

	if err != context.Canceled {
		t.Errorf("expected context.Canceled, got %v", err)
	}
	if called {
		t.Error("process should not be called when context is already cancelled")
	}
}
