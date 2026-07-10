package cmd

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/kurrent-io/gaffer/cli/internal/deploy"
	"github.com/kurrent-io/gaffer/cli/internal/remote"
)

// rollbackState is what the rollback modal can show for the selected entry.
type rollbackState int

const (
	// rbReady: the selected content can be applied over the deployed one.
	rbReady rollbackState = iota
	// rbCurrent: the selected content is what's deployed - nothing to roll back.
	rbCurrent
	// rbNoTarget: a state change (or the degenerate no-content-in-view case) -
	// there's no content of its own to roll back to.
	rbNoTarget
	// rbRefused: the selection differs from the deployed projection in a
	// create-only dimension, which an in-place rollback can't change.
	rbRefused
)

// historyRollback is a rollback proposal for the modal: the selected target,
// the deployed content it would replace, and the change between them.
type historyRollback struct {
	state rollbackState
	sel   historyVersion
	cur   *historyVersion // the newest content version in view - what's deployed
	cmp   deploy.Comparison
	lines []deploy.DiffLine
}

// historyRollbackAt computes the rollback proposal for entry i of a classified,
// newest-first history. The deployed content is the newest content version in
// view - the stream's head is what the server is running - so the diff reads
// current -> selected: what applying the rollback would change.
func historyRollbackAt(versions []historyVersion, i int) historyRollback {
	sel := versions[i]
	if sel.StateChange() || sel.Definition == nil {
		return historyRollback{state: rbNoTarget, sel: sel}
	}
	var cur *historyVersion
	for j := range versions {
		if !versions[j].StateChange() && versions[j].Definition != nil {
			cur = &versions[j]
			break
		}
	}
	if cur == nil {
		return historyRollback{state: rbNoTarget, sel: sel}
	}
	if cur.ContentHash == sel.ContentHash {
		return historyRollback{state: rbCurrent, sel: sel, cur: cur}
	}
	cmp := deploy.Compare(sel.Definition.Descriptor(), cur.Definition.Descriptor())
	rb := historyRollback{
		state: rbReady,
		sel:   sel,
		cur:   cur,
		cmp:   cmp,
		lines: deploy.LineDiff(cur.Definition.Query, sel.Definition.Query),
	}
	if cmp.EngineVersionDiffers || cmp.TrackEmittedStreamsDiffers {
		rb.state = rbRefused
	}
	return rb
}

// rollbackAppliedMsg reports the Update the modal fired: the rollback landed,
// or the error to show for a retry.
type rollbackAppliedMsg struct {
	err error
}

// historyReloadedMsg carries a fresh first page after an applied rollback, so
// the timeline restarts at the head with the new entry on top.
type historyReloadedMsg struct {
	versions []remote.Version
	total    int64
	err      error
}

// applyRollback fires the in-place update for the selected target, stamped with
// the rollback ledger the TUI was started with.
func (m *historyModel) applyRollback(target historyVersion) tea.Cmd {
	client, name, base := m.client, m.name, m.baseCtx
	ledger := m.ledger
	query, emit := target.Definition.Query, target.Definition.Emit
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(base, projectionRPCTimeout)
		defer cancel()
		return rollbackAppliedMsg{err: client.Update(ctx, name, query, remote.UpdateOptions{Emit: &emit, Ledger: &ledger})}
	}
}

// reloadHistory re-reads the first page after an applied rollback, replacing
// the loaded window so the new rollback entry appears at the head.
func (m *historyModel) reloadHistory() tea.Cmd {
	client, name, base := m.client, m.name, m.baseCtx
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(base, projectionRPCTimeout)
		defer cancel()
		vs, total, err := client.ReadHistory(ctx, name, -1, historyPageSize)
		return historyReloadedMsg{versions: vs, total: total, err: err}
	}
}

// handleRollbackKey is the rollback modal's key map: y applies (when the
// proposal is applyable), the arrows scrub the target underneath like the diff
// modal, page keys scroll, and esc/r/q cancel. While the update is in flight
// only ctrl+c is live, so a second y can't double-fire.
func (m historyModel) handleRollbackKey(key string) (tea.Model, tea.Cmd) {
	if key == "ctrl+c" {
		return m, tea.Quit
	}
	if m.rbBusy {
		return m, nil
	}
	switch key {
	case "q", "esc", "r":
		m.rbOpen = false
		m.rbErr = nil
		m.diffScroll = 0
		return m, nil
	case "y", "enter":
		rb := historyRollbackAt(m.versions, m.cursor)
		if rb.state != rbReady {
			return m, nil
		}
		m.rbBusy = true
		m.rbErr = nil
		cmd := m.applyRollback(rb.sel)
		return m, cmd
	case "pgup":
		m.diffScroll = max(0, m.diffScroll-m.diffBodyHeight())
		return m, nil
	case "pgdown":
		body := m.rollbackModalBody(historyRollbackAt(m.versions, m.cursor), m.modalInnerWidth())
		m.diffScroll = min(m.diffScroll+m.diffBodyHeight(), max(0, len(body)-m.diffBodyHeight()))
		return m, nil
	}
	if m.moveCursor(key) {
		m.diffScroll = 0
		m.rbErr = nil
		m.top = clampTop(m.top, m.cursor, m.visibleVersions())
		cmd := m.loadPage(false)
		return m, cmd
	}
	return m, nil
}

