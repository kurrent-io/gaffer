package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	gafferruntime "github.com/kurrent-io/gaffer/bindings/go"
	"github.com/kurrent-io/gaffer/cli/internal/engine"
)

func newTestSession(t *testing.T, source string) *gafferruntime.Session {
	t.Helper()
	session, err := gafferruntime.NewSession(source, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(session.Destroy)
	return session
}

func testEvent(eventType, streamID string, seq int) string {
	return fmt.Sprintf(
		`{"eventType":%q,"streamId":%q,"sequenceNumber":%d,"data":"{}","isJson":true,"eventId":"00000000-0000-0000-0000-%012d","created":"2026-01-01T00:00:00Z"}`,
		eventType, streamID, seq, seq,
	)
}

// recordingWriter captures calls to the outputWriter interface for assertions.
type recordingWriter struct {
	events  []eventInfo
	results []recordedResult
	errors  []recordedError
	summary *recordedSummary
}

type recordedResult struct {
	eventID string
	result  *gafferruntime.FeedResult
}

type recordedError struct {
	eventID     string
	code        string
	description string
}

type recordedSummary struct {
	stats engine.EventStats
	state engine.StateSummary
}

func (w *recordingWriter) WriteInfo(string, gafferruntime.QuerySources, string) {}
func (w *recordingWriter) WriteDebugListening(string, int)                   {}
func (w *recordingWriter) WriteEvent(event eventInfo)                        { w.events = append(w.events, event) }
func (w *recordingWriter) WriteResult(eventID string, r *gafferruntime.FeedResult) {
	w.results = append(w.results, recordedResult{eventID, r})
}
func (w *recordingWriter) WriteError(eventID, code, desc string) {
	w.errors = append(w.errors, recordedError{eventID, code, desc})
}
func (w *recordingWriter) WriteSummary(stats engine.EventStats, state engine.StateSummary) {
	w.summary = &recordedSummary{stats, state}
}

// --- runner + fixtureSource ---

func TestProcessEvents_HandledEvents(t *testing.T) {
	js := `fromAll().when({
		$init: function() { return { count: 0 }; },
		ItemAdded: function(s, e) { s.count++; return s; }
	})`
	session := newTestSession(t, js)
	writer := &recordingWriter{}

	events := []string{
		testEvent("ItemAdded", "s-1", 0),
		testEvent("ItemAdded", "s-1", 1),
	}

	r := engine.NewRunner(engine.RunnerConfig{Feed: session.Feed, Writer: &eventWriterAdapter{writer: writer}, History: nil})
	source := engine.NewFixtureSource(events)
	_ = source.Run(context.Background(), r.ProcessOne)
	stats, partitions, faulted := r.Stats, r.Partitions, r.Faulted

	if faulted {
		t.Fatal("expected no fault")
	}
	if stats.Handled != 2 {
		t.Errorf("handled: got %d, want 2", stats.Handled)
	}
	if stats.Skipped != 0 {
		t.Errorf("skipped: got %d, want 0", stats.Skipped)
	}
	if stats.Errors != 0 {
		t.Errorf("errors: got %d, want 0", stats.Errors)
	}
	if len(partitions) != 0 {
		t.Errorf("partitions: got %d, want 0 (unpartitioned)", len(partitions))
	}
	if len(writer.events) != 2 {
		t.Errorf("writer.events: got %d, want 2", len(writer.events))
	}
	if len(writer.results) != 2 {
		t.Errorf("writer.results: got %d, want 2", len(writer.results))
	}
}

func TestProcessEvents_SkippedEvents(t *testing.T) {
	js := `fromAll().when({
		$init: function() { return { count: 0 }; },
		ItemAdded: function(s, e) { s.count++; return s; }
	})`
	session := newTestSession(t, js)
	writer := &recordingWriter{}

	events := []string{
		testEvent("ItemAdded", "s-1", 0),
		testEvent("Unknown", "s-1", 1),
	}

	r := engine.NewRunner(engine.RunnerConfig{Feed: session.Feed, Writer: &eventWriterAdapter{writer: writer}, History: nil})
	source := engine.NewFixtureSource(events)
	_ = source.Run(context.Background(), r.ProcessOne)
	stats, faulted := r.Stats, r.Faulted

	if faulted {
		t.Fatal("expected no fault")
	}
	if stats.Handled != 1 {
		t.Errorf("handled: got %d, want 1", stats.Handled)
	}
	if stats.Skipped != 1 {
		t.Errorf("skipped: got %d, want 1", stats.Skipped)
	}
	if len(writer.events) != 2 {
		t.Errorf("writer.events: got %d, want 2", len(writer.events))
	}
	if len(writer.results) != 2 {
		t.Errorf("writer.results: got %d, want 2 (one processed, one skipped)", len(writer.results))
	}
}

func TestProcessEvents_Partitioned(t *testing.T) {
	js := `fromAll().foreachStream().when({
		$init: function() { return { count: 0 }; },
		ItemAdded: function(s, e) { s.count++; return s; }
	})`
	session := newTestSession(t, js)
	writer := &recordingWriter{}

	events := []string{
		testEvent("ItemAdded", "s-1", 0),
		testEvent("ItemAdded", "s-2", 1),
		testEvent("ItemAdded", "s-1", 2),
	}

	r := engine.NewRunner(engine.RunnerConfig{Feed: session.Feed, Writer: &eventWriterAdapter{writer: writer}, History: nil})
	source := engine.NewFixtureSource(events)
	_ = source.Run(context.Background(), r.ProcessOne)
	stats, partitions, faulted := r.Stats, r.Partitions, r.Faulted

	if faulted {
		t.Fatal("expected no fault")
	}
	if stats.Handled != 3 {
		t.Errorf("handled: got %d, want 3", stats.Handled)
	}
	if len(partitions) != 2 {
		t.Errorf("partitions: got %d, want 2", len(partitions))
	}
	if !partitions["s-1"] || !partitions["s-2"] {
		t.Errorf("expected partitions s-1 and s-2, got %v", partitions)
	}
}

func TestProcessEvents_Faulted(t *testing.T) {
	js := `fromAll().when({
		$init: function() { return { count: 0 }; },
		BadEvent: function(s, e) { throw new Error("boom"); }
	})`
	session := newTestSession(t, js)
	writer := &recordingWriter{}

	events := []string{
		testEvent("BadEvent", "s-1", 0),
		testEvent("BadEvent", "s-1", 1),
	}

	r := engine.NewRunner(engine.RunnerConfig{Feed: session.Feed, Writer: &eventWriterAdapter{writer: writer}, History: nil})
	source := engine.NewFixtureSource(events)
	_ = source.Run(context.Background(), r.ProcessOne)
	stats, faulted := r.Stats, r.Faulted

	if !faulted {
		t.Fatal("expected fault")
	}
	if stats.Errors != 1 {
		t.Errorf("errors: got %d, want 1", stats.Errors)
	}
	if stats.Handled != 0 {
		t.Errorf("handled: got %d, want 0 (should stop on first error)", stats.Handled)
	}
	if len(writer.events) != 1 {
		t.Errorf("writer.events: got %d, want 1 (should stop after first event)", len(writer.events))
	}
	if len(writer.errors) != 1 {
		t.Fatalf("writer.errors: got %d, want 1", len(writer.errors))
	}
	if writer.errors[0].code == "" {
		t.Error("expected non-empty error code")
	}
	if writer.errors[0].description == "" {
		t.Error("expected non-empty error description")
	}
}

func TestProcessEvents_FaultedMidStream(t *testing.T) {
	js := `fromAll().when({
		$init: function() { return { count: 0 }; },
		ItemAdded: function(s, e) { s.count++; return s; },
		BadEvent: function(s, e) { throw new Error("boom"); }
	})`
	session := newTestSession(t, js)
	writer := &recordingWriter{}

	events := []string{
		testEvent("ItemAdded", "s-1", 0),
		testEvent("ItemAdded", "s-1", 1),
		testEvent("BadEvent", "s-1", 2),
		testEvent("ItemAdded", "s-1", 3),
	}

	r := engine.NewRunner(engine.RunnerConfig{Feed: session.Feed, Writer: &eventWriterAdapter{writer: writer}, History: nil})
	source := engine.NewFixtureSource(events)
	_ = source.Run(context.Background(), r.ProcessOne)
	stats, faulted := r.Stats, r.Faulted

	if !faulted {
		t.Fatal("expected fault")
	}
	if stats.Handled != 2 {
		t.Errorf("handled: got %d, want 2 (events before fault)", stats.Handled)
	}
	if stats.Errors != 1 {
		t.Errorf("errors: got %d, want 1", stats.Errors)
	}
	if len(writer.events) != 3 {
		t.Errorf("writer.events: got %d, want 3 (should stop after faulting event)", len(writer.events))
	}
	if len(writer.results) != 2 {
		t.Errorf("writer.results: got %d, want 2 (only successful events)", len(writer.results))
	}
}

func TestProcessEvents_Empty(t *testing.T) {
	js := `fromAll().when({
		$init: function() { return { count: 0 }; },
		ItemAdded: function(s, e) { s.count++; return s; }
	})`
	session := newTestSession(t, js)
	writer := &recordingWriter{}

	r := engine.NewRunner(engine.RunnerConfig{Feed: session.Feed, Writer: &eventWriterAdapter{writer: writer}, History: nil})
	source := engine.NewFixtureSource([]string{})
	_ = source.Run(context.Background(), r.ProcessOne)
	stats, partitions, faulted := r.Stats, r.Partitions, r.Faulted

	if faulted {
		t.Error("expected no fault")
	}
	if stats.Handled != 0 || stats.Skipped != 0 || stats.Errors != 0 {
		t.Errorf("expected zero stats, got %+v", stats)
	}
	if len(partitions) != 0 {
		t.Errorf("expected no partitions, got %v", partitions)
	}
}

// --- CollectState ---

func TestBuildSummary_Unpartitioned(t *testing.T) {
	js := `fromAll().when({
		$init: function() { return { count: 0 }; },
		ItemAdded: function(s, e) { s.count++; return s; }
	})`
	session := newTestSession(t, js)
	info := session.GetSources()

	if _, err := session.Feed(testEvent("ItemAdded", "s-1", 0)); err != nil {
		t.Fatal(err)
	}

	summary := engine.CollectState(session, info, nil)

	if summary.Partitioned {
		t.Error("expected unpartitioned")
	}
	if !hasContent(summary.State) {
		t.Fatal("expected state")
	}

	var state map[string]any
	if err := json.Unmarshal(summary.State, &state); err != nil {
		t.Fatal(err)
	}
	if state["count"] != float64(1) {
		t.Errorf("state.count: got %v, want 1", state["count"])
	}
}

func TestBuildSummary_Partitioned(t *testing.T) {
	js := `fromAll().foreachStream().when({
		$init: function() { return { count: 0 }; },
		ItemAdded: function(s, e) { s.count++; return s; }
	})`
	session := newTestSession(t, js)
	info := session.GetSources()

	for i, stream := range []string{"s-1", "s-2", "s-1"} {
		if _, err := session.Feed(testEvent("ItemAdded", stream, i)); err != nil {
			t.Fatal(err)
		}
	}

	partitions := map[string]bool{"s-1": true, "s-2": true}
	summary := engine.CollectState(session, info, partitions)

	if !summary.Partitioned {
		t.Error("expected partitioned")
	}
	if len(summary.Partitions) != 2 {
		t.Fatalf("partitions: got %d, want 2", len(summary.Partitions))
	}

	for key, wantCount := range map[string]float64{"s-1": 2, "s-2": 1} {
		data, ok := summary.Partitions[key]
		if !ok {
			t.Errorf("missing partition %s", key)
			continue
		}
		if !hasContent(data.State) {
			t.Errorf("partition %s: expected state", key)
			continue
		}
		var state map[string]any
		if err := json.Unmarshal(data.State, &state); err != nil {
			t.Errorf("partition %s: %v", key, err)
			continue
		}
		if state["count"] != wantCount {
			t.Errorf("partition %s count: got %v, want %v", key, state["count"], wantCount)
		}
	}
}

func TestBuildSummary_WithTransforms(t *testing.T) {
	js := `fromAll().when({
		$init: function() { return { count: 0 }; },
		ItemAdded: function(s, e) { s.count++; return s; }
	}).transformBy(function(s) { return { doubled: s.count * 2 }; })`
	session := newTestSession(t, js)
	info := session.GetSources()

	if _, err := session.Feed(testEvent("ItemAdded", "s-1", 0)); err != nil {
		t.Fatal(err)
	}

	summary := engine.CollectState(session, info, nil)

	if !summary.HasTransforms {
		t.Fatal("expected hasTransforms")
	}

	if !hasContent(summary.State) {
		t.Fatal("expected state alongside result")
	}
	var state map[string]any
	if err := json.Unmarshal(summary.State, &state); err != nil {
		t.Fatal(err)
	}
	if state["count"] != float64(1) {
		t.Errorf("state.count: got %v, want 1", state["count"])
	}

	if !hasContent(summary.Result) {
		t.Fatal("expected result from transform")
	}
	var result map[string]any
	if err := json.Unmarshal(summary.Result, &result); err != nil {
		t.Fatal(err)
	}
	if result["doubled"] != float64(2) {
		t.Errorf("result.doubled: got %v, want 2", result["doubled"])
	}
}

func TestBuildSummary_PartitionedWithTransforms(t *testing.T) {
	js := `fromAll().foreachStream().when({
		$init: function() { return { count: 0 }; },
		ItemAdded: function(s, e) { s.count++; return s; }
	}).transformBy(function(s) { return { doubled: s.count * 2 }; })`
	session := newTestSession(t, js)
	info := session.GetSources()

	if _, err := session.Feed(testEvent("ItemAdded", "s-1", 0)); err != nil {
		t.Fatal(err)
	}

	partitions := map[string]bool{"s-1": true}
	summary := engine.CollectState(session, info, partitions)

	if !summary.Partitioned {
		t.Error("expected partitioned")
	}
	if !summary.HasTransforms {
		t.Error("expected hasTransforms")
	}

	data, ok := summary.Partitions["s-1"]
	if !ok {
		t.Fatal("missing partition s-1")
	}
	if !hasContent(data.State) {
		t.Error("expected state for partition")
	}
	if !hasContent(data.Result) {
		t.Error("expected result for partition")
	}

	var result map[string]any
	if err := json.Unmarshal(data.Result, &result); err != nil {
		t.Fatal(err)
	}
	if result["doubled"] != float64(2) {
		t.Errorf("result.doubled: got %v, want 2", result["doubled"])
	}
}

func TestBuildSummary_BiState(t *testing.T) {
	js := `fromAll().foreachStream().when({
		$init: function() { return { count: 0 }; },
		$initShared: function() { return { total: 0 }; },
		ItemAdded: function(s, e) {
			s.count++;
			linkTo('totals', e);
			return s;
		},
		$any: function(s, e) {
			if (e.streamId === 'totals') { s.total++; return s; }
		}
	})`
	session := newTestSession(t, js)
	info := session.GetSources()

	if !info.IsBiState {
		t.Skip("runtime did not report IsBiState - projection source may need adjustment")
	}

	if _, err := session.Feed(testEvent("ItemAdded", "s-1", 0)); err != nil {
		t.Fatal(err)
	}

	partitions := map[string]bool{"s-1": true}
	summary := engine.CollectState(session, info, partitions)

	if !summary.HasBiState {
		t.Error("expected hasBiState")
	}
	if !hasContent(summary.SharedState) {
		t.Error("expected sharedState")
	}
}

// --- classifyError (integration - verifies real runtime errors classify correctly) ---

func TestClassifyError_RuntimeError(t *testing.T) {
	js := `fromAll().when({
		BadEvent: function(s, e) { throw new Error("boom"); }
	})`
	session := newTestSession(t, js)

	_, err := session.Feed(testEvent("BadEvent", "s-1", 0))
	if err == nil {
		t.Fatal("expected error")
	}

	fe := engine.ClassifyError(err)
	if fe.Code == "" {
		t.Error("expected non-empty code")
	}
	if fe.Description == "" {
		t.Error("expected non-empty description")
	}
	if fe.Code == "unexpected-error" {
		t.Errorf("expected a projection error code, got %q", fe.Code)
	}
}
