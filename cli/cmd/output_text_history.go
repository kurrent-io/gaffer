package cmd

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/kurrent-io/gaffer/cli/internal/deploy"
	"github.com/kurrent-io/gaffer/cli/internal/remote"
)

const historyHashWidth = 7 // a short content hash; blank for a state change

// WriteHistory renders a projection's history as a timeline, newest first, in
// aligned columns: a run-state glyph, the content hash (blank for a state change),
// the operation, and the time, with a dimmed provenance line beneath where there's
// something to attribute. The glyph and rail are coloured by run state - filled/
// live when the projection was enabled at that point, hollow/dim when disabled.
func (tw *textWriter) WriteHistory(name string, versions []historyVersion, total int64) {
	if len(versions) == 0 {
		tw.write("No history for %s.\n", name)
		return
	}
	ow := operationWidth(versions)
	p := newGraphPainter(tw, versions, computeHistoryGraph(versions))
	// The provenance line aligns under the operation column, past the graph gutter:
	// space + hash + gap.
	indent := strings.Repeat(" ", historyHashWidth+3)
	for i, hv := range versions {
		tw.write("%s %s\n", p.node(i), tw.historyRowTail(hv, ow, p.nodePad(i)))
		cons := p.connectors(i)
		if line := tw.historyProvenance(hv); line != "" {
			tw.write("%s%s%s\n", p.railGutter(i), indent, line)
		} else if len(cons) == 0 && p.gapHasBranch(i) {
			// A revert runs across this gap but the row has nothing to attribute, so
			// carry the spine and bridge down on a bare rail line.
			tw.write("%s\n", strings.TrimRight(p.railGutter(i), " "))
		}
		for _, c := range cons {
			tw.write("%s\n", strings.TrimRight(c, " "))
		}
	}
	// total counts stream writes; folded bookends aren't rows, so discount them
	// or a fully-shown history would claim entries remain.
	if displayTotal := total - absorbedCount(versions); displayTotal > int64(len(versions)) {
		tw.blank()
		tw.write("%s\n", tw.styles.pipe.Render(fmt.Sprintf(
			"Showing %d of %d entries. Pass --all or --limit to see more.", len(versions), displayTotal)))
	}
}

// historyRowTail is a row's content past the node glyph: the hash (blank for a state
// change) which sits right after the glyph, then pad to hold the operation and time
// in fixed columns as the hash indents with its node. The graph painter renders the
// run-state glyph in the gutter ahead of it.
func (tw *textWriter) historyRowTail(hv historyVersion, ow int, pad string) string {
	hash := strings.Repeat(" ", historyHashWidth)
	if !hv.StateChange() {
		hash = tw.styles.dim.Render(padCells(hv.Hash, historyHashWidth))
	}
	op := tw.historyKindStyle(hv).Render(padCells(truncate(hv.eventLabel(), ow), ow))
	when := "-"
	if hv.Definition != nil && !hv.Definition.Time.IsZero() {
		when = hv.Definition.Time.Format("2006-01-02 15:04")
	}
	return fmt.Sprintf("%s%s  %s  %s", hash, pad, op, when)
}

// historyProvenance is the dimmed second line: the out-of-band caution for an
// external edit, or the deployer / tool / source revision for a version carrying
// metadata. Empty for a bare lifecycle or create event with nothing to attribute.
func (tw *textWriter) historyProvenance(hv historyVersion) string {
	text, warn := historyProvenanceText(hv)
	if text == "" {
		return ""
	}
	if warn {
		return tw.styles.warning.Render(text)
	}
	return tw.styles.dim.Render(text)
}

// historyProvenanceText is the plain second-line attribution for a version: the
// out-of-band caution (warn) or the deployer, tool, and source revision for a
// version carrying metadata. Empty for a bare lifecycle or create event. Shared by
// the static renderer and the interactive timeline's rail, which apply their own
// style and (for the TUI) truncate it to the pane.
func historyProvenanceText(hv historyVersion) (text string, warn bool) {
	switch hv.Kind {
	case remote.KindUpdated:
		// A metadata-less content change: show what moved. When it lands after
		// gaffer began managing the projection it's out-of-band, so add the caution.
		if hv.OutOfBand() {
			return glyphWarning + " " + changeSummary(hv.Change) + " outside gaffer", true
		}
		return changeSummary(hv.Change), false
	case remote.KindUnreadable:
		return glyphWarning + " deploy metadata could not be read", true
	case remote.KindReconfigured:
		return configSummary(hv.ConfigChanges), false
	}
	if hv.Ledger == nil {
		return "", false
	}
	var parts []string
	if hv.Ledger.Actor != "" {
		parts = append(parts, hv.Ledger.Actor)
	}
	via := hv.Ledger.Tool
	if hv.Ledger.ToolVersion != "" {
		via += " " + hv.Ledger.ToolVersion
	}
	if via != "" {
		parts = append(parts, via)
	}
	if hv.Ledger.Revision != "" {
		parts = append(parts, "src "+shortRevision(hv.Ledger.Revision))
	}
	// A foreign tool's write after gaffer began managing the projection is
	// out-of-band: keep its attribution (who/what/where) but flag it.
	if hv.OutOfBand() {
		parts = append([]string{"changed outside gaffer"}, parts...)
		return glyphWarning + " " + strings.Join(parts, dotSep), true
	}
	if len(parts) == 0 {
		return "", false
	}
	return strings.Join(parts, dotSep), false
}

