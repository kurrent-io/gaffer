package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	gafferruntime "github.com/kurrent-io/gaffer/bindings/go"
)

func processedResult(partition string) *gafferruntime.FeedResult {
	return &gafferruntime.FeedResult{
		Status:    "processed",
		Partition: partition,
		State:     json.RawMessage(`{"count":1}`),
	}
}

func skippedResult() *gafferruntime.FeedResult {
	return &gafferruntime.FeedResult{
		Status:     "skipped",
		SkipReason: "unhandled",
	}
}

// --- runner.processOne ---

func TestRunner_ProcessOne_Handled(t *testing.T) {
	writer := &recordingWriter{}
	r := newRunner(func(evt string) (*gafferruntime.FeedResult, error) {
		return processedResult("p-1"), nil
	}, writer)

	stop := r.processOne(testEvent("ItemAdded", "s-1", 0))

	if stop {
		t.Error("expected stop=false")
	}
	if r.stats.handled != 1 {
		t.Errorf("handled: got %d, want 1", r.stats.handled)
	}
	if !r.partitions["p-1"] {
		t.Error("expected partition p-1 to be tracked")
	}
	if len(writer.events) != 1 {
		t.Errorf("writer.events: got %d, want 1", len(writer.events))
	}
	if len(writer.results) != 1 {
		t.Errorf("writer.results: got %d, want 1", len(writer.results))
	}
}

func TestRunner_ProcessOne_HandledNoPartition(t *testing.T) {
	r := newRunner(func(evt string) (*gafferruntime.FeedResult, error) {
		return &gafferruntime.FeedResult{Status: "processed"}, nil
	}, &recordingWriter{})

	r.processOne(testEvent("ItemAdded", "s-1", 0))

	if r.stats.handled != 1 {
		t.Errorf("handled: got %d, want 1", r.stats.handled)
	}
	if len(r.partitions) != 0 {
		t.Errorf("partitions: got %d, want 0", len(r.partitions))
	}
}

func TestRunner_ProcessOne_Skipped(t *testing.T) {
	writer := &recordingWriter{}
	r := newRunner(func(evt string) (*gafferruntime.FeedResult, error) {
		return skippedResult(), nil
	}, writer)

	stop := r.processOne(testEvent("Unknown", "s-1", 0))

	if stop {
		t.Error("expected stop=false")
	}
	if r.stats.skipped != 1 {
		t.Errorf("skipped: got %d, want 1", r.stats.skipped)
	}
	if r.stats.handled != 0 {
		t.Errorf("handled: got %d, want 0", r.stats.handled)
	}
	if len(writer.results) != 1 {
		t.Errorf("writer.results: got %d, want 1", len(writer.results))
	}
}

func TestRunner_ProcessOne_Error(t *testing.T) {
	writer := &recordingWriter{}
	r := newRunner(func(evt string) (*gafferruntime.FeedResult, error) {
		return nil, fmt.Errorf("boom")
	}, writer)

	stop := r.processOne(testEvent("BadEvent", "s-1", 0))

	if !stop {
		t.Error("expected stop=true")
	}
	if !r.faulted {
		t.Error("expected faulted")
	}
	if r.stats.errors != 1 {
		t.Errorf("errors: got %d, want 1", r.stats.errors)
	}
	if len(writer.errors) != 1 {
		t.Fatalf("writer.errors: got %d, want 1", len(writer.errors))
	}
	if writer.errors[0].code != "unexpected-error" {
		t.Errorf("error code: got %q, want %q", writer.errors[0].code, "unexpected-error")
	}
	if len(writer.results) != 0 {
		t.Error("expected no results written on error")
	}
}

func TestRunner_ProcessOne_ParsesEvent(t *testing.T) {
	writer := &recordingWriter{}
	r := newRunner(func(evt string) (*gafferruntime.FeedResult, error) {
		return processedResult(""), nil
	}, writer)

	r.processOne(testEvent("ItemAdded", "order-1", 42))

	if len(writer.events) != 1 {
		t.Fatalf("writer.events: got %d, want 1", len(writer.events))
	}
	if writer.events[0].EventType != "ItemAdded" {
		t.Errorf("eventType: got %q, want %q", writer.events[0].EventType, "ItemAdded")
	}
	if writer.events[0].StreamID != "order-1" {
		t.Errorf("streamId: got %q, want %q", writer.events[0].StreamID, "order-1")
	}
	if writer.events[0].SequenceNumber != 42 {
		t.Errorf("sequenceNumber: got %d, want 42", writer.events[0].SequenceNumber)
	}
}

