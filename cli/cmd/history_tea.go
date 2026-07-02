package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/spf13/cobra"

	"github.com/kurrent-io/gaffer/cli/internal/remote"
)

// historyPageSize is how many older versions the picker fetches per scroll-driven
// load. The first page is read before the program starts; reaching the bottom of
// what's loaded pulls the next page in the background.
const historyPageSize = 50

// historyLoadThreshold triggers the next page when the cursor moves within this
// many rows of the oldest loaded version, so scrolling stays ahead of the read.
const historyLoadThreshold = 10

// historyLoadedMsg carries a fetched page (older versions) back into the model.
type historyLoadedMsg struct {
	versions []remote.Version
	err      error
}

// historyModel is the interactive timeline: a scrolling list of versions on the
// left, the selected version's full detail on the right, and a footer naming the
// projection, controls, and target. It pages older versions in as the cursor
// nears the bottom of what's loaded.
type historyModel struct {
	name      string
	envLabel  string
	connLabel string
	tw        *textWriter   // shared ANSI-palette styles for the timeline
	hs        historyStyles // charm-palette chrome for the footer bar and detail banner

	client  *remote.Client
	baseCtx context.Context //nolint:containedctx // the picker outlives a single RPC; each page derives a bounded ctx from this
	total   int64

	raw       []remote.Version // accumulated, newest-first
	versions  []historyVersion // classified view of raw
	graph     historyGraph     // revert-link lane layout, recomputed when versions change
	ow        int              // cached operation-column width, recomputed when versions change
	loading   bool
	exhausted bool // a paged read came back empty, so the whole stream is loaded
	loadErr   error

	cursor int
	top    int // index of the first visible row
	width  int
	height int

	diffOpen   bool // the diff modal is up, showing the selected entry's change
	diffScroll int  // the modal body's scroll offset
}

// historyStyles is the charm-palette chrome around the timeline: the footer status
// bar's pills and the detail panel's banner. These use fixed charmtone colours
// (the palette fang and the lipgloss examples use) rather than the terminal's ANSI
// palette, so the bar reads as a deliberate UI surface; the timeline itself keeps
// the theme-aware ANSI styles.
type historyStyles struct {
	bar       lipgloss.Style // footer bar background + base text
	tag       lipgloss.Style // the HISTORY pill
	barName   lipgloss.Style // projection name on the bar
	barDim    lipgloss.Style // controls / position on the bar
	badgeEnv  lipgloss.Style // environment pill
	badgeConn lipgloss.Style // connection pill
	badgeWarn lipgloss.Style // load-failure pill

	// Detail panel: a foreground-coloured title keyed to the version's nature
	// (accent for a deploy, warning for out-of-band, muted for lifecycle), over a
	// thin trailing rule - no solid banner, no vertical divider, the way crush keeps
	// panes separated by whitespace alone.
	titleAccent lipgloss.Style
	titleWarn   lipgloss.Style
	titleMuted  lipgloss.Style
	rule        lipgloss.Style // thin trailing rule after the detail title
	fieldKey    lipgloss.Style // detail field label (dim)
	fieldVal    lipgloss.Style // detail field value
	fieldHash   lipgloss.Style // detail content hash (the identity, highlighted)
}

func newHistoryStyles(w io.Writer) historyStyles {
	r := lipgloss.NewRenderer(w)
	const (
		pepper   = lipgloss.Color("#201F26") // near-black
		charcoal = lipgloss.Color("#3A3943") // bar background
		iron     = lipgloss.Color("#4D4C57") // subtle pill
		teal     = lipgloss.Color("#0ADCD9") // Turtle - gaffer accent
		purple   = lipgloss.Color("#6B50FF") // Charple
		coral    = lipgloss.Color("#FF577D") // warning
		mint     = lipgloss.Color("#68FFD6") // Bok - highlight
		squid    = lipgloss.Color("#858392") // dim
		smoke    = lipgloss.Color("#BFBCC8") // light
		salt     = lipgloss.Color("#F1EFEF") // near-white
		ash      = lipgloss.Color("#DFDBDD") // body text
	)
	bar := r.NewStyle().Background(charcoal).Foreground(smoke)
	pill := r.NewStyle().Padding(0, 1).Bold(true)
	return historyStyles{
		bar:       bar,
		tag:       pill.Background(teal).Foreground(pepper),
		barName:   bar.Foreground(salt).Bold(true),
		barDim:    bar.Foreground(squid),
		badgeEnv:  pill.Background(purple).Foreground(salt),
		badgeConn: r.NewStyle().Padding(0, 1).Background(iron).Foreground(smoke),
		badgeWarn: pill.Background(coral).Foreground(salt),

		titleAccent: r.NewStyle().Foreground(teal).Bold(true),
		titleWarn:   r.NewStyle().Foreground(coral).Bold(true),
		titleMuted:  r.NewStyle().Foreground(squid).Bold(true),
		rule:        r.NewStyle().Foreground(iron),
		fieldKey:    r.NewStyle().Foreground(squid),
		fieldVal:    r.NewStyle().Foreground(ash),
		fieldHash:   r.NewStyle().Foreground(mint),
	}
}

