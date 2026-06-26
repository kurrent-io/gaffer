package engine

import (
	"context"
	"errors"
	"fmt"
	"testing"

	gafferruntime "github.com/kurrent-io/gaffer/bindings/go"
	"github.com/kurrent-io/gaffer/cli/internal/history"
	"github.com/kurrent-io/gaffer/cli/internal/testutil"
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

	stop := r.ProcessOne(testutil.Event("ItemAdded", "s-1", 0))

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

	stop := r.ProcessOne(testutil.Event("Unknown", "s-1", 0))

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
		Feed:    func(string) (*gafferruntime.FeedResult, error) { return nil, errors.New("boom") },
		Writer:  w,
		History: nil,
	})

	stop := r.ProcessOne(testutil.Event("Bad", "s-1", 0))

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

func TestRunner_ProcessOne_WrappedError_SurfacesDiagnostics(t *testing.T) {
	inner := &mockProjectionError{
		code:        "handler-error",
		description: "boom",
		diagnostics: []gafferruntime.Diagnostic{{Code: "quirk-a"}, {Code: "quirk-b"}},
	}
	var seen []string
	r := NewRunner(RunnerConfig{
		Feed:         func(string) (*gafferruntime.FeedResult, error) { return nil, fmt.Errorf("feeding event: %w", inner) },
		OnDiagnostic: func(code string) { seen = append(seen, code) },
	})

	stop := r.ProcessOne(testutil.Event("Bad", "s-1", 0))

	if !stop {
		t.Error("expected stop=true")
	}
	if len(seen) != 2 || seen[0] != "quirk-a" || seen[1] != "quirk-b" {
		t.Errorf("diagnostics: got %v, want [quirk-a quirk-b]", seen)
	}
}

func TestRunner_ProcessOne_NilWriter(t *testing.T) {
	r := NewRunner(RunnerConfig{
		Feed:    func(string) (*gafferruntime.FeedResult, error) { return processedResult(""), nil },
		Writer:  nil,
		History: nil,
	})

	stop := r.ProcessOne(testutil.Event("A", "s-1", 0))

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

	stop := r.ProcessOne(testutil.Event("A", "s-1", 0))

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

	r.ProcessOne(testutil.Event("ItemAdded", "s-1", 0))

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
		Feed:    func(string) (*gafferruntime.FeedResult, error) { return nil, errors.New("boom") },
		Writer:  nil,
		History: store,
	})

	r.ProcessOne(testutil.Event("Bad", "s-1", 0))

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

// --- Integration tests: real runtime session + runner + fixture source ---

func TestRunner_Integration_Handled(t *testing.T) {
	session := newTestSession(t, `fromAll().when({
		$init() { return { count: 0 }; },
		ItemAdded(s, e) { s.count++; return s; }
	})`)

	r := NewRunner(RunnerConfig{Feed: session.Feed})
	source := NewFixtureSource([]string{
		testutil.Event("ItemAdded", "s-1", 0),
		testutil.Event("ItemAdded", "s-1", 1),
	})
	_ = source.Run(context.Background(), r.ProcessOne)

	if r.Faulted() {
		t.Fatal("expected no fault")
	}
	if r.Stats().Handled != 2 {
		t.Errorf("handled: got %d, want 2", r.Stats().Handled)
	}
}

func TestRunner_Integration_Skipped(t *testing.T) {
	session := newTestSession(t, `fromAll().when({
		$init() { return { count: 0 }; },
		ItemAdded(s, e) { s.count++; return s; }
	})`)

	r := NewRunner(RunnerConfig{Feed: session.Feed})
	source := NewFixtureSource([]string{
		testutil.Event("ItemAdded", "s-1", 0),
		testutil.Event("Unknown", "s-1", 1),
	})
	_ = source.Run(context.Background(), r.ProcessOne)

	if r.Stats().Handled != 1 {
		t.Errorf("handled: got %d, want 1", r.Stats().Handled)
	}
	if r.Stats().Skipped != 1 {
		t.Errorf("skipped: got %d, want 1", r.Stats().Skipped)
	}
}

func TestRunner_Integration_Partitioned(t *testing.T) {
	session := newTestSession(t, `fromAll().foreachStream().when({
		$init() { return { count: 0 }; },
		ItemAdded(s, e) { s.count++; return s; }
	})`)

	r := NewRunner(RunnerConfig{Feed: session.Feed})
	source := NewFixtureSource([]string{
		testutil.Event("ItemAdded", "s-1", 0),
		testutil.Event("ItemAdded", "s-2", 1),
		testutil.Event("ItemAdded", "s-1", 2),
	})
	_ = source.Run(context.Background(), r.ProcessOne)

	if r.Stats().Handled != 3 {
		t.Errorf("handled: got %d, want 3", r.Stats().Handled)
	}
	if len(r.Partitions()) != 2 {
		t.Errorf("partitions: got %d, want 2", len(r.Partitions()))
	}
}

