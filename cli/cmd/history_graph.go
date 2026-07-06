package cmd

import "slices"

// historyMaxLanes bounds the timeline's width: the main line plus two branch lanes,
// so a revert nested inside a revert still draws, but nothing deeper does (it falls
// back to a flat row). Three lanes covers every real case without a gutter wide
// enough to crowd the content.
const historyMaxLanes = 3

// graphSpan is a revert bracket: the content at row top (newer) reappears, matching
// the same content at row bottom (older). The rows strictly between are the detour
// that was undone, drawn in lane (1 = the first branch lane; the main line is 0).
type graphSpan struct {
	top    int // newer endpoint (smaller index, the reappearance / restore)
	bottom int // older endpoint (larger index, the original)
	lane   int
}

// contains reports whether a fully encloses s. A shared endpoint still counts as
// containment (a revert landing exactly on an older bracket's edge nests rather
// than reading as a crossing); an identical span does not contain itself.
func (a graphSpan) contains(s graphSpan) bool {
	return a.top <= s.top && a.bottom >= s.bottom && a != s
}

// interleaves reports whether a and s overlap without either containing the other -
// the crossing we can't draw in stacked lanes. Touching at a single shared endpoint
// (sequential reverts) is not an interleave.
func (a graphSpan) interleaves(s graphSpan) bool {
	lo, hi := a, s
	if hi.top < lo.top {
		lo, hi = hi, lo
	}
	return hi.top > lo.top && hi.top < lo.bottom && hi.bottom > lo.bottom
}

// historyGraph is the lane layout for a timeline: the lane each row's node sits in
// and the revert brackets that were drawn. It's derived purely from content-hash
// reappearance - independent of rendering, so it's unit-testable on its own.
type historyGraph struct {
	nodeLane []int       // lane of each row's node (0 = main line)
	spans    []graphSpan // accepted brackets
}

// computeHistoryGraph lays out the revert brackets for a classified, newest-first
// history. It finds every content that reappears, then greedily accepts brackets
// newest-first: a bracket is dropped when it would cross an already-accepted one
// (newest wins - the older span loses its bracket but keeps its rows) or nest
// deeper than the lane cap. What survives is a clean forest of nested/sequential
// brackets, and each row's node lane follows from the deepest bracket enclosing it.
func computeHistoryGraph(versions []historyVersion) historyGraph {
	g := historyGraph{nodeLane: make([]int, len(versions))}
	for _, s := range candidateSpans(versions) {
		lane := spanLane(s, g.spans)
		if lane < 0 {
			continue // interleaves with an accepted span, or nests too deep
		}
		s.lane = lane
		g.spans = append(g.spans, s)
	}
	for i := range g.nodeLane {
		g.nodeLane[i] = nodeLaneAt(i, g.spans)
	}
	return g
}

// candidateSpans pairs each content hash with its previous occurrence, yielding a
// bracket wherever the same content reappears with a genuine detour between. Only
// content versions anchor a span - a state change (disable / reconfigure / rewrite)
// carries no content identity of its own, so it can't be an endpoint. The gap is
// counted in content versions, not raw rows, so a same-content lifecycle toggle
// between two identical deploys (deploy X, disable, deploy X) isn't mistaken for a
// detour, while a real one (deploy X, deploy Y, deploy X) still is. Spans come out
// sorted newest-top first, the order the greedy pass needs so newer reverts win.
func candidateSpans(versions []historyVersion) []graphSpan {
	type occurrence struct{ index, pos int } // raw row index and content-version position
	last := map[string]occurrence{}
	var spans []graphSpan
	pos := 0
	for i, hv := range versions {
		if hv.ContentHash == "" || hv.StateChange() {
			continue
		}
		if o, ok := last[hv.ContentHash]; ok && pos-o.pos > 1 {
			spans = append(spans, graphSpan{top: o.index, bottom: i})
		}
		last[hv.ContentHash] = occurrence{index: i, pos: pos}
		pos++
	}
	slices.SortStableFunc(spans, func(a, b graphSpan) int { return a.top - b.top })
	return spans
}

// spanLane places a candidate relative to the already-accepted brackets: its lane
// is one past the deepest accepted span that contains it. It's rejected (-1) if it
// interleaves with any accepted span (the accepted one has the newer top, so it
// wins) or if nesting it would exceed the lane cap.
func spanLane(s graphSpan, accepted []graphSpan) int {
	depth := 0
	for _, a := range accepted {
		switch {
		case a.contains(s):
			depth++
		case a.interleaves(s):
			return -1
		}
	}
	if lane := depth + 1; lane < historyMaxLanes {
		return lane
	}
	return -1
}

// nodeLaneAt is the lane a row's node sits in: the deepest accepted span it's
// strictly interior to - a bracket's own endpoints stay on the enclosing line, so
// they read as the fork and rejoin rather than part of the detour.
func nodeLaneAt(i int, spans []graphSpan) int {
	lane := 0
	for _, s := range spans {
		if s.top < i && i < s.bottom && s.lane > lane {
			lane = s.lane
		}
	}
	return lane
}