func (m historyModel) Init() tea.Cmd { return nil }

func (m historyModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case tea.KeyMsg:
		return m.handleKey(msg)
	case historyLoadedMsg:
		m.loading = false
		if msg.err != nil {
			m.loadErr = msg.err
			return m, nil
		}
		m.loadErr = nil // the read succeeded (empty page included); a stale failure shouldn't keep reporting
		if len(msg.versions) == 0 {
			// A window below the oldest loaded version held no further state events,
			// so there's nothing more to page; stop, or every keypress re-fetches it.
			m.exhausted = true
			return m, nil
		}
		// Recollapse over the full raw slice: a recreate at the old page boundary
		// folds its bookends as they arrive. Folding only ever consumes rows from
		// the newly-loaded older tail (a create is newer than its bookends, and an
		// adjacent loaded bookend folds immediately), so no displayed row vanishes
		// and the cursor index stays on the same row.
		m.raw = append(m.raw, msg.versions...)
		m.versions = collapseHistory(classifyHistory(m.raw))
		m.graph = computeHistoryGraph(m.versions)
		m.ow = operationWidth(m.versions)
		if m.diffOpen {
			// The modal may still be waiting on its baseline; keep paging until
			// it's in view or the stream is exhausted.
			cmd := m.ensureBaseline()
			return m, cmd
		}
		return m, nil
	default:
		return m, nil
	}
}

func (m historyModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if m.diffOpen {
		return m.handleDiffKey(key)
	}
	switch key {
	case "q", "esc", "ctrl+c":
		return m, tea.Quit
	case "d":
		if len(m.versions) == 0 {
			return m, nil
		}
		m.diffOpen = true
		m.diffScroll = 0
		// The baseline may sit on an unloaded page; fetch it now rather than
		// leaving the modal on its loading message until the cursor nears the
		// bottom.
		cmd := m.ensureBaseline()
		return m, cmd
	case "pgup":
		m.cursor = max(0, m.cursor-m.visibleVersions())
	case "pgdown":
		m.cursor = min(len(m.versions)-1, m.cursor+m.visibleVersions())
	default:
		m.moveCursor(key)
	}
	m.top = clampTop(m.top, m.cursor, m.visibleVersions())
	// Sequence the call before the return: loadPage mutates m (loading) through
	// its pointer receiver, and a bare `return m, m.loadPage(...)` would copy m
	// into the result before that mutation lands, dropping the loading flag.
	cmd := m.loadPage(false)
	return m, cmd
}

// handleDiffKey is the modal's key map: the arrows scrub the timeline selection
// underneath (the diff re-renders for each entry), page keys scroll the diff
// body, and esc/d/q close back to the timeline at the same position.
func (m historyModel) handleDiffKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "ctrl+c":
		return m, tea.Quit
	case "q", "esc", "d":
		m.diffOpen = false
		m.diffScroll = 0
		return m, nil
	case "pgup":
		m.diffScroll = max(0, m.diffScroll-m.diffBodyHeight())
		return m, nil
	case "pgdown":
		// Clamped to the last useful offset, or repeated pgdowns would bank
		// dead offset that pgup has to unwind before anything moves on screen.
		m.diffScroll = min(m.diffScroll+m.diffBodyHeight(), m.diffMaxScroll())
		return m, nil
	}
	if m.moveCursor(key) {
		m.diffScroll = 0
		m.top = clampTop(m.top, m.cursor, m.visibleVersions())
		cmd := m.ensureBaseline()
		return m, cmd
	}
	return m, nil
}

