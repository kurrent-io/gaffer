package cmd

import (
	"reflect"
	"testing"

	"github.com/kurrent-io/gaffer/cli/internal/remote"
)

// graphOf lays out a timeline from a newest-first list of content hashes. An empty
// string is a state change (no content identity), matching a row whose hash column
// is blank.
func graphOf(hashes ...string) historyGraph {
	vs := make([]historyVersion, len(hashes))
	for i, h := range hashes {
		vs[i].ContentHash = h
		vs[i].Hash = h
	}
	return computeHistoryGraph(vs)
}

func TestHistoryGraphNoReverts(t *testing.T) {
	g := graphOf("aaa", "bbb", "ccc")
	if len(g.spans) != 0 {
		t.Fatalf("spans = %+v, want none", g.spans)
	}
	if want := []int{0, 0, 0}; !reflect.DeepEqual(g.nodeLane, want) {
		t.Errorf("nodeLane = %v, want %v", g.nodeLane, want)
	}
}

func TestHistoryGraphSimpleRevert(t *testing.T) {
	// aaa reappears at row 0, matching row 3; rows 1-2 are the detour.
	g := graphOf("aaa", "bbb", "", "aaa")
	if len(g.spans) != 1 {
		t.Fatalf("spans = %+v, want one", g.spans)
	}
	if s := g.spans[0]; s.top != 0 || s.bottom != 3 || s.lane != 1 {
		t.Errorf("span = %+v, want {0 3 1}", s)
	}
	if want := []int{0, 1, 1, 0}; !reflect.DeepEqual(g.nodeLane, want) {
		t.Errorf("nodeLane = %v, want %v (endpoints on main, detour in lane 1)", g.nodeLane, want)
	}
}

func TestHistoryGraphAdjacentRewriteIsNotABranch(t *testing.T) {
	// Two identical writes with nothing between: a rewrite, not a revert.
	g := graphOf("aaa", "aaa", "bbb")
	if len(g.spans) != 0 {
		t.Fatalf("spans = %+v, want none (adjacent identical is a rewrite)", g.spans)
	}
}

func TestHistoryGraphSequentialRevertsShareALane(t *testing.T) {
	// aaa at rows 0, 2, 5: two back-to-back reverts sharing the middle endpoint.
	// Both are top-level, so both draw in lane 1.
	g := graphOf("aaa", "x", "aaa", "y", "z", "aaa")
	if len(g.spans) != 2 {
		t.Fatalf("spans = %+v, want two sequential", g.spans)
	}
	for _, s := range g.spans {
		if s.lane != 1 {
			t.Errorf("span %+v lane = %d, want 1 (sequential, not nested)", s, s.lane)
		}
	}
	if want := []int{0, 1, 0, 1, 1, 0}; !reflect.DeepEqual(g.nodeLane, want) {
		t.Errorf("nodeLane = %v, want %v", g.nodeLane, want)
	}
}

func TestHistoryGraphNestedRevert(t *testing.T) {
	// Outer aaa (0..6) with an inner bbb revert (2..4) inside its detour.
	g := graphOf("aaa", "d", "bbb", "e", "bbb", "f", "aaa")
	outer, inner := false, false
	for _, s := range g.spans {
		switch {
		case s.top == 0 && s.bottom == 6:
			outer = true
			if s.lane != 1 {
				t.Errorf("outer lane = %d, want 1", s.lane)
			}
		case s.top == 2 && s.bottom == 4:
			inner = true
			if s.lane != 2 {
				t.Errorf("inner lane = %d, want 2", s.lane)
			}
		}
	}
	if !outer || !inner {
		t.Fatalf("spans = %+v, want nested outer{0 6} + inner{2 4}", g.spans)
	}
	// Row 3 sits in the inner detour (lane 2); the inner endpoints (2, 4) sit in the
	// outer detour (lane 1); the outer endpoints (0, 6) on the main line.
	if want := []int{0, 1, 1, 2, 1, 1, 0}; !reflect.DeepEqual(g.nodeLane, want) {
		t.Errorf("nodeLane = %v, want %v", g.nodeLane, want)
	}
}

