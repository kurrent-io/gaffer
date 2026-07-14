package cmd

import (
	"fmt"
	"strconv"

	"github.com/charmbracelet/lipgloss"

	"github.com/kurrent-io/gaffer/cli/internal/deploy"
	"github.com/kurrent-io/gaffer/cli/internal/drift"
)

// WriteDiff renders a projection's comparison against what's deployed, matching
// the info command's heading + indented detail layout. In-sync and drifted
// projections show a line per dimension; a one-sided projection (not deployed,
// untracked) shows a single status line.
func (tw *textWriter) WriteDiff(e drift.Comparison) {
	tw.heading(e.Name)
	switch e.State {
	case drift.NotDeployed:
		tw.status(tw.styles.muted.Render("not deployed (local only)"))
	case drift.Untracked:
		tw.status(tw.driftStyle(e).Render(drift.Verdict(e) + " (deployed, not in gaffer.toml)"))
	case drift.Invalid:
		tw.writeInvalidDiff(e)
	default:
		tw.detail("Query", tw.queryStatus(e))
		tw.detail("Engine version", tw.versionStatus(e))
		tw.detail("Emit", tw.flagStatus(e.Cmp.EmitDiffers, e.Deployed.Emit, e.Local.Emit))
		// Track-emitted-streams is a niche v1 option; show it only when it drifts.
		if e.Cmp.TrackEmittedStreamsDiffers {
			tw.detail("Track emitted streams", tw.flagStatus(true, e.Deployed.TrackEmittedStreams, e.Local.TrackEmittedStreams))
		}
		// Attribute the drift when the ledger allows it (local edit vs a server-side change).
		if e.State == drift.Drifted {
			tw.detail("Drift", tw.driftStyle(e).Render(drift.Verdict(e)))
		}
	}
	// Who last deployed it and from where, when the ledger has it.
	tw.writeLedgerProvenance(e)
}

// writeInvalidDiff renders a diff whose local source doesn't compile: the
// dimensions that need no compile (query, engine version, track-emitted-streams)
// still show against the deployed side, emit is unknown, and there's no overall
// verdict. With nothing deployed there's nothing to compare, so it just notes the
// state. The compile error follows so the user knows what to fix.
func (tw *textWriter) writeInvalidDiff(e drift.Comparison) {
	// No readable local definition (e.g. a config error with a bad entry): nothing
	// to compare, so just the shared invalid body. Same rendering as info.
	if e.Local == nil {
		tw.writeInvalidBody(e.LocalErr)
		return
	}
	if e.Deployed == nil {
		tw.status(tw.styles.warning.Render("not deployed; invalid local definition"))
	} else {
		tw.detail("Query", tw.queryStatus(e))
		tw.detail("Engine version", tw.versionStatus(e))
		tw.detail("Emit", tw.styles.warning.Render("unknown (invalid local definition)"))
		if e.Cmp.TrackEmittedStreamsDiffers {
			tw.detail("Track emitted streams", tw.flagStatus(true, e.Deployed.TrackEmittedStreams, e.Local.TrackEmittedStreams))
		}
	}
	if e.LocalErr != nil {
		tw.blank()
		tw.write("%s\n", tw.styles.errDetail.Render(e.LocalErr.Error()))
	}
}

// writeInvalidBody renders the body of an invalid projection - the "invalid"
// line and the reason. The caller writes the heading. Shared by info and diff so
// the invalid presentation stays consistent across the inspection commands.
func (tw *textWriter) writeInvalidBody(reason error) {
	tw.status(tw.styles.warning.Render("invalid local definition"))
	if reason != nil {
		tw.blank()
		tw.write("%s\n", tw.styles.errDetail.Render(reason.Error()))
	}
}

// The per-dimension helpers show the value when local and deployed agree (a
// single value implies in sync) and the change when they differ. The query has
// no scalar value, so it shows "in sync" or a +added -removed line stat; the
// full source diff is the external viewer's job.

// A matched dimension renders green (all green reads as in sync at a glance); a
// drifted one renders the change in the warning colour so it stands out.