// moveCursor applies a single-step or jump movement key to the selection,
// reporting whether the key was a movement key - the one place the scrub key
// set is defined, so the timeline and the modal can't drift apart.
func (m *historyModel) moveCursor(key string) bool {
	switch key {
	case "up", "k":
		m.cursor = max(0, m.cursor-1)
	case "down", "j":
		m.cursor = min(len(m.versions)-1, m.cursor+1)
	case "g", "home":
		m.cursor = 0
	case "G", "end":
		m.cursor = max(0, len(m.versions)-1)
	default:
		return false
	}
	return true
}

// ensureBaseline starts a page load when the selected entry's diff baseline is
// on an unloaded page, regardless of where the cursor sits; alongside it the
// ordinary near-the-bottom paging still applies.
func (m *historyModel) ensureBaseline() tea.Cmd {
	if historyDiffAt(m.versions, m.cursor, m.morePages()).state == diffBaselineUnloaded {
		return m.loadPage(true)
	}
	return m.loadPage(false)
}

// clampTop scrolls the visible window the least it must to keep the cursor in
// view, so the timeline holds its position rather than re-anchoring each frame.
func clampTop(top, cursor, height int) int {
	if height <= 0 {
		return 0
	}
	if cursor < top {
		return cursor
	}
	if cursor >= top+height {
		return cursor - height + 1
	}
	return top
}

// morePages reports whether older history exists beyond the loaded window, so
// a missing diff baseline means "not loaded yet" rather than "first version".
func (m historyModel) morePages() bool {
	return !m.exhausted && len(m.raw) > 0 && m.raw[len(m.raw)-1].Number > 0
}

// loadPage fetches the next older page when the cursor nears the bottom of
// what's loaded and older versions remain; force skips the near-the-bottom
// check, for the diff modal chasing a baseline on an unloaded page. The oldest
// loaded version's number being zero means the whole stream is in view, so
// there's nothing more to pull.
func (m *historyModel) loadPage(force bool) tea.Cmd {
	if m.loading || m.exhausted || len(m.raw) == 0 {
		return nil
	}
	oldest := m.raw[len(m.raw)-1].Number
	if oldest <= 0 || (!force && m.cursor < len(m.versions)-historyLoadThreshold) {
		return nil
	}
	m.loading = true
	client, name, base := m.client, m.name, m.baseCtx
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(base, projectionRPCTimeout)
		defer cancel()
		vs, _, err := client.ReadHistory(ctx, name, oldest, historyPageSize)
		return historyLoadedMsg{versions: vs, err: err}
	}
}

// historyTopPad is the blank space above the panes, so content has room to breathe
// rather than crowding the top edge.
const historyTopPad = 1

func (m historyModel) bodyHeight() int {
	// The footer takes the last line and the top pad the first few; the rest is the
	// timeline / detail panes.
	if h := m.height - 1 - historyTopPad; h > 0 {
		return h
	}
	return 1
}

func (m historyModel) View() string {
	if m.width < 10 || m.height < 6 || len(m.versions) == 0 {
		return ""
	}
	body := m.bodyHeight()
	leftW, rightW := m.paneWidths()

	panes := lipgloss.NewStyle().Width(leftW).Height(body).MaxHeight(body).Render(m.timeline(leftW, body))
	if rightW > 0 {
		// No divider between the panes - the sidebar's left padding holds the gap,
		// the way crush separates regions by whitespace alone.
		hv := m.versions[m.cursor]
		sidebar := lipgloss.NewStyle().Width(rightW).Height(body).MaxHeight(body).
			PaddingLeft(4).Render(m.detail(hv, max(1, rightW-4)))
		panes = lipgloss.JoinHorizontal(lipgloss.Top, panes, sidebar)
	}
	// A blank top line, then the panes, then the footer - no rule between them.
	rows := make([]string, 0, historyTopPad+2)
	for range historyTopPad {
		rows = append(rows, "")
	}
	rows = append(rows, panes, m.footer())
	view := lipgloss.JoinVertical(lipgloss.Left, rows...)
	if m.diffOpen {
		// The timeline stays visible around the modal (crush-style, no scrim);
		// the app footer below it keeps the projection and position in view.
		view = centerOverlay(m.diffModal(), view, m.width, m.height)
	}
	return view
}

