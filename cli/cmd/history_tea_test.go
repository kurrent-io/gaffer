package cmd

import (
	"errors"
	"io"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/kurrent-io/gaffer/cli/internal/remote"
)

func newTestHistoryModel(raw []remote.Version, w, h int) historyModel {
	tw := newTextWriter(io.Discard, io.Discard)
	m := historyModel{
		name:      "orders",
		envLabel:  "staging",
		connLabel: "localhost:2114",
		tw:        tw,
		hs:        newHistoryStyles(io.Discard),
		total:     12,
		raw:       raw,
		versions:  collapseHistory(classifyHistory(raw)),
		width:     w,
		height:    h,
	}
	m.graph = computeHistoryGraph(m.versions)
	m.ow = operationWidth(m.versions)
	return m
}

func asModel(t *testing.T, m tea.Model) historyModel {
	t.Helper()
	hm, ok := m.(historyModel)
	if !ok {
		t.Fatalf("not a historyModel: %T", m)
	}
	return hm
}

func key(s string) tea.KeyMsg {
	switch s {
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

func sampleHistory() []remote.Version {
	return []remote.Version{
		ver(7, "current", true, gafferLedger(remote.OpDeploy)),
		ver(6, "tampered", true, nil),
		ver(5, "original", true, gafferLedger(remote.OpDeploy)),
	}
}

func TestHistoryViewShowsTimelineDetailAndFooter(t *testing.T) {
	m := newTestHistoryModel(sampleHistory(), 100, 20)
	out := m.View()
	for _, want := range []string{
		"deploy", "edited externally", // operation column on content rows
		"HISTORY", "orders", // footer left
		"1 of 12",                   // cursor position (no vN)
		"staging", "localhost:2114", // footer target
		"q quit", // controls
	} {
		if !strings.Contains(out, want) {
			t.Errorf("view missing %q\n---\n%s", want, out)
		}
	}
}

func TestHistoryDetailFollowsCursor(t *testing.T) {
	m := newTestHistoryModel(sampleHistory(), 100, 20)
	// Cursor starts on v7 (a gaffer deploy): the detail panel shows its deployer.
	if !strings.Contains(m.View(), "george@kurrent.io") {
		t.Errorf("detail for v7 should name the deployer\n%s", m.View())
	}
	// Move to v6 (edited externally): the panel shows the out-of-band caution.
	nm, _ := m.handleKey(key("down"))
	m = asModel(t, nm)
	if m.cursor != 1 {
		t.Fatalf("cursor = %d, want 1 after down", m.cursor)
	}
	if !strings.Contains(m.View(), "outside gaffer") {
		t.Errorf("detail for v6 should show the external-edit caution\n%s", m.View())
	}
}

func TestHistoryNavigationClamps(t *testing.T) {
	m := newTestHistoryModel(sampleHistory(), 100, 20)
	nm, _ := m.handleKey(key("up")) // already at top
	m = asModel(t, nm)
	if m.cursor != 0 {
		t.Errorf("cursor = %d, want clamped at 0", m.cursor)
	}
	nm, _ = m.handleKey(key("G")) // jump to oldest
	m = asModel(t, nm)
	if m.cursor != 2 {
		t.Errorf("cursor = %d, want 2 (last) after G", m.cursor)
	}
	nm, _ = m.handleKey(key("down")) // past the end
	m = asModel(t, nm)
	if m.cursor != 2 {
		t.Errorf("cursor = %d, want clamped at 2", m.cursor)
	}
	nm, _ = m.handleKey(key("g")) // back to head
	m = asModel(t, nm)
	if m.cursor != 0 {
		t.Errorf("cursor = %d, want 0 after g", m.cursor)
	}
}

func TestHistoryQuits(t *testing.T) {
	m := newTestHistoryModel(sampleHistory(), 100, 20)
	for _, k := range []string{"q", "esc", "ctrl+c"} {
		_, cmd := m.handleKey(key(k))
		if cmd == nil {
			t.Errorf("%q should quit (return a command)", k)
			continue
		}
		if msg := cmd(); msg != tea.Quit() {
			t.Errorf("%q returned %T, want tea.Quit", k, msg)
		}
	}
}

func TestHistoryMatchesEarlierAnnotation(t *testing.T) {
	// The newest "original" reverts to the older one; the match looks backward only,
	// so the newest is flagged and the original occurrence is not.
	raw := []remote.Version{
		ver(7, "original", true, gafferLedger(remote.OpDeploy)),
		ver(6, "tampered", true, nil),
		ver(5, "original", true, gafferLedger(remote.OpDeploy)),
	}
	m := newTestHistoryModel(raw, 100, 20)
	if !m.matchesEarlier(0) {
		t.Error("newest 'original' should match an earlier deploy")
	}
	if m.matchesEarlier(2) {
		t.Error("the original occurrence should not match anything earlier")
	}
	m.cursor = 0
	if !strings.Contains(m.View(), "matches an earlier deploy") {
		t.Errorf("a revert should be flagged in the detail panel\n%s", m.View())
	}
}

func TestHistoryPanelPriority(t *testing.T) {
	// The panel is the priority. Wide: it sits at its comfortable width and the
	// timeline takes the rest. Once the timeline degrades to blobs, the panel expands
	// into the leftover space. Narrower still, the panel crushes rather than dropping.
	wide := newTestHistoryModel(sampleHistory(), 100, 20)
	if left, right := wide.paneWidths(); right != 46 || left != 54 {
		t.Errorf("wide = (%d, %d), want (54, 46)", left, right)
	}
	blobs := newTestHistoryModel(sampleHistory(), 60, 20)
	if left, right := blobs.paneWidths(); left != 3 || right != 57 {
		t.Errorf("blobs = (%d, %d), want (3, 57) - panel expands into the gap", left, right)
	}
	narrow := newTestHistoryModel(sampleHistory(), 30, 20)
	if left, right := narrow.paneWidths(); left != 3 || right != 27 {
		t.Errorf("narrow = (%d, %d), want (3, 27) - panel crushed to fit", left, right)
	}
	if strings.TrimSpace(narrow.View()) == "" {
		t.Error("narrow view should still render")
	}
}

func TestHistoryScrollSetsLoading(t *testing.T) {
	// Scrolling to the bottom of a partial window must fire a page load and the
	// returned model must carry loading=true - a return-order bug once dropped it,
	// defeating the de-dup guard and the loading indicator.
	var raw []remote.Version
	for n := int64(20); n >= 1; n-- { // oldest is version 1 (>0), so more pages remain
		raw = append(raw, ver(n, "q", true, gafferLedger(remote.OpDeploy)))
	}
	m := newTestHistoryModel(raw, 100, 20)
	m.cursor = len(m.versions) - 1 // at the bottom, inside the load threshold
	next, cmd := m.handleKey(key("down"))
	if cmd == nil {
		t.Fatal("scrolling to the bottom should schedule a page load")
	}
	if !asModel(t, next).loading {
		t.Error("the returned model should carry loading=true")
	}
}

func TestHistoryEmptyPageClearsStaleLoadError(t *testing.T) {
	// A successful empty page (stream exhausted) must clear an earlier failure,
	// or the footer keeps showing "load failed" for the rest of the session.
	m := newTestHistoryModel(sampleHistory(), 100, 20)
	m.loadErr = errors.New("transient read failure")
	nm, _ := m.Update(historyLoadedMsg{versions: nil})
	m = asModel(t, nm)
	if m.loadErr != nil {
		t.Errorf("loadErr = %v after a successful empty page, want cleared", m.loadErr)
	}
	if !m.exhausted {
		t.Error("empty page should still mark the stream exhausted")
	}
}

func TestHistoryFoldsRecreateBookendsAcrossPages(t *testing.T) {
	// A recreate at the bottom of the loaded window folds its bookends when the
	// next page brings them in: the tombstone and disable never appear as rows,
	// and the rows already on screen keep their indices.
	m := newTestHistoryModel([]remote.Version{
		ver(4, "q", true, gafferLedger(remote.OpRecreate)),
	}, 100, 20)
	if len(m.versions) != 1 || len(m.versions[0].Absorbed) != 0 {
		t.Fatalf("precondition: want the bare recreate row, got %d rows", len(m.versions))
	}
	nm, _ := m.Update(historyLoadedMsg{versions: []remote.Version{
		tombstone(3, "q"),
		ver(2, "q", false, nil),
		ver(1, "q", true, gafferLedger(remote.OpDeploy)),
	}})
	m = asModel(t, nm)
	if len(m.versions) != 2 {
		t.Fatalf("got %d rows after the page, want 2 (recreate + deploy): %v", len(m.versions), kinds(m.versions))
	}
	if m.versions[0].Kind != kindRecreate || len(m.versions[0].Absorbed) != 2 {
		t.Errorf("row 0 = %q with %d absorbed, want recreate folding both bookends", m.versions[0].Kind, len(m.versions[0].Absorbed))
	}
	if m.versions[1].Kind != kindDeploy {
		t.Errorf("row 1 = %q, want the deploy", m.versions[1].Kind)
	}
}

func TestHistoryRecreateDetailAndFooter(t *testing.T) {
	// A collapsed recreate: the detail panel names the folded steps, and the
	// footer discounts them so the position can reach "N of N".
	m := newTestHistoryModel([]remote.Version{
		ver(3, "q", true, gafferLedger(remote.OpRecreate)),
		tombstone(2, "q"),
		ver(1, "q", false, nil),
		ver(0, "q", true, gafferLedger(remote.OpDeploy)),
	}, 100, 20)
	m.total = 4
	out := m.View()
	if !strings.Contains(out, "reprocessed from zero") {
		t.Errorf("detail should note the reprocess\n%s", out)
	}
	if n := strings.Count(out, "┬"); n != 1 {
		t.Errorf("the rail below a recreate should carry exactly one termination cap, got %d\n%s", n, out)
	}
	if strings.Contains(out, "folds") {
		t.Errorf("detail should not mention the fold mechanics\n%s", out)
	}
	if !strings.Contains(out, "1 of 2") {
		t.Errorf("footer should discount the 2 folded bookends (want 1 of 2)\n%s", out)
	}
}

func TestHistoryEmptyPageStopsPaging(t *testing.T) {
	// An empty page must set exhausted so a gap of non-state events doesn't make
	// every keypress re-fetch the same window forever.
	m := newTestHistoryModel(sampleHistory(), 100, 20)
	m.cursor = len(m.versions) - 1 // at the bottom, where paging would fire
	if cmd := m.loadPage(false); cmd == nil {
		t.Fatal("should page when older versions remain (oldest > 0)")
	}
	m.loading = false
	nm, _ := m.Update(historyLoadedMsg{versions: nil})
	m = asModel(t, nm)
	if !m.exhausted {
		t.Fatal("an empty page should mark the stream exhausted")
	}
	if cmd := m.loadPage(false); cmd != nil {
		t.Error("exhausted: must not page again")
	}
}

func TestClampTop(t *testing.T) {
	for _, tc := range []struct {
		top, cursor, height, want int
	}{
		{0, 0, 10, 0},  // cursor visible at top
		{0, 5, 10, 0},  // still within window, hold position
		{0, 12, 10, 3}, // cursor below window: scroll down to show it
		{5, 4, 10, 4},  // cursor above window: scroll up to it
		{5, 8, 10, 5},  // within window after scroll: hold
		{3, 3, 0, 0},   // degenerate height
	} {
		if got := clampTop(tc.top, tc.cursor, tc.height); got != tc.want {
			t.Errorf("clampTop(%d,%d,%d) = %d, want %d", tc.top, tc.cursor, tc.height, got, tc.want)
		}
	}
}

func TestHistoryWindowSizeMsg(t *testing.T) {
	m := newTestHistoryModel(sampleHistory(), 0, 0)
	nm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = asModel(t, nm)
	if m.width != 80 || m.height != 24 {
		t.Errorf("size = (%d, %d), want (80, 24)", m.width, m.height)
	}
}

func TestRedactConnection(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"esdb://localhost:2114?tls=false", "localhost:2114"},
		{"kurrentdb://user:secret@db.example:2113", "db.example:2113"},
		{"", ""},
		{"not a url with spaces", "the target"},
	} {
		if got := redactConnection(tc.in); got != tc.want {
			t.Errorf("redactConnection(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestTruncate(t *testing.T) {
	for _, tc := range []struct {
		s    string
		w    int
		want string
	}{
		{"hello", 10, "hello"}, // fits, unchanged
		{"hello world", 8, ""}, // too long: ellipsised, checked by width below
		{"abc", 0, ""},         // no room
		{"abc", 1, "…"},        // single cell
	} {
		got := truncate(tc.s, tc.w)
		if tc.want != "" && got != tc.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", tc.s, tc.w, got, tc.want)
		}
		if lipgloss.Width(got) > tc.w {
			t.Errorf("truncate(%q, %d) = %q exceeds width %d", tc.s, tc.w, got, tc.w)
		}
	}
}