func TestRunner_ProcessOne_MultipleEvents(t *testing.T) {
	call := 0
	r := newRunner(func(evt string) (*gafferruntime.FeedResult, error) {
		call++
		if call == 2 {
			return skippedResult(), nil
		}
		return processedResult(fmt.Sprintf("p-%d", call)), nil
	}, &recordingWriter{})

	r.processOne(testEvent("A", "s-1", 0))
	r.processOne(testEvent("B", "s-1", 1))
	r.processOne(testEvent("C", "s-1", 2))

	if r.stats.handled != 2 {
		t.Errorf("handled: got %d, want 2", r.stats.handled)
	}
	if r.stats.skipped != 1 {
		t.Errorf("skipped: got %d, want 1", r.stats.skipped)
	}
	if len(r.partitions) != 2 {
		t.Errorf("partitions: got %d, want 2", len(r.partitions))
	}
}

// --- fixtureSource ---

func TestFixtureSource_Run(t *testing.T) {
	var processed []string
	source := &fixtureSource{events: []string{"a", "b", "c"}}

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
	source := &fixtureSource{events: []string{"a", "b", "c"}}

	err := source.Run(context.Background(), func(evt string) bool {
		processed = append(processed, evt)
		return evt == "b"
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(processed) != 2 {
		t.Errorf("processed: got %d, want 2 (should stop after 'b')", len(processed))
	}
}

func TestFixtureSource_Run_Empty(t *testing.T) {
	source := &fixtureSource{events: []string{}}
	called := false

	err := source.Run(context.Background(), func(evt string) bool {
		called = true
		return false
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if called {
		t.Error("process should not be called for empty events")
	}
}

func TestFixtureSource_Run_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	source := &fixtureSource{events: []string{"a", "b", "c"}}
	called := false

	err := source.Run(ctx, func(evt string) bool {
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

func TestFixtureSource_Run_ContextCancelledMidStream(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var processed []string
	source := &fixtureSource{events: []string{"a", "b", "c"}}

	err := source.Run(ctx, func(evt string) bool {
		processed = append(processed, evt)
		if evt == "a" {
			cancel()
		}
		return false
	})

	if err != context.Canceled {
		t.Errorf("expected context.Canceled, got %v", err)
	}
	if len(processed) != 1 {
		t.Errorf("processed: got %d, want 1 (should stop before next event)", len(processed))
	}
}

func TestFixtureSource_Run_StopTakesPriorityOverCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	source := &fixtureSource{events: []string{"a", "b"}}

	err := source.Run(ctx, func(evt string) bool {
		cancel()
		return true
	})

	// process returned true (stop) on the same call that cancelled the context.
	// The break from stop exits the loop before the next iteration's ctx check,
	// so Run returns nil, not context.Canceled.
	if err != nil {
		t.Errorf("expected nil (stop takes priority over cancel), got %v", err)
	}
}

// --- classifyError ---

func TestClassifyError_GenericError_Unit(t *testing.T) {
	code, desc := classifyError(fmt.Errorf("something went wrong"))

	if code != "unexpected-error" {
		t.Errorf("code: got %q, want %q", code, "unexpected-error")
	}
	if desc != "something went wrong" {
		t.Errorf("description: got %q, want %q", desc, "something went wrong")
	}
}

// --- edge cases ---

func TestRunner_ProcessOne_NilResult(t *testing.T) {
	writer := &recordingWriter{}
	r := newRunner(func(evt string) (*gafferruntime.FeedResult, error) {
		return nil, nil
	}, writer)

	stop := r.processOne(testEvent("ItemAdded", "s-1", 0))

	if stop {
		t.Error("expected stop=false")
	}
	if r.stats.handled != 0 && r.stats.skipped != 0 {
		t.Error("expected no stats change for nil result")
	}
	if len(writer.results) != 0 {
		t.Error("expected no result written for nil result")
	}
}