// paneWidths splits the row into the timeline and the detail panel, separated by
// the panel's own left padding rather than a divider. The panel is the priority: it
// holds its comfortable width and the timeline degrades in the remainder (see
// rowStage). Only once the timeline is down to a column of blobs does the panel
// start crushing below its comfortable width.
func (m historyModel) paneWidths() (left, right int) {
	const (
		panelMax = 46 // enough for every field; extra width goes to the timeline
		blobsMin = 3  // the timeline's floor: cursor gutter plus a single glyph
	)
	// While the timeline can show more than blobs, the panel keeps its comfortable
	// width and the timeline takes the rest.
	if tl := m.width - panelMax; tl >= blobsMin && m.stageFor(tl) > stageBlobs {
		return tl, panelMax
	}
	// The timeline is down to a column of blobs: give it just that column and let the
	// panel expand into all the leftover width rather than leaving a dead gap.
	left = min(blobsMin, m.width)
	return left, m.width - left
}

// gp is the graph painter for the current versions and their revert-link layout.
func (m historyModel) gp() graphPainter {
	return newGraphPainter(m.tw, m.versions, m.graph)
}

// rowStage is how much of a timeline row fits the pane width, degrading as space
// runs out: the panel is kept whole and the timeline sheds pieces in this order -
// the provenance second row (with the date), the content hash, the revert graph
// indent, and finally the operation name, leaving a bare column of run-state blobs.
type rowStage int

const (
	stageBlobs   rowStage = iota // glyph only
	stageOp                      // glyph + operation (graph dropped)
	stageGraphOp                 // graph + operation (hash dropped)
	stageHash                    // graph + hash + operation, one line (date dropped)
	stageFull                    // + date + provenance second row
)

// stageFor picks the row stage for a timeline pane of the given width.
func (m historyModel) stageFor(left int) rowStage {
	rowW := left - 2 // cursor gutter
	gw := m.graph.gutterWidth()
	switch {
	case rowW >= gw+m.ow+28: // + "  " + date(16)
		return stageFull
	case rowW >= gw+m.ow+10: // gutter + " " + hash(7) + "  " + op
		return stageHash
	case rowW >= gw+m.ow+1: // gutter + " " + op
		return stageGraphOp
	case rowW >= m.ow+2: // glyph + " " + op
		return stageOp
	default:
		return stageBlobs
	}
}

// visibleVersions is roughly how many versions fit the timeline pane, for paging: a
// full (two-line) entry takes three lines, a graph entry two (node + rail), a flat
// entry one.
func (m historyModel) visibleVersions() int {
	left, _ := m.paneWidths()
	per := 1
	switch m.stageFor(left) {
	case stageFull:
		per = 3
	case stageHash, stageGraphOp:
		per = 2
	}
	return max(1, m.bodyHeight()/per)
}

func (m historyModel) timeline(width, height int) string {
	rowW := max(1, width-2) // leading cursor gutter
	stage := m.stageFor(width)
	per := 1
	switch stage {
	case stageFull:
		per = 3
	case stageHash, stageGraphOp:
		per = 2
	}
	// m.top is kept in step with the cursor by clampTop on each key; re-clamp here
	// only as a defence against a resize that shrank the window since.
	top := clampTop(m.top, m.cursor, max(1, height/per))
	gp := m.gp()
	var lines []string
	emit := func(s string) { lines = append(lines, s) }
	for i := top; i < len(m.versions) && len(lines) < height; i++ {
		sel := i == m.cursor
		last := i == len(m.versions)-1
		emit(m.nodeLine(i, sel, stage, rowW))
		if stage <= stageOp {
			continue // flat: one line per entry, the graph and its rails are dropped
		}
		// Below the node: the live spine and bridge rails, plus any fork or rejoin
		// connector. The cursor bar sits on the node line only, so these lead with a
		// blank two-cell gutter. The provenance text only rides these on the full stage.
		cons := gp.connectors(i)
		if text, warn := historyProvenanceText(m.versions[i]); stage == stageFull && text != "" {
			emit(m.railProvenance(i, text, warn, sel, rowW))
			for _, c := range cons {
				emit("  " + c)
			}
			if !last && len(cons) == 0 {
				emit("  " + gp.railGutter(i)) // spacer so the next node has breathing room
			}
			continue
		}
		switch {
		case len(cons) > 0:
			for _, c := range cons {
				emit("  " + c)
			}
		case !last:
			emit("  " + gp.railGutter(i))
		}
	}
	if len(lines) > height {
		lines = lines[:height]
	}
	return strings.Join(lines, "\n")
}

