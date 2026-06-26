package cmd

import (
	"fmt"
	"strconv"

	"github.com/kurrent-io/gaffer/cli/internal/deploy"
)

// WriteDiff renders a projection's comparison against what's deployed, matching
// the info command's heading + indented detail layout. In-sync and drifted
// projections show a line per dimension; a one-sided projection (not deployed,
// untracked) shows a single status line.
func (tw *textWriter) WriteDiff(e comparison) {
	tw.heading(e.Name)
	switch e.State {
	case driftNotDeployed:
		tw.status(tw.styles.warning.Render("not deployed (local only)"))
	case driftUntracked:
		tw.status(tw.styles.warning.Render("untracked (deployed, not in gaffer.toml)"))
	case driftInvalid:
		tw.writeInvalidDiff(e)
	default:
		tw.detail("Query", tw.queryStatus(e))
		tw.detail("Engine version", tw.versionStatus(e))
		tw.detail("Emit", tw.flagStatus(e.Cmp.EmitDiffers, e.Deployed.Emit, e.Local.Emit))
		// Track-emitted-streams is a niche v1 option; show it only when it drifts.
		if e.Cmp.TrackEmittedStreamsDiffers {
			tw.detail("Track emitted streams", tw.flagStatus(true, e.Deployed.TrackEmittedStreams, e.Local.TrackEmittedStreams))
		}
	}
}

// writeInvalidDiff renders a diff whose local source doesn't compile: the
// dimensions that need no compile (query, engine version, track-emitted-streams)
// still show against the deployed side, emit is unknown, and there's no overall
// verdict. With nothing deployed there's nothing to compare, so it just notes the
// state. The compile error follows so the user knows what to fix.
func (tw *textWriter) writeInvalidDiff(e comparison) {
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

func (tw *textWriter) queryStatus(e comparison) string {
	if !e.Cmp.QueryDiffers {
		return tw.styles.added.Render("in sync")
	}
	added, removed := deploy.LineStat(e.Deployed.Query, e.Local.Query)
	return tw.styles.added.Render(fmt.Sprintf("+%d", added)) + " " +
		tw.styles.errDetail.Render(fmt.Sprintf("-%d", removed))
}

func (tw *textWriter) versionStatus(e comparison) string {
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