func TestHistoryGraphNestingCapDropsThirdLevel(t *testing.T) {
	// Three levels deep: outer (0..8), middle (2..6), inner (4..... wait
	// aaa 0/8, bbb 2/6, ccc 4/... needs a matching pair one level deeper.
	g := graphOf("aaa", "p", "bbb", "q", "ccc", "r", "ccc", "s", "bbb", "t", "aaa")
	//            0     1    2      3    4      5    6      7    8      9    10
	// aaa 0/10 (lane1), bbb 2/8 (lane2), ccc 4/6 would be lane3 -> dropped (flat).
	for _, s := range g.spans {
		if s.top == 4 && s.bottom == 6 {
			t.Fatalf("innermost span %+v was accepted, want dropped at the lane cap", s)
		}
		if s.lane >= historyMaxLanes {
			t.Errorf("span %+v lane %d exceeds cap %d", s, s.lane, historyMaxLanes)
		}
	}
	// Row 5 (the would-be inner detour) falls back to its enclosing lane (2), not 3.
	if g.nodeLane[5] >= historyMaxLanes {
		t.Errorf("nodeLane[5] = %d, want < %d (flattened)", g.nodeLane[5], historyMaxLanes)
	}
}

func TestHistoryGraphInterleaveNewestWins(t *testing.T) {
	// A at rows 0,2; B at rows 1,3 - the spans cross. The newer-top span (A, top 0)
	// keeps its bracket; the older (B, top 1) is dropped to flat.
	g := graphOf("A", "B", "A", "B")
	if len(g.spans) != 1 {
		t.Fatalf("spans = %+v, want one (the newer span wins the crossing)", g.spans)
	}
	if s := g.spans[0]; s.top != 0 || s.bottom != 2 {
		t.Errorf("kept span = %+v, want the newer {0 2}", s)
	}
	// A's detour (row 1) draws in lane 1; B's rows (1 already placed, 3) stay off any
	// bracket of their own - row 3 is on the main line.
	if want := []int{0, 1, 0, 0}; !reflect.DeepEqual(g.nodeLane, want) {
		t.Errorf("nodeLane = %v, want %v", g.nodeLane, want)
	}
}

func TestHistoryGraphEmpty(t *testing.T) {
	if g := graphOf(); len(g.spans) != 0 || len(g.nodeLane) != 0 {
		t.Errorf("empty history graph = %+v, want zero", g)
	}
}

func TestHistoryGraphIgnoresStateChangeEndpoints(t *testing.T) {
	// deploy b, disable (still b), deploy b: the content never diverged, so a bracket
	// here would be spurious - a state change can't anchor a revert.
	g := computeHistoryGraph(classifyHistory([]remote.Version{
		ver(2, "b", true, gafferLedger(remote.OpDeploy)),
		ver(1, "b", false, nil), // disabled - same content
		ver(0, "b", true, gafferLedger(remote.OpDeploy)),
	}))
	if len(g.spans) != 0 {
		t.Fatalf("spans = %+v, want none (a lifecycle toggle between identical deploys isn't a revert)", g.spans)
	}

	// deploy X, deploy Y, disable X, deploy X: a genuine revert. The endpoints are the
	// X deploys (rows 0 and 3), not the disabled row.
	g = computeHistoryGraph(classifyHistory([]remote.Version{
		ver(3, "X", true, gafferLedger(remote.OpDeploy)),
		ver(2, "Y", true, gafferLedger(remote.OpDeploy)),
		ver(1, "X", false, nil), // disabled X
		ver(0, "X", true, gafferLedger(remote.OpDeploy)),
	}))
	if len(g.spans) != 1 || g.spans[0].top != 0 || g.spans[0].bottom != 3 {
		t.Fatalf("spans = %+v, want one {0 3} anchored on the X deploys", g.spans)
	}
}
