package cmd

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// maxLane is the deepest lane any node sits in - the timeline's branch depth, which
// sets the graph gutter's fixed width so every row's content starts at one column.
func (g historyGraph) maxLane() int {
	m := 0
	for _, l := range g.nodeLane {
		if l > m {
			m = l
		}
	}
	return m
}

// gutterWidth is the fixed width of the graph field: two cells per branch lane (rail
// plus gap) and one for the node glyph, so a linear timeline is a single glyph
// column (identical to the pre-branch layout) and each nested level adds two.
func (g historyGraph) gutterWidth() int { return 2*g.maxLane() + 1 }

// graphPainter renders the timeline's left graph field. The live path is solid and
// run-state tinted: it runs down the spine, curves into an indented lane through the
// entries a revert rolled past, and curves back. Alongside the detour runs the dotted
// bridge - the faint link from the restore back to the entry it matched. The curves
// are rounded so they join the endpoints and the branch without tying into the
// bridge, which stays a separate dotted indent line.
type graphPainter struct {
	tw       *textWriter
	g        historyGraph
	versions []historyVersion
	width    int // gutter width, g.gutterWidth()
}

func newGraphPainter(tw *textWriter, versions []historyVersion, g historyGraph) graphPainter {
	return graphPainter{tw: tw, g: g, versions: versions, width: g.gutterWidth()}
}

func (p graphPainter) blank() []string {
	cells := make([]string, p.width)
	for i := range cells {
		cells[i] = " "
	}
	return cells
}

// dim renders the dotted bridge in the faintest grey - dimmer than a disabled entry,
// so the link recedes furthest and never competes with the live line.
func (p graphPainter) dim(s string) string { return p.tw.styles.faded.Render(s) }

// spineBlank reports whether the rail below row i should be blank: past the oldest
// row, or across the gap above a tombstone (the projection didn't exist over that
// span, so nothing connects the delete to the entry above it).
func (p graphPainter) spineBlank(i int) bool {
	return i+1 >= len(p.versions) || p.versions[i+1].Deleted
}

// bridged reports whether lane's vertical is the dotted bridge rather than the live
// line: true when the live path has forked one lane deeper across this point, so this
// lane only links the revert's endpoints. On a node row the deeper detour must
// strictly enclose the row; over a gap it must have forked above (top < i) and still
// be open.
func (p graphPainter) bridged(lane, i int, nodeRow bool) bool {
	for _, s := range p.g.spans {
		if s.lane != lane+1 {
			continue
		}
		if nodeRow && s.top < i && i < s.bottom {
			return true
		}
		if !nodeRow && s.top < i && s.bottom >= i+1 {
			return true
		}
	}
	return false
}

// liveStyle is the run-state colour for the live path across the gap below row i -
// the same tint as the entry's glyph (green running, grey stopped, red deleted), so
// the solid line reads as the live timeline, not scaffolding.
func (p graphPainter) liveStyle(i int) lipgloss.Style {
	if i+1 < len(p.versions) {
		return p.tw.historyRunStyle(p.versions[i+1])
	}
	return p.tw.styles.faded
}

// vert is a lane's vertical: the run-state-tinted live rail, or the faint dotted
// bridge when the live path has forked deeper and this lane is only the link.
func (p graphPainter) vert(lane, i int, nodeRow bool) string {
	if p.bridged(lane, i, nodeRow) {
		return p.dim("┆")
	}
	return p.liveStyle(i).Render("│")
}

// node renders a row's node prefix: a vertical for each shallower lane (live rail or
// dotted bridge) then the run-state glyph in the row's own lane. It stops at the
// glyph (no trailing pad) so the caller can place the content hash right after it -
// the hash belongs to the dot and indents with it.
func (p graphPainter) node(i int) string {
	lane := p.g.nodeLane[i]
	cells := make([]string, 2*lane+1)
	for k := range cells {
		cells[k] = " "
	}
	for k := range lane {
		cells[2*k] = p.vert(k, i, true)
	}
	hv := p.versions[i]
	cells[2*lane] = p.tw.historyRunStyle(hv).Render(historyGlyph(hv))
	return strings.Join(cells, "")
}

// nodePad is the spacing after a row's hash that holds the operation column fixed
// while the hash itself indents with the node: shallower nodes pad by the lanes they
// don't occupy, so every row's operation lands in the same column.
func (p graphPainter) nodePad(i int) string {
	return strings.Repeat(" ", 2*(p.g.maxLane()-p.g.nodeLane[i]))
}

// railGutter is the gutter for the line below row i - a provenance or bare rail line:
// the live spine plus each lane's vertical still open across the gap. A lane forking
// at this gap is drawn on its own connector line below, so it's omitted here. In a
// linear view this collapses to the run-state-tinted spine of the pre-branch layout.
func (p graphPainter) railGutter(i int) string {
	if p.g.maxLane() == 0 {
		if p.spineBlank(i) {
			return " "
		}
		return p.tw.historyRunStyle(p.versions[i+1]).Render("│")
	}
	cells := p.blank()
	if !p.spineBlank(i) {
		cells[0] = p.vert(0, i, false)
	}
	for _, s := range p.g.spans {
		if s.lane > 0 && s.top != i && s.top <= i && s.bottom >= i+1 {
			cells[2*s.lane] = p.vert(s.lane, i, false)
		}
	}
	return strings.Join(cells, "")
}

// gapHasBranch reports whether a lane is open across the gap below row i, so a bare
// rail line must carry its vertical down even when the row has no provenance.
func (p graphPainter) gapHasBranch(i int) bool {
	for _, s := range p.g.spans {
		if s.lane > 0 && s.top <= i && s.bottom >= i+1 {
			return true
		}
	}
	return false
}

// connectors are the fork and rejoin lines in the gap below row i: the live path
// curving out to a detour lane (╰─╮) or back to the spine (╭─╯). The corners are
// rounded so the connector joins the endpoint and the branch without tying into the
// dotted bridge that runs alongside. Usually none or one; a gap that both closes and
// opens a bracket emits both.
func (p graphPainter) connectors(i int) []string {
	if p.g.maxLane() == 0 {
		return nil
	}
	var out []string
	for _, s := range p.g.spans {
		if s.bottom == i+1 {
			out = append(out, p.junction(s.lane, "╭", "╯", i))
		}
	}
	for _, s := range p.g.spans {
		if s.top == i {
			out = append(out, p.junction(s.lane, "╰", "╮", i))
		}
	}
	return out
}

// junction draws a fork or rejoin: the spine and any shallower open lane as their
// verticals, a rounded corner on the parent lane where the live path turns (╰ out on
// a fork, ╭ back on a rejoin), and the branch corner one lane to the right (╮ / ╯).
// The rounded parent corner has no vertical stroke into the bridge alongside it.
func (p graphPainter) junction(lane int, parent, corner string, i int) string {
	cells := p.blank()
	if !p.spineBlank(i) {
		cells[0] = p.vert(0, i, false)
	}
	for _, s := range p.g.spans {
		if s.lane > 0 && s.lane < lane && s.top <= i && s.bottom >= i+1 {
			cells[2*s.lane] = p.vert(s.lane, i, false)
		}
	}
	live := p.liveStyle(i)
	cells[2*(lane-1)] = live.Render(parent)
	cells[2*lane-1] = live.Render("─")
	cells[2*lane] = live.Render(corner)
	return strings.Join(cells, "")
}