func TestRunner_Integration_Faulted(t *testing.T) {
	session := newTestSession(t, `fromAll().when({
		BadEvent(s, e) { throw new Error("boom"); }
	})`)

	r := NewRunner(RunnerConfig{Feed: session.Feed})
	source := NewFixtureSource([]string{
		testutil.Event("BadEvent", "s-1", 0),
		testutil.Event("BadEvent", "s-1", 1),
	})
	_ = source.Run(context.Background(), r.ProcessOne)

	if !r.Faulted() {
		t.Fatal("expected fault")
	}
	if r.Stats().Errors != 1 {
		t.Errorf("errors: got %d, want 1", r.Stats().Errors)
	}
	if r.Stats().Handled != 0 {
		t.Errorf("handled: got %d, want 0", r.Stats().Handled)
	}
}

func TestRunner_Integration_FaultedMidStream(t *testing.T) {
	session := newTestSession(t, `fromAll().when({
		$init() { return { count: 0 }; },
		ItemAdded(s, e) { s.count++; return s; },
		BadEvent(s, e) { throw new Error("boom"); }
	})`)

	r := NewRunner(RunnerConfig{Feed: session.Feed})
	source := NewFixtureSource([]string{
		testutil.Event("ItemAdded", "s-1", 0),
		testutil.Event("ItemAdded", "s-1", 1),
		testutil.Event("BadEvent", "s-1", 2),
		testutil.Event("ItemAdded", "s-1", 3),
	})
	_ = source.Run(context.Background(), r.ProcessOne)

	if !r.Faulted() {
		t.Fatal("expected fault")
	}
	if r.Stats().Handled != 2 {
		t.Errorf("handled: got %d, want 2", r.Stats().Handled)
	}
	if r.Stats().Errors != 1 {
		t.Errorf("errors: got %d, want 1", r.Stats().Errors)
	}
}

// --- nil-guard tests: debug and history methods on a minimal runner ---

func TestRunner_DebugNilGuards(t *testing.T) {
	r := NewRunner(RunnerConfig{Feed: func(string) (*gafferruntime.FeedResult, error) { return nil, nil }})

	if _, err := r.SetBreakpoints(nil); err == nil {
		t.Error("SetBreakpoints: expected error")
	}
	if _, err := r.Evaluate("1+1"); err == nil {
		t.Error("Evaluate: expected error")
	}
	if _, err := r.GetCallStack(); err == nil {
		t.Error("GetCallStack: expected error")
	}
	if _, err := r.GetScopes(0); err == nil {
		t.Error("GetScopes: expected error")
	}
	if _, err := r.GetVariables(0); err == nil {
		t.Error("GetVariables: expected error")
	}

	// These should not panic, and return nil when debug is disabled
	for _, fn := range []func() error{r.ClearBreakpoints, r.Continue, r.StepOver, r.StepInto, r.StepOut} {
		if err := fn(); err != nil {
			t.Errorf("debug-disabled control method: unexpected error %v", err)
		}
	}
	r.Drain() // no-op when debug is disabled; must not panic
	r.Destroy()
}

func TestRunner_HistoryNilGuards(t *testing.T) {
	r := NewRunner(RunnerConfig{Feed: func(string) (*gafferruntime.FeedResult, error) { return nil, nil }})

	if _, err := r.GetStep(1); err == nil {
		t.Error("GetStep: expected error")
	}
	if _, err := r.Timeline(1, 5); err == nil {
		t.Error("Timeline: expected error")
	}
	if _, err := r.TimelineFiltered(1, 5, "p"); err == nil {
		t.Error("TimelineFiltered: expected error")
	}
	if _, _, err := r.HistoryRange(); err == nil {
		t.Error("HistoryRange: expected error")
	}
	if _, err := r.HistoryCount(); err == nil {
		t.Error("HistoryCount: expected error")
	}
}

func TestEventStats_Total(t *testing.T) {
	stats := EventStats{Handled: 10, Skipped: 3, Errors: 1}
	if stats.Total() != 14 {
		t.Errorf("total() = %d, want 14", stats.Total())
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

	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
	if called {
		t.Error("process should not be called when context is already cancelled")
	}
}
