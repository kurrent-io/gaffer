package cmd

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/kurrent-io/gaffer/cli/internal/remote"
)

// rollbackHistory is a timeline with a rollback target: the deployed content at
// the head, a state change over the older content, and the older content itself.
func rollbackHistory() []remote.Version {
	return []remote.Version{
		ver(3, "current", true, gafferLedger(remote.OpDeploy)),
		ver(2, "older", false, nil), // disabled: same content as below, a pure state change
		ver(1, "older", true, gafferLedger(remote.OpDeploy)),
	}
}

func TestHistoryRollbackAt(t *testing.T) {
	versions := collapseHistory(classifyHistory(rollbackHistory()))

	t.Run("older content is ready with a current->selected diff", func(t *testing.T) {
		rb := historyRollbackAt(versions, 2)
		if rb.state != rbReady || rb.cur == nil {
			t.Fatalf("state = %v, want ready with the deployed head found", rb.state)
		}
		if rb.cur.Number != 3 || rb.sel.Number != 1 {
			t.Errorf("cur v%d sel v%d, want deployed v3 and target v1", rb.cur.Number, rb.sel.Number)
		}
		var removed, added string
		for _, l := range rb.lines {
			switch l.Kind.String() {
			case "removed":
				removed += l.Text
			case "added":
				added += l.Text
			}
		}
		if !strings.Contains(removed, "current") || !strings.Contains(added, "older") {
			t.Errorf("diff direction wrong: removed %q added %q, want current removed and older added", removed, added)
		}
	})

	t.Run("the deployed content has nothing to roll back", func(t *testing.T) {
		if rb := historyRollbackAt(versions, 0); rb.state != rbCurrent {
			t.Errorf("state = %v, want rbCurrent", rb.state)
		}
	})

	t.Run("a state change is no target", func(t *testing.T) {
		if rb := historyRollbackAt(versions, 1); rb.state != rbNoTarget {
			t.Errorf("state = %v, want rbNoTarget", rb.state)
		}
	})

	t.Run("an engine-version difference refuses", func(t *testing.T) {
		raw := rollbackHistory()
		raw[2].Definition.EngineVersion = 2
		vs := collapseHistory(classifyHistory(raw))
		if rb := historyRollbackAt(vs, 2); rb.state != rbRefused {
			t.Errorf("state = %v, want rbRefused", rb.state)
		}
	})
}

func TestHistoryRollbackModalFlow(t *testing.T) {
	m := newTestHistoryModel(rollbackHistory(), 100, 24)
	m.cursor = 2 // the older content version

	// r opens the confirm; the view shows the proposal and cautions.
	nm, _ := m.handleKey(key("r"))
	m = asModel(t, nm)
	if !m.rbOpen {
		t.Fatal("r should open the rollback modal")
	}
	out := m.View()
	for _, want := range []string{"roll back to", "replaces", "code rolls back, state does not", "y confirm"} {
		if !strings.Contains(out, want) {
			t.Errorf("modal missing %q\n%s", want, out)
		}
	}

	// y fires the update and deadens the keys while it flies.
	nm, cmd := m.handleKey(key("y"))
	m = asModel(t, nm)
	if !m.rbBusy || cmd == nil {
		t.Fatalf("y should fire the apply (busy=%v cmd=%v)", m.rbBusy, cmd != nil)
	}
	if nm2, cmd2 := m.handleKey(key("y")); cmd2 != nil || !asModel(t, nm2).rbBusy {
		t.Error("a second y while busy must not double-fire")
	}

	// A failed apply surfaces in the modal for a retry.
	nm, _ = m.Update(rollbackAppliedMsg{err: errors.New("boom")})
	m = asModel(t, nm)
	if m.rbBusy || m.rbErr == nil || !m.rbOpen {
		t.Fatalf("failed apply: busy=%v err=%v open=%v, want retryable modal", m.rbBusy, m.rbErr, m.rbOpen)
	}
	if !strings.Contains(m.View(), "rollback failed") {
		t.Error("the modal should show the failure")
	}

	// A successful apply reloads from the head...
	m.rbBusy = true
	nm, cmd = m.Update(rollbackAppliedMsg{})
	m = asModel(t, nm)
	if cmd == nil {
		t.Fatal("a landed rollback should trigger the reload")
	}
	if !m.rbBusy {
		t.Error("busy should hold until the reload lands")
	}

	// ...and the reload replaces the window with the new entry on top.
	reloaded := append([]remote.Version{ver(4, "older", true, gafferLedger(remote.OpRollback))}, rollbackHistory()...)
	nm, _ = m.Update(historyReloadedMsg{versions: reloaded, total: 5})
	m = asModel(t, nm)
	if m.rbOpen || m.rbBusy {
		t.Errorf("reload should close the modal (open=%v busy=%v)", m.rbOpen, m.rbBusy)
	}
	if m.cursor != 0 || len(m.versions) != 4 || m.versions[0].Kind != kindRollback {
		t.Errorf("cursor=%d rows=%d head=%v, want the new rollback entry selected on top", m.cursor, len(m.versions), m.versions[0].Kind)
	}
	if m.total != 5 {
		t.Errorf("total = %d, want the reload's 5", m.total)
	}
}

