package cmd

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/kurrent-io/gaffer/cli/internal/remote"
)

func diffableHistory() []remote.Version {
	return []remote.Version{
		ver(3, "fromAll()\ncount(2)\n", true, gafferLedger(remote.OpDeploy)),
		ver(2, "fromAll()\ncount(1)\n", false, nil), // disabled
		ver(1, "fromAll()\ncount(1)\n", true, gafferLedger(remote.OpDeploy)),
		ver(0, "fromAll()\n", true, nil),
	}
}

func TestHistoryDiffModalOpenClose(t *testing.T) {
	m := newTestHistoryModel(diffableHistory(), 100, 30)
	m.cursor = 1 // remember the position across open/close

	nm, _ := m.Update(key("d"))
	m = asModel(t, nm)
	if !m.diffOpen {
		t.Fatal("d should open the diff modal")
	}
	if v := m.View(); !strings.Contains(v, "╭") || !strings.Contains(v, "scrub") {
		t.Errorf("open modal missing from the view:\n%s", v)
	}

	nm, _ = m.Update(key("esc"))
	m = asModel(t, nm)
	if m.diffOpen {
		t.Fatal("esc should close the modal")
	}
	if m.cursor != 1 {
		t.Errorf("cursor = %d after close, want the position it opened at (1)", m.cursor)
	}
	// q quits the program only when the modal is closed.
	if _, cmd := m.Update(key("q")); cmd == nil {
		t.Error("q on the timeline should quit")
	}
}

func TestHistoryDiffModalScrubMovesSelection(t *testing.T) {
	m := newTestHistoryModel(diffableHistory(), 100, 30)
	nm, _ := m.Update(key("d"))
	m = asModel(t, nm)
	m.diffScroll = 3

	nm, _ = m.Update(key("down"))
	m = asModel(t, nm)
	if m.cursor != 1 {
		t.Errorf("cursor = %d, want 1 - arrows scrub the timeline under the modal", m.cursor)
	}
	if m.diffScroll != 0 {
		t.Errorf("diffScroll = %d, want reset on scrub", m.diffScroll)
	}
	if !m.diffOpen {
		t.Error("scrubbing must keep the modal open")
	}
	// The scrubbed-to entry is a state change: the modal reports it rather than
	// rendering a bogus diff.
	if v := m.View(); !strings.Contains(v, "no definition change") {
		t.Errorf("state-change entry should read 'no definition change':\n%s", v)
	}
}

func TestHistoryDiffModalScroll(t *testing.T) {
	// A query long enough to overflow the modal body at height 20.
	long := "fromAll()\n" + strings.Repeat("s.count += 1;\n", 40)
	m := newTestHistoryModel([]remote.Version{
		ver(1, long, true, gafferLedger(remote.OpDeploy)),
		ver(0, "fromAll()\n", true, nil),
	}, 100, 20)
	nm, _ := m.Update(key("d"))
	m = asModel(t, nm)

	before := m.View()
	nm, _ = m.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	m = asModel(t, nm)
	if m.diffScroll == 0 {
		t.Fatal("pgdown should scroll the diff body")
	}
	if after := m.View(); after == before {
		t.Error("scrolling should change the rendered window")
	}
	nm, _ = m.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	m = asModel(t, nm)
	if m.diffScroll != 0 {
		t.Errorf("diffScroll = %d after pgup, want back to 0", m.diffScroll)
	}
}

func TestHistoryDiffModalChasesUnloadedBaseline(t *testing.T) {
	// The oldest loaded version is 5 (> 0, more pages exist) and nothing older
	// in view carries content, so opening the modal must force a page load even
	// though the cursor isn't near the bottom threshold.
	m := newTestHistoryModel([]remote.Version{
		ver(6, "b\n", true, gafferLedger(remote.OpDeploy)),
		ver(5, "b\n", false, nil), // disabled: no content baseline in view
	}, 100, 30)
	m.cursor = 0
	nm, cmd := m.Update(key("d"))
	m = asModel(t, nm)
	if cmd == nil || !m.loading {
		t.Fatalf("cmd=%v loading=%v - opening on an unloaded baseline should force a page load", cmd, m.loading)
	}
	if v := m.View(); !strings.Contains(v, "loading older entries") {
		t.Errorf("modal should say it's loading the baseline:\n%s", v)
	}
}

