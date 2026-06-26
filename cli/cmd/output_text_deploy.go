package cmd

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// deployResultLine renders one projection's verdict: a status marker, the name
// padded to nameWidth so verdicts align, and the outcome. Returns the line
// without a trailing newline so both the plain sink and the interactive view can
// place it. Created/updated read green, skipped faint, refused a warning, a
// failed RPC red.
func (tw *textWriter) deployResultLine(res deployResult, nameWidth int) string {
	name := fmt.Sprintf("%-*s", nameWidth, res.Name)
	var marker, verdict string
	switch {
	case res.Err != nil:
		marker = tw.styles.errStatus.Render("✗")
		verdict = tw.styles.errDetail.Render("failed: " + res.Err.Error())
	case res.Action == actRefuse:
		marker = tw.styles.warning.Render("✗")
		verdict = tw.styles.warning.Render("refused (" + res.Reason + ")")
	case res.Action == actSkip:
		marker = tw.styles.pipe.Render("·")
		verdict = tw.styles.pipe.Render("skipped (in sync)")
	case res.Action == actCreate:
		marker = tw.styles.added.Render("✓")
		verdict = tw.styles.added.Render("created")
	case res.Action == actUpdate:
		marker = tw.styles.added.Render("✓")
		word := "updated"
		if res.LogicChange {
			word = "updated (logic change, continued from checkpoint)"
		}
		verdict = tw.styles.added.Render(word)
	case res.Action == actReset:
		marker = tw.styles.added.Render("✓")
		verdict = tw.styles.added.Render("rebuilt (reprocessing from zero)")
	default:
		marker = tw.styles.warning.Render("?")
		verdict = tw.styles.warning.Render("unknown")
	}
	return fmt.Sprintf("  %s %s  %s", marker, name, verdict)
}

// deployRowLine renders one projection row for the interactive in-place view:
// pending (dim, waiting), active (the spinner frame, deploying now), or its
// final verdict once done. spin is the current spinner frame, used only for the
// active row. Done rows reuse deployResultLine so live and committed lines match.
func (tw *textWriter) deployRowLine(row deployRow, spin string, nameWidth int) string {
	switch row.status {
	case rowActive:
		return fmt.Sprintf("  %s %-*s  %s", spin, nameWidth, row.name, tw.styles.label.Render("deploying"))
	case rowDone:
		return tw.deployResultLine(row.res, nameWidth)
	default:
		return fmt.Sprintf("  %s %s", tw.styles.pipe.Render("·"), tw.styles.pipe.Render(fmt.Sprintf("%-*s", nameWidth, row.name)))
	}
}

// deploySummaryLine is the tally after a run. Created, updated and skipped
// always show; refused and failed only when non-zero, in their alert colour.
func (tw *textWriter) deploySummaryLine(c deployCounts) string {
	segs := []string{
		fmt.Sprintf("%d created", c.created),
		fmt.Sprintf("%d updated", c.updated),
		fmt.Sprintf("%d skipped", c.skipped),
	}
	if c.rebuilt > 0 {
		segs = append(segs, fmt.Sprintf("%d rebuilt", c.rebuilt))
	}
	if c.refused > 0 {
		segs = append(segs, tw.styles.warning.Render(fmt.Sprintf("%d refused", c.refused)))
	}
	if c.failed > 0 {
		segs = append(segs, tw.styles.errStatus.Render(fmt.Sprintf("%d failed", c.failed)))
	}
	return strings.Join(segs, tw.styles.pipe.Render(" · "))
}

func (tw *textWriter) writeDeploySummary(c deployCounts) {
	tw.blank()
	tw.write("%s\n", tw.deploySummaryLine(c))
}