func TestHistoryStaleLoadDiscardedAfterReload(t *testing.T) {
	// A page load fired before the rollback reload targets the old window; if it
	// lands after the reload it must be discarded - appending it would punch a
	// hole where the new head entry sits (or falsely exhaust the fresh window).
	m := newTestHistoryModel(rollbackHistory(), 100, 24)
	m.rbOpen, m.rbBusy, m.loading = true, true, true // a load in flight when y fired
	staleGen := m.loadGen

	reloaded := append([]remote.Version{ver(4, "older", true, gafferLedger(remote.OpRollback))}, rollbackHistory()...)
	nm, _ := m.Update(historyReloadedMsg{versions: reloaded, total: 5})
	m = asModel(t, nm)
	fresh := len(m.versions)

	// The stale page lands late, carrying pre-rollback versions.
	nm, _ = m.Update(historyLoadedMsg{versions: []remote.Version{ver(0, "ancient", true, nil)}, gen: staleGen})
	m = asModel(t, nm)
	if len(m.versions) != fresh {
		t.Errorf("stale page appended: %d rows, want the fresh window's %d", len(m.versions), fresh)
	}
	// And a stale EMPTY page must not exhaust the fresh window.
	nm, _ = m.Update(historyLoadedMsg{versions: nil, gen: staleGen})
	m = asModel(t, nm)
	if m.exhausted {
		t.Error("a stale empty page must not mark the fresh window exhausted")
	}
	// A page from the current generation still applies.
	nm, _ = m.Update(historyLoadedMsg{versions: []remote.Version{ver(0, "ancient", true, nil)}, gen: m.loadGen})
	m = asModel(t, nm)
	if len(m.versions) != fresh+1 {
		t.Errorf("a current-generation page should append: %d rows, want %d", len(m.versions), fresh+1)
	}
}

func TestHistoryRollbackModalGuards(t *testing.T) {
	t.Run("esc closes and clears the error", func(t *testing.T) {
		m := newTestHistoryModel(rollbackHistory(), 100, 24)
		m.rbOpen, m.rbErr = true, errors.New("old failure")
		nm, _ := m.handleKey(key("esc"))
		m = asModel(t, nm)
		if m.rbOpen || m.rbErr != nil {
			t.Errorf("open=%v err=%v, want closed and cleared", m.rbOpen, m.rbErr)
		}
	})

	t.Run("y on the deployed content is inert", func(t *testing.T) {
		m := newTestHistoryModel(rollbackHistory(), 100, 24)
		m.rbOpen = true // cursor 0: the deployed head
		nm, cmd := m.handleKey(key("y"))
		if cmd != nil || asModel(t, nm).rbBusy {
			t.Error("nothing should fire for rbCurrent")
		}
		if !strings.Contains(asModel(t, nm).View(), "what's deployed") {
			t.Error("the modal should say the content is already deployed")
		}
	})

	t.Run("scrubbing under the modal moves the selection", func(t *testing.T) {
		m := newTestHistoryModel(rollbackHistory(), 100, 24)
		m.rbOpen = true
		nm, _ := m.handleKey(key("down"))
		if asModel(t, nm).cursor != 1 {
			t.Errorf("cursor = %d, want scrubbed to 1", asModel(t, nm).cursor)
		}
	})

	t.Run("r on an empty timeline is inert", func(t *testing.T) {
		m := newTestHistoryModel(nil, 100, 24)
		nm, _ := m.handleKey(key("r"))
		if asModel(t, nm).rbOpen {
			t.Error("r must not open a modal over an empty timeline")
		}
	})

	t.Run("ctrl+c quits even while busy", func(t *testing.T) {
		m := newTestHistoryModel(rollbackHistory(), 100, 24)
		m.rbOpen, m.rbBusy = true, true
		_, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlC})
		if cmd == nil {
			t.Fatal("ctrl+c should quit")
		}
		if msg := cmd(); msg != tea.Quit() {
			t.Errorf("got %T, want tea.Quit", msg)
		}
	})

	t.Run("a refused target renders the refusal", func(t *testing.T) {
		raw := rollbackHistory()
		raw[2].Definition.EngineVersion = 2
		m := newTestHistoryModel(raw, 100, 24)
		m.rbOpen, m.cursor = true, 2
		if out := m.View(); !strings.Contains(out, "engine version") || !strings.Contains(out, "recreate") {
			t.Errorf("the modal should show the refusal pointing at recreate\n%s", out)
		}
	})

	t.Run("a reload failure keeps the stale timeline and flags it", func(t *testing.T) {
		m := newTestHistoryModel(rollbackHistory(), 100, 24)
		m.rbOpen, m.rbBusy = true, true
		nm, _ := m.Update(historyReloadedMsg{err: errors.New("read failed")})
		m = asModel(t, nm)
		if m.rbOpen || m.rbBusy || m.loadErr == nil || len(m.versions) != 3 {
			t.Errorf("open=%v busy=%v loadErr=%v rows=%d, want closed with the stale window flagged", m.rbOpen, m.rbBusy, m.loadErr, len(m.versions))
		}
	})
}