func TestHistoryDiffModalScrollClamps(t *testing.T) {
	long := "fromAll()\n" + strings.Repeat("s.count += 1;\n", 40)
	m := newTestHistoryModel([]remote.Version{
		ver(1, long, true, gafferLedger(remote.OpDeploy)),
		ver(0, "fromAll()\n", true, nil),
	}, 100, 20)
	nm, _ := m.Update(key("d"))
	m = asModel(t, nm)
	for range 10 {
		nm, _ = m.Update(tea.KeyMsg{Type: tea.KeyPgDown})
		m = asModel(t, nm)
	}
	if m.diffScroll != m.diffMaxScroll() {
		t.Fatalf("diffScroll = %d after overscroll, want clamped to %d", m.diffScroll, m.diffMaxScroll())
	}
	// The very next pgup must move the window - no dead offset to unwind.
	before := m.diffScroll
	nm, _ = m.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	m = asModel(t, nm)
	if m.diffScroll >= before {
		t.Errorf("pgup did not move: %d -> %d", before, m.diffScroll)
	}
}

func TestHistoryDiffModalTinyTerminal(t *testing.T) {
	// Narrower than the modal's 30-cell floor: the box clamps to the terminal
	// and no composited row may exceed it (an oversized row wraps and corrupts
	// the frame).
	m := newTestHistoryModel(diffableHistory(), 20, 8)
	nm, _ := m.Update(key("d"))
	m = asModel(t, nm)
	for i, line := range strings.Split(m.View(), "\n") {
		if w := ansi.StringWidth(line); w > 20 {
			t.Errorf("line %d is %d cells wide, exceeds the 20-cell terminal: %q", i, w, line)
		}
	}
}

func TestHistoryDiffModalExhaustedBecomesFirstVersion(t *testing.T) {
	// The modal is waiting on an unloaded baseline; an empty page (stream
	// exhausted) resolves it to a genuine first version, diffed from empty.
	m := newTestHistoryModel([]remote.Version{
		ver(6, "b\n", true, gafferLedger(remote.OpDeploy)),
		ver(5, "b\n", false, nil),
	}, 100, 30)
	nm, _ := m.Update(key("d"))
	m = asModel(t, nm)
	nm, _ = m.Update(historyLoadedMsg{versions: nil})
	m = asModel(t, nm)
	if !m.exhausted {
		t.Fatal("empty page should mark the stream exhausted")
	}
	if v := m.View(); !strings.Contains(v, "first version") {
		t.Errorf("modal should resolve to a from-empty diff:\n%s", v)
	}
}

func TestPlaceOverlayStyledBackground(t *testing.T) {
	// A styled background row keeps its width and the overlay is isolated by
	// resets on both seams.
	bg := "\x1b[31maaaaaaaaaa\x1b[0m"
	got := placeOverlay(4, 0, "XX", bg)
	if w := ansi.StringWidth(got); w != 10 {
		t.Errorf("composited width = %d, want 10", w)
	}
	if !strings.Contains(got, ansi.ResetStyle+"XX"+ansi.ResetStyle) {
		t.Errorf("overlay not reset-isolated: %q", got)
	}
}

func TestPlaceOverlay(t *testing.T) {
	bg := "aaaaaaaa\nbbbbbbbb\ncccccccc"
	got := placeOverlay(2, 1, "XX", bg)
	want := "aaaaaaaa\nbb" + ansi.ResetStyle + "XX" + ansi.ResetStyle + "bbbb\ncccccccc"
	if got != want {
		t.Errorf("placeOverlay = %q, want %q", got, want)
	}
	// A background shorter than the overlay column is padded, not truncated.
	got = placeOverlay(4, 0, "Y", "ab")
	if !strings.Contains(got, "ab  ") || !strings.Contains(got, "Y") {
		t.Errorf("short background not padded: %q", got)
	}
}