// nodeLine is an entry's primary timeline line, rendered for the current stage. From
// the fullest: cursor gutter, the graph field (rails + run-state glyph), the content
// hash (blank for a state change), the operation, and the date. As the pane narrows
// the date and hash drop, then the graph collapses to a bare glyph, then the
// operation, leaving just the run-state blob.
func (m historyModel) nodeLine(i int, sel bool, stage rowStage, width int) string {
	hv := m.versions[i]
	glyph := m.tw.historyRunStyle(hv).Render(historyGlyph(hv))
	op := func(w int) string { return m.tw.historyKindStyle(hv).Render(truncate(hv.eventLabel(), w)) }

	// Flat stages drop the graph entirely: a glyph, optionally the operation.
	if stage <= stageOp {
		if stage == stageBlobs {
			return m.gutter(sel) + glyph
		}
		return m.gutter(sel) + glyph + " " + op(max(1, width-3))
	}

	gp := m.gp()
	field := gp.node(i) // graph gutter: shallower rails plus the run-state glyph
	pad := gp.nodePad(i)
	opCell := m.tw.historyKindStyle(hv).Render(padCells(truncate(hv.eventLabel(), m.ow), m.ow))
	if stage == stageGraphOp {
		return m.gutter(sel) + field + " " + pad + opCell
	}

	hashStyle := m.tw.styles.dim
	if sel {
		hashStyle = m.tw.styles.label
	}
	hash := strings.Repeat(" ", historyHashWidth)
	if !hv.stateChange() {
		hash = hashStyle.Render(padCells(hv.Hash, historyHashWidth))
	}
	if stage == stageHash {
		return m.gutter(sel) + fmt.Sprintf("%s %s%s  %s", field, hash, pad, opCell)
	}

	// stageFull: also the date.
	when := ""
	if hv.Definition != nil && !hv.Definition.Time.IsZero() {
		when = hv.Definition.Time.Format("2006-01-02 15:04")
	}
	whenStyle := m.tw.styles.dim
	if sel {
		whenStyle = m.tw.styles.emitted
	}
	return m.gutter(sel) + fmt.Sprintf("%s %s%s  %s  %s", field, hash, pad, opCell, whenStyle.Render(when))
}

// railProvenance is the rail line carrying a version's attribution: the rail in
// the glyph column (so the nodes read as a timeline) then the text, aligned under
// the content-hash column. The cursor bar sits on the node line only, so the rail
// gutter is always blank. The text dims off the selected row, and the out-of-band
// caution is always in warning colour.
func (m historyModel) railProvenance(i int, text string, warn, sel bool, width int) string {
	style := m.tw.styles.dim
	switch {
	case warn:
		style = m.tw.styles.warning
	case sel:
		style = m.tw.styles.emitted
	}
	// Align under the operation column: cursor gutter (2) + graph gutter + hash + gap.
	gw := m.graph.gutterWidth()
	indent := strings.Repeat(" ", historyHashWidth+3)
	avail := max(1, width-gw-(historyHashWidth+3))
	return "  " + m.gp().railGutter(i) + indent + style.Render(truncate(text, avail))
}

// gutter is the two-cell left margin on the node line: a bright bar on the
// selected version, blank otherwise.
func (m historyModel) gutter(sel bool) string {
	if sel {
		return m.tw.styles.label.Render("▌") + " "
	}
	return "  "
}