// historyKindStyle colours the operation label by meaning: a good content change
// - gaffer's deploys and any neutral create/update - in cyan; enable green and
// delete red to match the run-state palette; genuine attention (out-of-band
// changes, unreadable metadata) in warning orange; a plain no-op rewrite in the
// faintest dim; a lifecycle disable/reconfigure a quiet-but-present grey.
func (tw *textWriter) historyKindStyle(hv historyVersion) lipgloss.Style {
	if hv.OutOfBand() {
		return tw.styles.warning
	}
	switch hv.Kind {
	case remote.KindDeleted:
		return tw.styles.errStatus
	case remote.KindEnabled:
		return tw.styles.added
	case remote.KindUnreadable:
		return tw.styles.warning
	// A "good" content change - gaffer's own operations and any neutral (not
	// out-of-band) create/update, whoever made it - shares the deploy colour; it
	// only goes warning-orange when out-of-band (handled above).
	case remote.KindDeploy, remote.KindRollback, remote.KindReset, remote.KindRecreate,
		remote.KindCreated, remote.KindUpdated, remote.KindUpdatedByTool:
		return tw.styles.label
	case remote.KindRewritten:
		return tw.styles.dim
	default: // disabled, reconfigured
		return tw.styles.muted
	}
}

// historyGlyph shows the run state at this point: a cross for a deleted (gone)
// projection, a filled dot when enabled (running), a hollow dot when disabled.
// A recreate keeps the plain dot; the rail below it carries the termination cap
// (see graphPainter.railGutter), marking where the old line ended.
func historyGlyph(hv historyVersion) string {
	switch {
	case hv.Deleted:
		return "✗"
	case hv.Enabled():
		return "●"
	default:
		return "○"
	}
}

// historyRunStyle colours a node's glyph and its live-path rail by run state: red
// for a deleted projection, green while enabled (running), a quiet grey while
// disabled - still brighter than the faint dotted indent guides, so a stopped entry
// reads as real history, not scaffolding.
func (tw *textWriter) historyRunStyle(hv historyVersion) lipgloss.Style {
	switch {
	case hv.Deleted:
		return tw.styles.errStatus
	case hv.Enabled():
		return tw.styles.added
	default:
		return tw.styles.muted
	}
}

// changeSummary names the dimensions that differ between two versions, e.g.
// "query changed" or "query and emit changed", for the external-edit caution.
func changeSummary(c deploy.Comparison) string {
	var dims []string
	if c.QueryDiffers {
		dims = append(dims, "query")
	}
	if c.EngineVersionDiffers {
		dims = append(dims, "engine version")
	}
	if c.EmitDiffers {
		dims = append(dims, "emit")
	}
	if c.TrackEmittedStreamsDiffers {
		dims = append(dims, "tracking")
	}
	if len(dims) == 0 {
		return "definition changed"
	}
	return joinAnd(dims) + " changed"
}

// joinAnd joins items as "a", "a and b", or "a, b and c".
func joinAnd(items []string) string {
	switch len(items) {
	case 0:
		return ""
	case 1:
		return items[0]
	case 2:
		return items[0] + " and " + items[1]
	default:
		return strings.Join(items[:len(items)-1], ", ") + " and " + items[len(items)-1]
	}
}

// operationWidth is the display width of the operation column: the widest
// operation label present, capped so a long "updated via <tool>" doesn't push the
// time column off the screen (it truncates instead).
func operationWidth(versions []historyVersion) int {
	const cap = 20 // fits "unreadable metadata"
	w := 0
	for _, hv := range versions {
		if l := lipgloss.Width(hv.eventLabel()); l > w {
			w = l
		}
	}
	return min(w, cap)
}