// rollbackModal renders the confirm overlay: the proposal named in the title,
// the current -> selected diff with any emit flip, the state and drift
// cautions, and the confirm hint.
func (m historyModel) rollbackModal() string {
	innerW := m.modalInnerWidth()
	rb := historyRollbackAt(m.versions, m.cursor)
	hint := hintBar("y confirm", "↑↓ scrub", "esc cancel")
	switch {
	case m.rbBusy:
		hint = "rolling back…"
	case rb.state != rbReady:
		hint = hintBar("↑↓ scrub", "esc close")
	}
	return m.modalFrame(m.rollbackModalTitle(rb, innerW), m.rollbackModalBody(rb, innerW), hint)
}

// rollbackModalTitle names the proposal: the target entry in its own colours,
// then its hash, the deployed hash it replaces, and the target's time, dimmed.
func (m historyModel) rollbackModalTitle(rb historyRollback, width int) string {
	hv := rb.sel
	lead := m.tw.historyRunStyle(hv).Render(historyGlyph(hv)) + " " +
		m.tw.styles.label.Render("roll back to")
	var parts []string
	if !hv.StateChange() && hv.Hash != "" {
		parts = append(parts, hv.Hash)
	}
	if rb.cur != nil && rb.state != rbCurrent {
		parts = append(parts, "replaces "+rb.cur.Hash)
	}
	if hv.Definition != nil && !hv.Definition.Time.IsZero() {
		parts = append(parts, hv.Definition.Time.Format("2006-01-02 15:04"))
	}
	tail := ""
	if len(parts) > 0 {
		tail = m.hs.fieldKey.Render(dotSep + strings.Join(parts, dotSep))
	}
	return ansi.Truncate(lead+tail, width, "…")
}

// rollbackModalBody is the modal's scrollable content: the diff the rollback
// would apply plus the cautions, or the message for a state that can't apply.
func (m historyModel) rollbackModalBody(rb historyRollback, width int) []string {
	switch rb.state {
	case rbNoTarget:
		return []string{m.tw.styles.muted.Render(
			truncate("a state change has no content of its own - select a content version", width))}
	case rbCurrent:
		return []string{m.tw.styles.muted.Render(
			truncate("this content is what's deployed - nothing to roll back", width))}
	case rbRefused:
		// The refusal names the dimension and the recreate escape; wrap it
		// rather than truncating the actionable tail off.
		var rows []string
		// Glyph before the wrap so it leads the first line and the wrapping accounts
		// for its width.
		msg := glyphWarning + " " + remote.RollbackRefusal(rb.cmp, rb.sel.ContentHash, m.name).Error()
		for line := range strings.SplitSeq(lipgloss.NewStyle().Width(width).Render(msg), "\n") {
			rows = append(rows, m.tw.styles.warning.Render(line))
		}
		return rows
	}
	var rows []string
	if m.prod {
		// The same louder-against-production confirm gaffer rollback gives:
		// the tier is resolved once at TUI start, from the shared identity.
		rows = append(rows,
			m.tw.warnLine("rolls back on "+prodWhere(m.target, true), width),
			"")
	}
	if rb.cmp.EmitDiffers {
		rows = append(rows,
			m.tw.styles.warning.Render(truncate(fieldChange("emit", enabledStr(rb.cur.Definition.Emit), enabledStr(rb.sel.Definition.Emit)), width)),
			"")
	}
	for _, row := range m.tw.queryDiffRows(rb.lines) {
		rows = append(rows, ansi.Truncate(row, width, "…"))
	}
	rows = append(rows, "",
		m.tw.warnLine("code rolls back, state does not (gaffer recreate rebuilds from zero)", width),
		m.tw.warnLine("local files stay untouched; gaffer diff will show this as drift", width))
	if m.rbErr != nil {
		rows = append(rows, "",
			m.tw.warnLine("rollback failed: "+m.rbErr.Error()+" - y retries", width))
	}
	return rows
}
