package engine

import (
	"encoding/json"
	"testing"

	gafferruntime "github.com/kurrent-io/gaffer/bindings/go"
	"github.com/kurrent-io/gaffer/cli/internal/testutil"
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

func TestCollectState_Unpartitioned(t *testing.T) {
	session := newTestSession(t, `fromAll().when({
		$init: function() { return { count: 0 }; },
		ItemAdded: function(s, e) { s.count++; return s; }
	})`)
	info := session.GetSources()

	if _, err := session.Feed(testutil.Event("ItemAdded", "s-1", 0)); err != nil {
		t.Fatal(err)
	}

	summary := CollectState(session, info, nil)

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
		$init: function() { return { count: 0 }; },
		ItemAdded: function(s, e) { s.count++; return s; }
	})`)
	info := session.GetSources()

	for i, stream := range []string{"s-1", "s-2", "s-1"} {
		if _, err := session.Feed(testutil.Event("ItemAdded", stream, i)); err != nil {
			t.Fatal(err)
		}
	}

	partitions := map[string]bool{"s-1": true, "s-2": true}
	summary := CollectState(session, info, partitions)

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
	session := newTestSession(t, `fromAll().when({
		$init: function() { return { count: 0 }; },
		ItemAdded: function(s, e) { s.count++; return s; }
	}).transformBy(function(s) { return { doubled: s.count * 2 }; })`)
	info := session.GetSources()

	if _, err := session.Feed(testutil.Event("ItemAdded", "s-1", 0)); err != nil {
		t.Fatal(err)
	}

	summary := CollectState(session, info, nil)

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
	session := newTestSession(t, `fromAll().foreachStream().when({
		$init: function() { return { count: 0 }; },
		ItemAdded: function(s, e) { s.count++; return s; }
	}).transformBy(function(s) { return { doubled: s.count * 2 }; })`)
	info := session.GetSources()

	if _, err := session.Feed(testutil.Event("ItemAdded", "s-1", 0)); err != nil {
		t.Fatal(err)
	}

	partitions := map[string]bool{"s-1": true}
	summary := CollectState(session, info, partitions)

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
	})`)
	info := session.GetSources()

	if !info.IsBiState {
		t.Skip("runtime did not report IsBiState")
	}

	if _, err := session.Feed(testutil.Event("ItemAdded", "s-1", 0)); err != nil {
		t.Fatal(err)
	}

	partitions := map[string]bool{"s-1": true}
	summary := CollectState(session, info, partitions)

	if !summary.HasBiState {
		t.Error("expected hasBiState")
	}
	if len(summary.SharedState) == 0 {
		t.Error("expected sharedState")
	}
}