func (tw *textWriter) queryStatus(e drift.Comparison) string {
	if !e.Cmp.QueryDiffers {
		return tw.styles.added.Render("in sync")
	}
	added, removed := deploy.LineStat(e.Deployed.Query, e.Local.Query)
	return tw.styles.added.Render(fmt.Sprintf("+%d", added)) + " " +
		tw.styles.errDetail.Render(fmt.Sprintf("-%d", removed))
}

func (tw *textWriter) versionStatus(e drift.Comparison) string {
	if !e.Cmp.EngineVersionDiffers {
		return tw.styles.added.Render(strconv.Itoa(e.Local.EngineVersion))
	}
	return tw.styles.warning.Render(fmt.Sprintf("remote %d, local %d", e.Deployed.EngineVersion, e.Local.EngineVersion))
}

func (tw *textWriter) flagStatus(differs, remote, local bool) string {
	if !differs {
		return tw.styles.added.Render(enabledStr(local))
	}
	return tw.styles.warning.Render(fmt.Sprintf("remote %s, local %s", enabledStr(remote), enabledStr(local)))
}

func enabledStr(b bool) string {
	if b {
		return "enabled"
	}
	return "disabled"
}

// WriteQueryDiff renders the aligned query diff inline: dual line-number
// gutters (remote then local), a +/- marker, and every line of both sides with
// the changes in place. The span that changed within a paired line renders
// reversed, so a one-token edit is findable without reading the whole pair.
func (tw *textWriter) WriteQueryDiff(lines []deploy.DiffLine) {
	for _, row := range tw.queryDiffRows(lines) {
		tw.write("%s\n", row)
	}
}

// queryDiffRows renders the aligned diff one string per row, shared by the
// static output (which prints them) and the history diff modal (which scrolls
// them in a viewport).
func (tw *textWriter) queryDiffRows(lines []deploy.DiffLine) []string {
	maxOld, maxNew := 1, 1
	for _, dl := range lines {
		maxOld = max(maxOld, dl.OldN)
		maxNew = max(maxNew, dl.NewN)
	}
	ow, nw := len(strconv.Itoa(maxOld)), len(strconv.Itoa(maxNew))
	rows := make([]string, 0, len(lines))
	for _, dl := range lines {
		oldN, newN := "", ""
		if dl.OldN > 0 {
			oldN = strconv.Itoa(dl.OldN)
		}
		if dl.NewN > 0 {
			newN = strconv.Itoa(dl.NewN)
		}
		gutter := tw.styles.dim.Render(fmt.Sprintf("%*s %*s", ow, oldN, nw, newN))
		switch {
		case dl.Kind == deploy.LineRemoved:
			rows = append(rows, fmt.Sprintf("%s %s", gutter, tw.diffLineText(dl, "-", tw.styles.diffRemoved, tw.styles.diffRemovedEmph)))
		case dl.Kind == deploy.LineAdded:
			rows = append(rows, fmt.Sprintf("%s %s", gutter, tw.diffLineText(dl, "+", tw.styles.diffAdded, tw.styles.diffAddedEmph)))
		case dl.Text == "":
			// A blank equal line carries no padding after the gutter - trailing
			// spaces we add are noise to whitespace-sensitive consumers. Trailing
			// spaces inside a line are source content and always kept.
			rows = append(rows, gutter)
		default:
			rows = append(rows, fmt.Sprintf("%s   %s", gutter, dl.Text))
		}
	}
	return rows
}

// diffLineText is a changed line with its marker, washed in the line tint,
// the emphasised span in the stronger tint. The span wash is skipped when it
// is unset (an unpaired line) or covers the entire line (a full rewrite -
// emphasising everything highlights nothing). A blank line is its bare
// marker, no trailing pad.
func (tw *textWriter) diffLineText(dl deploy.DiffLine, marker string, line, emph lipgloss.Style) string {
	if dl.Text == "" {
		return line.Render(marker)
	}
	whole := dl.EmphFrom == 0 && dl.EmphTo == len(dl.Text)
	if dl.EmphFrom >= dl.EmphTo || whole {
		return line.Render(marker + " " + dl.Text)
	}
	return line.Render(marker+" "+dl.Text[:dl.EmphFrom]) +
		emph.Render(dl.Text[dl.EmphFrom:dl.EmphTo]) +
		line.Render(dl.Text[dl.EmphTo:])
}
