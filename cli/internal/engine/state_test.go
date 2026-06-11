package engine

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	gafferruntime "github.com/kurrent-io/gaffer/bindings/go"
	"github.com/kurrent-io/gaffer/cli/internal/testutil"
)

func newTestSession(t *testing.T, source string) *gafferruntime.Session {
	t.Helper()
	return newTestSessionWithVersion(t, source, 2)
}

func newTestSessionWithVersion(t *testing.T, source string, engineVersion int) *gafferruntime.Session {
	t.Helper()
	opts := fmt.Sprintf(`{"engineVersion":%d}`, engineVersion)
	session, err := gafferruntime.NewSession(source, &opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(session.Destroy)
	return session
}

func mustCollectState(t *testing.T, session *gafferruntime.Session, info gafferruntime.ProjectionInfo, partitions map[string]bool) StateSummary {
	t.Helper()
	summary, err := CollectState(session, info, partitions)
	if err != nil {
		t.Fatalf("CollectState: %v", err)
	}
	return summary
}

func TestCollectState_Unpartitioned(t *testing.T) {
	session := newTestSession(t, `fromAll().when({
		$init() { return { count: 0 }; },
		ItemAdded(s, e) { s.count++; return s; }
	})`)
	info := session.GetSources()

	if _, err := session.Feed(testutil.Event("ItemAdded", "s-1", 0)); err != nil {
		t.Fatal(err)
	}

	summary := mustCollectState(t, session, info, nil)

	if summary.Partitioned {
		t.Error("expected unpartitioned")
	}
	if len(summary.State) == 0 {
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

func TestCollectState_Partitioned(t *testing.T) {
	session := newTestSession(t, `fromAll().foreachStream().when({
		$init() { return { count: 0 }; },
		ItemAdded(s, e) { s.count++; return s; }
	})`)
	info := session.GetSources()

	for i, stream := range []string{"s-1", "s-2", "s-1"} {
		if _, err := session.Feed(testutil.Event("ItemAdded", stream, i)); err != nil {
			t.Fatal(err)
		}
	}

	partitions := map[string]bool{"s-1": true, "s-2": true}
	summary := mustCollectState(t, session, info, partitions)

	if !summary.Partitioned {
		t.Error("expected partitioned")
	}
	if len(summary.Partitions) != 2 {
		t.Fatalf("partitions: got %d, want 2", len(summary.Partitions))
	}

	for key, wantCount := range map[string]float64{"s-1": 2, "s-2": 1} {
		ps, ok := summary.Partitions[key]
		if !ok {
			t.Errorf("missing partition %s", key)
			continue
		}
		var state map[string]any
		if err := json.Unmarshal(ps.State, &state); err != nil {
			t.Errorf("partition %s: %v", key, err)
			continue
		}
		if state["count"] != wantCount {
			t.Errorf("partition %s count: got %v, want %v", key, state["count"], wantCount)
		}
	}
}

func TestCollectState_WithTransforms(t *testing.T) {
	// V1 only - V2 doesn't iterate transforms; result == post-handler state.
	session := newTestSessionWithVersion(t, `fromAll().when({
		$init() { return { count: 0 }; },
		ItemAdded(s, e) { s.count++; return s; }
	}).transformBy(function(s) { return { doubled: s.count * 2 }; })`, 1)
	info := session.GetSources()

	if _, err := session.Feed(testutil.Event("ItemAdded", "s-1", 0)); err != nil {
		t.Fatal(err)
	}

	summary := mustCollectState(t, session, info, nil)

	if !summary.HasTransforms {
		t.Fatal("expected HasTransforms")
	}
	if len(summary.Result) == 0 {
		t.Fatal("expected result")
	}

	var result map[string]any
	if err := json.Unmarshal(summary.Result, &result); err != nil {
		t.Fatal(err)
	}
	if result["doubled"] != float64(2) {
		t.Errorf("result.doubled: got %v, want 2", result["doubled"])
	}
}

func TestCollectState_PartitionedWithTransforms(t *testing.T) {
	// V1 only - V2 doesn't iterate transforms; result == post-handler state.
	session := newTestSessionWithVersion(t, `fromAll().foreachStream().when({
		$init() { return { count: 0 }; },
		ItemAdded(s, e) { s.count++; return s; }
	}).transformBy(function(s) { return { doubled: s.count * 2 }; })`, 1)
	info := session.GetSources()

	if _, err := session.Feed(testutil.Event("ItemAdded", "s-1", 0)); err != nil {
		t.Fatal(err)
	}

	partitions := map[string]bool{"s-1": true}
	summary := mustCollectState(t, session, info, partitions)

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
	if len(data.State) == 0 {
		t.Error("expected state for partition")
	}
	if len(data.Result) == 0 {
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

func TestCollectState_BiState(t *testing.T) {
	session := newTestSession(t, `fromAll().foreachStream().when({
		$init() { return { count: 0 }; },
		$initShared() { return { total: 0 }; },
		ItemAdded(s, e) {
			s.count++;
			linkTo('totals', e);
			return s;
		},
		$any(s, e) {
			if (e.streamId === 'totals') { s.total++; return s; }
		}
	})`)
	info := session.GetSources()

	if !info.BiState {
		t.Skip("runtime did not report BiState")
	}

	if _, err := session.Feed(testutil.Event("ItemAdded", "s-1", 0)); err != nil {
		t.Fatal(err)
	}

	partitions := map[string]bool{"s-1": true}
	summary := mustCollectState(t, session, info, partitions)

	if !summary.HasBiState {
		t.Error("expected hasBiState")
	}
	if len(summary.SharedState) == 0 {
		t.Error("expected sharedState")
	}
}

func TestCollectState_GetResultError(t *testing.T) {
	// A throwing V1 transformBy makes GetResult error. CollectState must
	// propagate it rather than report the result as absent.
	session := newTestSessionWithVersion(t, `fromAll().when({
		$init() { return { count: 0 }; },
		ItemAdded(s, e) { s.count++; return s; }
	}).transformBy(function(s) { throw new Error("boom"); })`, 1)
	info := session.GetSources()

	// The throw surfaces at Feed (V1 computes the result eagerly); ignore it.
	// GetResult re-runs the transform on the next read and errors again.
	_, _ = session.Feed(testutil.Event("ItemAdded", "s-1", 0))

	_, err := CollectState(session, info, nil)
	if err == nil {
		t.Fatal("expected error from throwing transformBy")
	}
	if !strings.Contains(err.Error(), "reading result") {
		t.Errorf("expected a result-read error, got %v", err)
	}
}

func TestToMap_Unpartitioned(t *testing.T) {
	summary := StateSummary{
		State: json.RawMessage(`{"count":5}`),
	}

	m := summary.ToMap()

	state, ok := m["state"]
	if !ok {
		t.Fatal("expected state key")
	}
	if string(state.(json.RawMessage)) != `{"count":5}` {
		t.Errorf("state: got %s", state)
	}
	if _, ok := m["partitions"]; ok {
		t.Error("unexpected partitions key")
	}
}

func TestToMap_Partitioned(t *testing.T) {
	summary := StateSummary{
		Partitioned: true,
		Partitions: map[string]PartitionState{
			"s-1": {State: json.RawMessage(`{"count":2}`)},
			"s-2": {State: json.RawMessage(`{"count":1}`)},
		},
	}

	m := summary.ToMap()

	partitions, ok := m["partitions"].(map[string]any)
	if !ok {
		t.Fatal("expected partitions map")
	}
	if len(partitions) != 2 {
		t.Fatalf("partitions: got %d, want 2", len(partitions))
	}
	for _, key := range []string{"s-1", "s-2"} {
		pd, ok := partitions[key].(map[string]any)
		if !ok {
			t.Errorf("missing partition %s", key)
			continue
		}
		if _, ok := pd["state"]; !ok {
			t.Errorf("partition %s: missing state", key)
		}
	}
	if _, ok := m["state"]; ok {
		t.Error("unexpected top-level state key")
	}
}

func TestToMap_WithTransforms(t *testing.T) {
	summary := StateSummary{
		State:         json.RawMessage(`{"count":3}`),
		Result:        json.RawMessage(`{"doubled":6}`),
		HasTransforms: true,
	}

	m := summary.ToMap()

	if _, ok := m["state"]; !ok {
		t.Error("expected state key")
	}
	result, ok := m["result"]
	if !ok {
		t.Fatal("expected result key")
	}
	if string(result.(json.RawMessage)) != `{"doubled":6}` {
		t.Errorf("result: got %s", result)
	}
}

func TestToMap_TransformsFlagWithoutResult(t *testing.T) {
	summary := StateSummary{
		State:         json.RawMessage(`{"count":3}`),
		HasTransforms: true,
	}

	m := summary.ToMap()

	if _, ok := m["result"]; ok {
		t.Error("result should be absent when Result is empty")
	}
}

func TestToMap_BiStateWithSharedState(t *testing.T) {
	summary := StateSummary{
		Partitioned: true,
		Partitions: map[string]PartitionState{
			"s-1": {State: json.RawMessage(`{"count":1}`)},
		},
		SharedState: json.RawMessage(`{"total":10}`),
		HasBiState:  true,
	}

	m := summary.ToMap()

	shared, ok := m["sharedState"]
	if !ok {
		t.Fatal("expected sharedState key")
	}
	if string(shared.(json.RawMessage)) != `{"total":10}` {
		t.Errorf("sharedState: got %s", shared)
	}
}

func TestToMap_BiStateFlagWithoutSharedState(t *testing.T) {
	summary := StateSummary{
		State:      json.RawMessage(`{"count":1}`),
		HasBiState: true,
	}

	m := summary.ToMap()

	if _, ok := m["sharedState"]; ok {
		t.Error("sharedState should be absent when SharedState is empty")
	}
}

func TestToMap_Empty(t *testing.T) {
	summary := StateSummary{}

	m := summary.ToMap()

	if len(m) != 0 {
		t.Errorf("expected empty map, got %v", m)
	}
}

func TestDescribeSource(t *testing.T) {
	tests := []struct {
		name string
		info gafferruntime.ProjectionInfo
		want string
	}{
		{"all", gafferruntime.ProjectionInfo{AllStreams: true}, "all"},
		{"categories", gafferruntime.ProjectionInfo{Categories: []string{"order"}}, "categories"},
		{"streams", gafferruntime.ProjectionInfo{Streams: []string{"order-1"}}, "streams"},
		{"unknown", gafferruntime.ProjectionInfo{}, "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := DescribeSource(tt.info)
			if result["type"] != tt.want {
				t.Errorf("type: got %v, want %s", result["type"], tt.want)
			}
		})
	}
}

func TestDescribePartitioning(t *testing.T) {
	tests := []struct {
		name string
		info gafferruntime.ProjectionInfo
		want string
	}{
		{"byStream", gafferruntime.ProjectionInfo{ByStreams: true}, "byStream"},
		{"byCustomKey", gafferruntime.ProjectionInfo{ByCustomPartitions: true}, "byCustomKey"},
		{"none", gafferruntime.ProjectionInfo{}, "none"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DescribePartitioning(tt.info)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}