// detail is the right panel's content, in two sections: the event itself (when,
// state, and - for a content version - its content), then the metadata of the
// deployed version it relates to. The split makes clear that a state change's
// metadata comes from the deploy it toggles, not from the state change itself.
func (m historyModel) detail(hv historyVersion, width int) string {
	const labelW = 11 // widest label ("operation") + a gap
	var b strings.Builder

	field := func(label, value string) {
		if value == "" {
			return
		}
		b.WriteString(m.hs.fieldKey.Render(padCells(label, labelW)))
		b.WriteString(m.hs.fieldVal.Render(truncate(value, max(1, width-labelW))))
		b.WriteByte('\n')
	}
	header := func(text string, style lipgloss.Style) {
		plain := truncate(text, width)
		b.WriteString(style.Render(plain))
		if gap := width - lipgloss.Width(plain) - 1; gap > 0 {
			b.WriteString(" " + m.hs.rule.Render(strings.Repeat("─", gap)))
		}
		b.WriteString("\n\n")
	}

	// Section 1: the event. Title coloured by kind, then when / state, plus the
	// content and revert flag for a content version (a state change has no content
	// of its own - that lands in the version-metadata section).
	header(fmt.Sprintf("%s %s", historyGlyph(hv), hv.eventLabel()), m.detailTitle(hv))
	if hv.Definition != nil && !hv.Definition.Time.IsZero() {
		field("when", hv.Definition.Time.Format("2006-01-02 15:04"))
	}
	if !hv.Deleted { // a tombstone's title already says "deleted"
		state := "disabled"
		if hv.enabled() {
			state = "enabled"
		}
		field("state", state)
	}
	if !hv.stateChange() && hv.Hash != "" {
		b.WriteString(m.hs.fieldKey.Render(padCells("content", labelW)))
		b.WriteString(m.hs.fieldHash.Render(hv.Hash))
		b.WriteByte('\n')
		if m.matchesEarlier(m.cursor) {
			b.WriteString(strings.Repeat(" ", labelW) + m.tw.styles.dim.Render("↩ matches an earlier deploy"))
			b.WriteByte('\n')
		}
	}
	if len(hv.Absorbed) > 0 {
		step := " step"
		if len(hv.Absorbed) > 1 {
			step = " steps"
		}
		b.WriteString(m.tw.styles.dim.Render(truncate("⟳ reprocessed from zero", width)) + "\n")
		b.WriteString(m.tw.styles.dim.Render(truncate("  folds the "+absorbedSummary(hv.Absorbed)+step, width)) + "\n")
	}
	switch hv.Kind {
	case kindEditedExternally:
		b.WriteString(m.tw.styles.warning.Render(truncate("⚠ "+changeSummary(hv.Change)+" outside gaffer", width)) + "\n")
	case kindUnreadable:
		b.WriteString(m.tw.styles.warning.Render(truncate("⚠ deploy metadata could not be read", width)) + "\n")
	}

	// Section 2: the deployed version this entry relates to - itself for a content
	// version, the governing deploy for a state change.
	if gv := m.governingContent(m.cursor); gv != nil {
		b.WriteByte('\n')
		header("version metadata", m.hs.fieldKey)
		field("hash", gv.Hash)
		if gv.Ledger != nil {
			via := gv.Ledger.Tool
			if gv.Ledger.ToolVersion != "" {
				via += " " + gv.Ledger.ToolVersion
			}
			field("tool", via)
			field("actor", gv.Ledger.Actor)
		}
		field("operation", gv.operationLabel())
		if gv.Ledger != nil {
			field("source", shortRevision(gv.Ledger.Revision))
		}
	}

	// Section 3 (reconfigured only): the checkpoint/perf knobs that moved.
	if len(hv.ConfigChanges) > 0 {
		cw := 0
		for _, cc := range hv.ConfigChanges {
			cw = max(cw, len(cc.Label))
		}
		b.WriteByte('\n')
		header("configuration", m.hs.fieldKey)
		for _, cc := range hv.ConfigChanges {
			b.WriteString(m.hs.fieldKey.Render(padCells(cc.Label, cw+2)))
			b.WriteString(m.hs.fieldVal.Render(truncate(cc.From+" → "+cc.To, max(1, width-cw-2))))
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// detailTitle is the panel title's style, coloured by what the version is: accent
// for gaffer's own deploys, warning for an out-of-band change or a delete, muted
// for a lifecycle event.
func (m historyModel) detailTitle(hv historyVersion) lipgloss.Style {
	switch hv.Kind {
	case kindEditedExternally, kindChangedByTool, kindDeleted, kindUnreadable:
		return m.hs.titleWarn
	case kindDeploy, kindRollback, kindReset, kindRecreate:
		return m.hs.titleAccent
	default:
		return m.hs.titleMuted
	}
}

// governingContent returns the content version the entry at i relates to: itself
// when it's a content version, else the nearest older content version - the deploy
// a state change (enable / disable / delete / config) toggles. nil when none is in
// view (a history of only state changes).
func (m historyModel) governingContent(i int) *historyVersion {
	for j := i; j < len(m.versions); j++ {
		if !m.versions[j].stateChange() {
			return &m.versions[j]
		}
	}
	return nil
}

// matchesEarlier reports whether an older entry shares i's content hash - i.e.
// this content was deployed before, so the entry is a revert or rollback. Matching
// only looks backward in time: a version is derived from an older one, never a
// newer one, so the original occurrence of a content is not a match.
func (m historyModel) matchesEarlier(i int) bool {
	for j := i + 1; j < len(m.versions); j++ {
		if !m.versions[j].stateChange() && m.versions[j].contentKey != "" && m.versions[j].contentKey == m.versions[i].contentKey {
			return true
		}
	}
	return false
}

// footer is the status bar: a HISTORY pill and the projection / position on the
// left, the controls in the middle, and the environment and target as pills on the
// right - all on a continuous bar background. Segments are sized by rendered width
// and the controls yield first as the terminal narrows, so the bar fills exactly
// the width and never wraps (which would throw off the height the panes size to).
func (m historyModel) footer() string {
	left := m.hs.tag.Render("HISTORY") + m.hs.barName.Render(" "+m.name+" ")
	if m.total > 0 && m.cursor < len(m.versions) {
		// The server total counts stream writes; discount the folded recreate
		// bookends so a fully-scrolled cursor can reach "N of N".
		left += m.hs.barDim.Render(fmt.Sprintf("%d of %d ", m.cursor+1, m.total-absorbedCount(m.versions)))
	}

	var right string
	switch {
	case m.loadErr != nil:
		right = m.hs.badgeWarn.Render("load failed")
	case m.loading:
		right = m.hs.barDim.Render(" loading… ")
	default:
		if m.envLabel != "" {
			right += m.hs.badgeEnv.Render(m.envLabel)
		}
		right += m.hs.badgeConn.Render(m.connLabel)
	}

	controls := m.hs.barDim.Render("↑↓ move · d diff · g/G jump · q quit")
	lw, rw, cw := lipgloss.Width(left), lipgloss.Width(right), lipgloss.Width(controls)
	// Drop the controls when there isn't room for them plus a gap each side.
	if lw+rw+cw+4 > m.width {
		controls, cw = "", 0
	}
	free := max(0, m.width-lw-rw-cw)
	lead := free / 2
	bar := left + m.hs.bar.Render(strings.Repeat(" ", lead)) + controls +
		m.hs.bar.Render(strings.Repeat(" ", free-lead)) + right
	// On a terminal narrower than the pills themselves the bar would overflow
	// and stretch every frame line to its width; cut it at the edge instead.
	return ansi.Truncate(bar, m.width, "")
}

// truncate cuts a plain (unstyled) string to w display cells, with an ellipsis
// when it doesn't fit. Used for the variable-length labels the layout can't
// guarantee room for; styled strings are sized by lipgloss instead.
func truncate(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= w {
		return s
	}
	if w == 1 {
		return "…"
	}
	r := []rune(s)
	for len(r) > 0 && lipgloss.Width(string(r))+1 > w {
		r = r[:len(r)-1]
	}
	return string(r) + "…"
}

// runHistoryTUI reads the first page, then runs the interactive timeline on the
// alt screen, paging older versions in on demand.
func runHistoryTUI(cmd *cobra.Command, r *remote.Client, name, envLabel, connLabel string) error {
	ctx, cancel := context.WithTimeout(cmd.Context(), projectionRPCTimeout)
	versions, total, err := r.ReadHistory(ctx, name, -1, historyPageSize)
	cancel()
	if err != nil {
		if errors.Is(err, remote.ErrNotFound) {
			return fmt.Errorf("%w: %q is not deployed on the server", remote.ErrNotFound, name)
		}
		return err
	}
	tw := newTextWriter(cmd.OutOrStdout(), cmd.ErrOrStderr())
	tw.warmBackground()
	m := historyModel{
		name:      name,
		envLabel:  envLabel,
		connLabel: connLabel,
		tw:        tw,
		hs:        newHistoryStyles(cmd.OutOrStdout()),
		client:    r,
		baseCtx:   cmd.Context(),
		total:     total,
		raw:       versions,
		versions:  collapseHistory(classifyHistory(versions)),
	}
	m.graph = computeHistoryGraph(m.versions)
	m.ow = operationWidth(m.versions)
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithContext(cmd.Context()), tea.WithOutput(cmd.OutOrStdout()))
	_, err = p.Run()
	if errors.Is(err, tea.ErrProgramKilled) || errors.Is(err, tea.ErrInterrupted) {
		return nil
	}
	return err
}
