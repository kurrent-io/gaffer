package cmd

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// diffModalSize is the modal's outer box: near-fullscreen with a small margin,
// capped so a very wide terminal doesn't stretch code lines across the room,
// and never larger than the terminal itself - an oversized overlay wraps its
// composited rows and corrupts the whole frame.
func (m historyModel) diffModalSize() (w, h int) {
	return min(m.width, max(30, min(m.width-6, 120))), min(m.height, max(8, m.height-4))
}

// diffBodyHeight is how many diff rows fit the modal: the box minus its border
// (2), the title line, the gaps around the body, and the hint line (4).
func (m historyModel) diffBodyHeight() int {
	_, h := m.diffModalSize()
	return max(1, h-2-4)
}

// diffMaxScroll is the largest useful body scroll offset for the selected
// entry, so the page keys clamp instead of accumulating dead offset that the
// render would ignore but pgup would have to unwind.
func (m historyModel) diffMaxScroll() int {
	w, _ := m.diffModalSize()
	body := m.diffModalBody(historyDiffAt(m.versions, m.cursor, m.morePages()), w-4)
	return max(0, len(body)-m.diffBodyHeight())
}

// modalInnerWidth is the content width inside the modal box: the outer width
// minus the border and one cell of padding each side.
func (m historyModel) modalInnerWidth() int {
	w, _ := m.diffModalSize()
	return w - 4
}

// diffModal renders the diff overlay for the selected entry: a bordered box
// with the entry named in the title, the scalar dimensions that moved, and the
// aligned query diff (the same rows gaffer diff prints). The box shrinks to
// its content - a two-line change is a small dialog, not a full screen of
// blank rows - and scrolls when the diff outgrows the screen.
func (m historyModel) diffModal() string {
	innerW := m.modalInnerWidth()
	d := historyDiffAt(m.versions, m.cursor, m.morePages())
	return m.modalFrame(m.diffModalTitle(d, innerW), m.diffModalBody(d, innerW),
		hintBar("↑↓ scrub", "pgup/pgdn scroll", "esc close"))
}

// modalFrame windows the body by the current scroll and wraps title, body, and
// hint in the shared bordered overlay box - one frame for the diff and rollback
// modals so their size, scrolling, and chrome can't drift apart.
func (m historyModel) modalFrame(title string, body []string, hint string) string {
	w, _ := m.diffModalSize()
	innerW := m.modalInnerWidth()

	visible := min(len(body), m.diffBodyHeight())
	from := min(m.diffScroll, len(body)-visible)
	window := body[from : from+visible]
	if len(body) > visible {
		hint = fmt.Sprintf("%d-%d of %d", from+1, from+visible, len(body)) + dotSep + hint
	}

	content := title + "\n\n" + strings.Join(window, "\n") + "\n\n" +
		m.hs.fieldKey.Render(truncate(hint, innerW))
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(m.hs.rule.GetForeground()).
		Padding(0, 1).
		Width(w - 2).
		Render(content)
}

// diffModalTitle names the entry the modal is showing - glyph and operation in
// the entry's own colours, then its hash, the baseline it's diffed against,
// and the time, dimmed.
func (m historyModel) diffModalTitle(d historyDiff, width int) string {
	hv := d.sel
	lead := m.tw.historyRunStyle(hv).Render(historyGlyph(hv)) + " " +
		m.tw.historyKindStyle(hv).Render(hv.eventLabel())
	var parts []string
	if !hv.StateChange() && hv.Hash != "" {
		parts = append(parts, hv.Hash)
	}
	switch {
	case d.base != nil:
		parts = append(parts, "vs "+d.base.Hash)
	case d.state == diffReady:
		parts = append(parts, "vs nothing (first version)")
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

// diffModalBody is the modal's scrollable content for the entry's diff state:
// the query diff rows (with any scalar changes leading them), or the message
// for an entry with nothing to diff.
func (m historyModel) diffModalBody(d historyDiff, width int) []string {
	switch d.state {
	case diffNoChange:
		return []string{m.tw.styles.muted.Render(
			truncate("no definition change"+dotSep+d.sel.eventLabel(), width))}
	case diffBaselineUnloaded:
		if m.loadErr != nil {
			return []string{m.tw.warnLine("couldn't load older entries to find the previous version - ↑/↓ retries", width)}
		}
		return []string{m.tw.styles.muted.Render(truncate("loading older entries to find the previous version…", width))}
	}
	var rows []string
	for _, sc := range scalarChanges(d) {
		rows = append(rows, m.tw.styles.warning.Render(truncate(sc, width)))
	}
	if len(rows) > 0 {
		rows = append(rows, "")
	}
	for _, row := range m.tw.queryDiffRows(d.lines) {
		rows = append(rows, ansi.Truncate(row, width, "…"))
	}
	return rows
}

// scalarChanges names the non-query dimensions that moved between the baseline
// and the entry, with their values - "engine version 1 → 2".
func scalarChanges(d historyDiff) []string {
	if d.base == nil {
		return nil
	}
	sel, base := d.sel.Definition.Descriptor(), d.base.Definition.Descriptor()
	var out []string
	if d.cmp.EngineVersionDiffers {
		out = append(out, fmt.Sprintf("engine version %d → %d", base.EngineVersion, sel.EngineVersion))
	}
	if d.cmp.EmitDiffers {
		out = append(out, fmt.Sprintf("emit %s → %s", enabledStr(base.Emit), enabledStr(sel.Emit)))
	}
	if d.cmp.TrackEmittedStreamsDiffers {
		out = append(out, fmt.Sprintf("track emitted streams %s → %s", enabledStr(base.TrackEmittedStreams), enabledStr(sel.TrackEmittedStreams)))
	}
	return out
}