// writePlanSummary previews what a deploy would change, ahead of the confirm
// prompt: each changing or failed projection on its own line (name, verdict,
// detail), then the per-action counts, then any faulted-update or emitting-reset
// caution. In-sync projections are counted only, not listed.
func (tw *textWriter) writePlanSummary(plan []plannedItem, target string, totals planTotals, prod bool) {
	if prod {
		banner := "PRODUCTION"
		if target != "" {
			banner += " - " + target
		}
		tw.write("%s\n", tw.styles.errStatus.Render("⚠ "+banner))
	}
	skipped, refused, logicContinues, errored := 0, 0, 0, 0
	for _, it := range plan {
		switch {
		case it.err != nil:
			errored++
		case it.action == actSkip:
			skipped++
		case it.action == actRefuse:
			refused++
		case it.action == actUpdate && it.logicChange:
			logicContinues++
		}
	}

	var segs []string
	if totals.creates > 0 {
		segs = append(segs, tw.styles.added.Render(fmt.Sprintf("%d to create", totals.creates)))
	}
	if totals.updates > 0 {
		segs = append(segs, tw.styles.added.Render(fmt.Sprintf("%d to update", totals.updates)))
	}
	if totals.rebuilds > 0 {
		segs = append(segs, tw.styles.warning.Render(fmt.Sprintf("%d to rebuild", totals.rebuilds)))
	}
	if skipped > 0 {
		segs = append(segs, tw.styles.pipe.Render(fmt.Sprintf("%d in sync", skipped)))
	}
	if errored > 0 {
		segs = append(segs, tw.styles.errStatus.Render(fmt.Sprintf("%d failed", errored)))
	}
	if refused > 0 {
		segs = append(segs, tw.styles.warning.Render(fmt.Sprintf("%d refused", refused)))
	}

	heading := "Plan"
	if target != "" {
		heading += " for " + target
	}
	tw.write("%s\n", tw.styles.heading.Render(heading+":"))

	// List every projection that does something (or failed to plan), in plan
	// order, so the user sees which ones change and why any are refused. In-sync
	// projections are counted only, not listed, so they don't drown the signal.
	// Three columns: name, the coloured verdict, then a dimmed detail.
	// Collect the listed projections once (planVerdict is the single source for
	// which list and how), then size the columns and render from the rows.
	var rows []planPreviewRow
	for _, it := range plan {
		if word, styled, detail := tw.planVerdict(it); word != "" {
			rows = append(rows, planPreviewRow{it.name, word, styled, detail})
		}
	}
	nameWidth, verdictWidth := 0, 0
	for _, r := range rows {
		nameWidth = max(nameWidth, utf8.RuneCountInString(r.name))
		verdictWidth = max(verdictWidth, len(r.word))
	}
	for _, r := range rows {
		line := fmt.Sprintf("  %-*s  %s", nameWidth, r.name, r.styled)
		if r.detail != "" {
			line += strings.Repeat(" ", verdictWidth-len(r.word)) + "  " + tw.styles.muted.Render(r.detail)
		}
		tw.write("%s\n", line)
	}

	tw.write("  %s\n", strings.Join(segs, tw.styles.pipe.Render(" · ")))
	if logicContinues > 0 {
		tw.write("  %s\n", tw.styles.pipe.Render(fmt.Sprintf(
			"%d logic change(s) continuing from checkpoint - --reset-on-logic-change to rebuild instead", logicContinues)))
	}
	tw.writeApplyWarnings(plan)
	tw.blank()
}

// planPreviewRow is one listed projection's rendered columns, collected once so
// the verdict is styled a single time rather than re-rendered for width sizing.
type planPreviewRow struct{ name, word, styled, detail string }

// planVerdict is one projection's disposition for the preview: word is the
// unstyled disposition (returned for column alignment), styled is it in its
// severity colour, and detail is an optional dimmed explanation for a trailing
// column - the reason for a refusal (shown in full, since the immutable field and
// the recreate remedy are the point) or the error for a plan failure. An in-sync
// projection returns an empty word: it's counted, not listed.
func (tw *textWriter) planVerdict(it plannedItem) (word, styled, detail string) {
	switch {
	case it.err != nil:
		return "failed", tw.styles.errStatus.Render("failed"), it.err.Error()
	case it.action == actCreate:
		return "create", tw.styles.added.Render("create"), ""
	case it.action == actUpdate:
		if it.logicChange {
			return "update", tw.styles.added.Render("update"), "logic change, continuing from checkpoint"
		}
		return "update", tw.styles.added.Render("update"), ""
	case it.action == actReset:
		return "rebuild", tw.styles.warning.Render("rebuild"), "reprocessing from zero"
	case it.action == actRefuse:
		return "refused", tw.styles.warning.Render("refused"), it.reason
	default:
		return "", "", ""
	}
}

// writeApplyWarnings emits the per-projection cautions for a plan: an update over
// a faulted projection (the update won't clear the fault), and a reset of an
// emitting projection (reprocessing re-emits, duplicating into its target
// streams). Shared by the interactive plan summary and the non-interactive
// (--yes) path, so the cautions surface however the deploy is confirmed.
func (tw *textWriter) writeApplyWarnings(plan []plannedItem) {
	for _, name := range faultedUpdates(plan) {
		tw.write("  %s %s\n", tw.styles.warning.Render("⚠"),
			tw.styles.warning.Render(name+" is faulted; updating won't clear the fault"))
	}
	for _, name := range emittingResets(plan) {
		tw.write("  %s %s\n", tw.styles.warning.Render("⚠"),
			tw.styles.warning.Render(name+" emits; rebuilding re-emits and may duplicate - use gaffer recreate --delete-emitted for a clean rebuild"))
	}
}

// writePreflightFailures reports the projections that can't be deployed and how
// to proceed. Each failure shows its name and one line per problem (a compile
// error, or each error-severity diagnostic), in the alert colour.
func (tw *textWriter) writePreflightFailures(total int, failures []preflightFailure) {
	tw.write("%s\n\n", tw.styles.heading.Render(
		fmt.Sprintf("Preflight failed: %d of %d projections have errors", len(failures), total)))
	for _, f := range failures {
		reasons := f.reasons()
		tw.write("  %s %s\n", tw.styles.errStatus.Render("✗"), tw.styles.heading.Render(f.Name))
		for _, r := range reasons {
			tw.write("    %s\n", tw.styles.errDetail.Render(r))
		}
	}
	tw.write("\n%s\n", tw.styles.pipe.Render("Fix the errors above, or pass --no-validate to deploy anyway."))
}
