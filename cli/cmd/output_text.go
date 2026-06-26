package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
	gafferruntime "github.com/kurrent-io/gaffer/bindings/go"
	"github.com/kurrent-io/gaffer/cli/internal/deploy"
	"github.com/kurrent-io/gaffer/cli/internal/engine"
	"github.com/kurrent-io/gaffer/cli/internal/remote"
	"github.com/muesli/termenv"
)

const indentSize = 3

type field struct{ label, value string }

type textWriter struct {
	prefixed
	w      io.Writer
	errW   io.Writer
	line   prefixed
	corner prefixed
	styles textStyles
	// Pending event held between WriteEvent and the matching
	// WriteResult / WriteError. Lets WriteResult drop the entire
	// event block silently when the result is "skipped" and
	// showSkipped is off.
	pending *eventInfo
	// showSkipped renders the per-event skip row + a breakdown in
	// the summary. Set true for fixture mode (the user curated
	// the events; a skip is diagnostic - "you forgot a handler",
	// "your partitionBy returned null"), false for live mode
	// (skips are runtime hygiene noise from $all).
	showSkipped bool
	// compileQuirks holds quirk.* diagnostic codes seen at compile time
	// (captured in WriteInfo); runtimeQuirks the distinct codes streamed via
	// OnDiagnostic during the run. The summary lists their union, so it covers
	// every quirk the run surfaced - header or per-event.
	compileQuirks []string
	runtimeQuirks map[string]bool
	// links is true on interactive terminals (the renderer resolves a
	// non-Ascii colour profile), where diagnostic codes are wrapped in OSC 8
	// hyperlinks to their docs anchor. Off for pipes, CI, and tests so output
	// stays plain and copyable.
	links bool
}

// diagnosticsReferenceURL is the generated reference page; each code has a
// matching `#<code>` anchor. Printed once per summary as a plain `See` line,
// and used as the target when codes are hyperlinked on interactive terminals.
const diagnosticsReferenceURL = "https://gaffer.kurrent.io/reference/diagnostics/"

type textStyles struct {
	label     lipgloss.Style
	pipe      lipgloss.Style
	muted     lipgloss.Style
	logLabel  lipgloss.Style
	emitted   lipgloss.Style
	processed lipgloss.Style
	added     lipgloss.Style
	skipped   lipgloss.Style
	warning   lipgloss.Style
	errStatus lipgloss.Style
	errDetail lipgloss.Style
	heading   lipgloss.Style
	info      lipgloss.Style
}

type prefixed struct {
	tw  *textWriter
	pfx string
}

func newTextWriter(w, errW io.Writer) *textWriter {
	r := lipgloss.NewRenderer(w)
	tw := &textWriter{
		w:     w,
		errW:  errW,
		links: r.ColorProfile() != termenv.Ascii,
		styles: textStyles{
			label:     r.NewStyle().Foreground(lipgloss.Color("6")),
			pipe:      r.NewStyle().Faint(true).Foreground(lipgloss.Color("6")),
			muted:     r.NewStyle().Foreground(lipgloss.Color("8")),
			logLabel:  r.NewStyle().Foreground(lipgloss.Color("4")),
			emitted:   r.NewStyle(),
			processed: r.NewStyle().Faint(true).Foreground(lipgloss.Color("2")),
			added:     r.NewStyle().Foreground(lipgloss.Color("2")),
			skipped:   r.NewStyle().Foreground(lipgloss.Color("3")),
			warning:   r.NewStyle().Foreground(lipgloss.Color("3")),
			errStatus: r.NewStyle().Foreground(lipgloss.Color("9")),
			errDetail: r.NewStyle().Foreground(lipgloss.Color("1")),
			heading:   r.NewStyle().Bold(true),
			info:      r.NewStyle().Foreground(lipgloss.Color("4")),
		},
	}
	tw.prefixed = prefixed{tw: tw, pfx: tw.ind()}
	tw.line = prefixed{tw: tw, pfx: tw.ind("│")}
	tw.corner = prefixed{tw: tw, pfx: tw.styles.pipe.Render("╰") + " "}
	return tw
}

func (tw *textWriter) RegisterCallbacks(session sessionCallbacks) {
	session.OnEmit(func(streamID, eventType, data, metadata string, isJSON, isLink bool) {
		tw.writeEmittedCb(streamID, eventType, data, metadata, isJSON, isLink)
	})
	session.OnLog(func(message string) {
		// Flush the deferred event header first so logs nest under their
		// own event in the order they were emitted, not before the header.
		tw.flushPending()
		tw.write("%s %s\n", tw.lineSub(tw.styles.logLabel.Render("[log]")), message)
	})
	session.OnDiagnostic(func(d gafferruntime.Diagnostic) {
		// Quirks stream at the point they fire, so they render inline in the
		// same ├ flow as logs/emits. Also collected for the run summary.
		tw.flushPending()
		tw.writeStepDiagnostic(d)
		if tw.runtimeQuirks == nil {
			tw.runtimeQuirks = map[string]bool{}
		}
		tw.runtimeQuirks[d.Code] = true
	})
}

func (tw *textWriter) ind(lead ...string) string {
	if len(lead) == 0 {
		return strings.Repeat(" ", indentSize)
	}
	return tw.styles.pipe.Render(lead[0]) + strings.Repeat(" ", indentSize-1)
}

func (tw *textWriter) write(format string, args ...any) {
	_, _ = fmt.Fprintf(tw.w, format, args...)
}

func (tw *textWriter) heading(text string) {
	tw.write("%s\n", tw.styles.heading.Render(text))
}

func (tw *textWriter) blank() {
	tw.write("\n")
}

func (p prefixed) detail(label, value string) {
	p.tw.write("%s%s %s\n", p.pfx, p.tw.styles.label.Render(label+":"), value)
}

func (p prefixed) status(text string) {
	p.tw.write("%s%s\n", p.pfx, text)
}

func (tw *textWriter) lineSub(label string) string {
	return tw.styles.pipe.Render("├") + " " + label
}

func (tw *textWriter) writeNestedFields(fields []field) {
	mid := prefixed{tw: tw, pfx: tw.ind("│") + tw.ind("│")}
	end := prefixed{tw: tw, pfx: tw.ind("│") + tw.ind("╵")}
	for i, f := range fields {
		if i == len(fields)-1 {
			end.detail(f.label, f.value)
		} else {
			mid.detail(f.label, f.value)
		}
	}
}

func (tw *textWriter) writeEmittedCb(streamID, eventType, data, metadata string, isJSON, isLink bool) {
	// Flush the deferred event header so emitted events nest under their
	// own event, not before the header.
	tw.flushPending()
	em := tw.styles.emitted
	hasData := data != ""
	hasMeta := metadata != ""

	if isLink {
		tw.write("%s\n", tw.lineSub(em.Render("linked")))
		fields := []field{
			{"stream", streamID},
		}
		if hasData {
			fields = append(fields, field{"data", displayJSON(json.RawMessage(data))})
		}
		if hasMeta {
			fields = append(fields, field{"metadata", displayJSON(json.RawMessage(metadata))})
		}
		tw.writeNestedFields(fields)
	} else {
		tw.write("%s\n", tw.lineSub(em.Render("emitted")))
		fields := []field{
			{"stream", streamID},
			{"type", eventType},
		}
		if hasData {
			fields = append(fields, field{"data", displayJSON(json.RawMessage(data))})
		}
		if hasMeta {
			fields = append(fields, field{"metadata", displayJSON(json.RawMessage(metadata))})
		}
		tw.writeNestedFields(fields)
	}
}

func (tw *textWriter) WriteInfo(proj *engine.Projection, info gafferruntime.ProjectionInfo) {
	tw.heading(proj.Def.Name)

	if info.AllStreams {
		tw.detail("Source", "$all")
	} else if len(info.Categories) > 0 {
		tw.detail("Source", "category "+strings.Join(info.Categories, ", "))
	} else if len(info.Streams) > 0 {
		tw.detail("Source", "streams "+strings.Join(info.Streams, ", "))
	}

	if info.ByStreams {
		tw.detail("Partitioning", "per stream")
	} else if info.ByCustomPartitions {
		tw.detail("Partitioning", "custom key")
	}

	if len(info.Events) > 0 {
		tw.detail("Events", strings.Join(info.Events, ", "))
	}

	if info.BiState {
		tw.detail("BiState", "yes")
	}
	if info.ProducesResults {
		tw.detail("Produces results", "yes")
	}
	if info.EmitsEvents {
		tw.detail("Emits events", "yes")
	}

	if proj.EngineVersion != 0 {
		tw.detail("Engine", fmt.Sprintf("v%d", proj.EngineVersion))
	}

	if proj.QuirksVersion != "" {
		tw.detail("Quirks", proj.QuirksVersion)
	} else {
		tw.detail("Quirks", "unversioned (matching all KurrentDB quirks)")
	}

	tw.blank()

	for _, d := range info.Diagnostics {
		tw.writeDiagnostic(d)
		if strings.HasPrefix(d.Code, "quirk.") {
			tw.compileQuirks = append(tw.compileQuirks, d.Code)
		}
	}
}

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
		return tw.styles.added.Render(fmt.Sprintf("%d", e.Local.EngineVersion))
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

// WriteStatus renders a single projection's status as a detail block: its
// runtime state (when deployed) and how it compares to local.
func (tw *textWriter) WriteStatus(e statusEntry) {
	tw.heading(e.Name)
	if e.runtime != nil {
		tw.detail("State", tw.runtimeStateStyle(e).Render(runtimeStateText(e)))
		tw.detail("Progress", progressText(e))
		if e.runtime.Position != "" {
			tw.detail("Position", e.runtime.Position)
		}
		if e.runtime.State == remote.StateFaulted && e.runtime.FaultReason != "" {
			tw.detail("Fault", tw.styles.errDetail.Render(e.runtime.FaultReason))
		}
	}
	tw.detail("Drift", tw.driftStyle(e.State).Render(driftBlockText(e.State)))
	if e.State == driftInvalid && e.LocalErr != nil {
		tw.blank()
		tw.write("%s\n", tw.styles.errDetail.Render(e.LocalErr.Error()))
	}
}

// driftBlockText spells out the one-sided verdicts for the single-projection
// block (the table keeps the terse driftText), matching gaffer diff's phrasing.
func driftBlockText(d driftState) string {
	switch d {
	case driftNotDeployed:
		return "not deployed (local only)"
	case driftUntracked:
		return "untracked (deployed, not in gaffer.toml)"
	case driftInvalid:
		return "invalid (local definition)"
	default:
		return driftText(d)
	}
}

// WriteStatusTable renders all projections as a borderless aligned table. The
// cell text is plain; colour is applied per cell by the StyleFunc keying off the
// row's entry, so lipgloss's ANSI-aware width keeps the columns aligned.
func (tw *textWriter) WriteStatusTable(entries []statusEntry) {
	const pad = 3
	t := table.New().
		BorderTop(false).BorderBottom(false).BorderLeft(false).BorderRight(false).
		BorderColumn(false).BorderRow(false).BorderHeader(false).
		Headers("PROJECTION", "STATE", "PROGRESS", "DRIFT").
		StyleFunc(func(row, col int) lipgloss.Style {
			if row == table.HeaderRow {
				return tw.styles.heading.PaddingRight(pad)
			}
			switch col {
			case 1:
				return tw.runtimeStateStyle(entries[row]).PaddingRight(pad)
			case 3:
				return tw.driftStyle(entries[row].State).PaddingRight(pad)
			default:
				return tw.styles.emitted.PaddingRight(pad)
			}
		})
	for _, e := range entries {
		t.Row(e.Name, runtimeStateText(e), progressText(e), driftText(e.State))
	}
	// Trim the column padding the last cell leaves as trailing whitespace (plain
	// in piped output; invisible inside the colour codes on a terminal).
	for _, line := range strings.Split(strings.TrimRight(t.String(), "\n"), "\n") {
		tw.write("%s\n", strings.TrimRight(line, " "))
	}
	for _, e := range entries {
		if e.State == driftDrifted {
			tw.write("\n%s\n", tw.styles.pipe.Render("Drifted - run gaffer diff <projection> to see what changed."))
			break
		}
	}
}

func runtimeStateText(e statusEntry) string {
	if e.runtime == nil {
		return "-"
	}
	return string(e.runtime.State)
}

func progressText(e statusEntry) string {
	if e.runtime == nil {
		return "-"
	}
	if e.runtime.Progress < 0 {
		return "unknown"
	}
	return fmt.Sprintf("%.0f%%", e.runtime.Progress)
}

func (tw *textWriter) runtimeStateStyle(e statusEntry) lipgloss.Style {
	if e.runtime == nil {
		return tw.styles.emitted
	}
	switch e.runtime.State {
	case remote.StateRunning:
		return tw.styles.added
	case remote.StateFaulted:
		return tw.styles.errStatus
	default:
		return tw.styles.emitted
	}
}

func driftText(d driftState) string {
	switch d {
	case driftInSync:
		return "in sync"
	case driftDrifted:
		return "drifted"
	case driftNotDeployed:
		return "not deployed"
	case driftUntracked:
		return "untracked"
	case driftInvalid:
		return "invalid"
	default:
		return string(d)
	}
}

func (tw *textWriter) driftStyle(d driftState) lipgloss.Style {
	switch d {
	case driftInSync:
		return tw.styles.added
	case driftInvalid:
		return tw.styles.errStatus
	default:
		return tw.styles.warning
	}
}

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
		nameWidth = max(nameWidth, len(r.name))
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

// diagnosticAnchor is the docs heading slug for a code: github-slugger's
// lowercase, dot-stripped form (quirk.log.multiParam -> quirklogmultiparam).
// It must match the Starlight heading slug so the CLI links to the same anchor
// a reader gets by copying the heading's own anchor link.
func diagnosticAnchor(code string) string {
	return strings.ToLower(strings.ReplaceAll(code, ".", ""))
}

// linkCode wraps a diagnostic code in an OSC 8 hyperlink to its docs anchor on
// interactive terminals; elsewhere it returns the code unchanged.
func (tw *textWriter) linkCode(code string) string {
	if !tw.links {
		return code
	}
	return termenv.Hyperlink(diagnosticsReferenceURL+"#"+diagnosticAnchor(code), code)
}

func (tw *textWriter) writeDiagnostic(d gafferruntime.Diagnostic) {
	header := fmt.Sprintf("[%s] %s", severityLabel(d.Severity), tw.linkCode(d.Code))
	if d.Range != nil {
		header += fmt.Sprintf(" (line %d, col %d)", d.Range.Start.Line, d.Range.Start.Column)
	}
	tw.write("%s\n", tw.severityStyle(d.Severity).Render(header))
	tw.write("%s%s\n\n", tw.ind(), d.Message)
}

// writeStepDiagnostic renders a runtime quirk as a per-event item in the same
// ├ flow as logs and emits - it streams at the point it fires - with the
// styled [severity] code header and its message on a continuation line. No
// source range; runtime quirks are value-dependent, not tied to a location.
func (tw *textWriter) writeStepDiagnostic(d gafferruntime.Diagnostic) {
	header := fmt.Sprintf("[%s] %s", severityLabel(d.Severity), tw.linkCode(d.Code))
	tw.write("%s\n", tw.lineSub(tw.severityStyle(d.Severity).Render(header)))
	tw.write("%s%s\n", tw.ind("│"), d.Message)
}

func severityLabel(s gafferruntime.DiagnosticSeverity) string {
	switch s {
	case gafferruntime.DiagnosticSeverityError:
		return "error"
	case gafferruntime.DiagnosticSeverityWarning:
		return "warning"
	case gafferruntime.DiagnosticSeverityInformation:
		return "info"
	default:
		return "diagnostic"
	}
}

func (tw *textWriter) severityStyle(s gafferruntime.DiagnosticSeverity) lipgloss.Style {
	switch s {
	case gafferruntime.DiagnosticSeverityError:
		return tw.styles.errStatus
	case gafferruntime.DiagnosticSeverityWarning:
		return tw.styles.warning
	default:
		return tw.styles.info
	}
}

func (tw *textWriter) WriteDebugListening(addr string, port int) {}

func (tw *textWriter) WriteEvent(event eventInfo) {
	// Defer the actual print until we know the result. Skipped events
	// won't render at all; processed / errored ones get flushed by
	// WriteResult / WriteError.
	tw.pending = &event
}

// flushPending prints the deferred event header, at most once per event.
//
// WriteEvent defers the header (rather than printing it immediately) so a
// skipped event renders nothing: in live mode WriteResult drops the block
// entirely on a "skipped" result. The header is therefore shown lazily by
// whatever produces the event's first visible output - a log (OnLog), an
// emitted event (writeEmittedCb), or the result itself (WriteResult) - so it
// always lands above them in order. Skips are decided before the handler
// runs, so a dropped event never has logs or emits and never flushes here.
func (tw *textWriter) flushPending() {
	if tw.pending == nil {
		return
	}
	event := *tw.pending
	tw.pending = nil
	tw.heading(event.ID())
	tw.line.detail("type", event.EventType)
	if hasContent(event.Data) {
		tw.line.detail("data", displayJSON(event.Data))
	}
	if hasContent(event.Metadata) {
		tw.line.detail("metadata", displayJSON(event.Metadata))
	}
}

func (tw *textWriter) WriteResult(_ string, result *gafferruntime.FeedResult) {
	if result.Status == "skipped" {
		if !tw.showSkipped {
			// Live mode: drop the entire event block. Skipped is
			// runtime hygiene noise (link metadata, system events).
			tw.pending = nil
			return
		}
		// Fixture mode: surface the skip with its reason - the user
		// curated the events, so a skip is diagnostic.
		tw.flushPending()
		tw.corner.status(tw.styles.skipped.Render("skipped"))
		if result.SkipReason != "" {
			tw.detail("reason", result.SkipReason)
		}
		tw.blank()
		return
	}
	tw.flushPending()
	tw.corner.status(tw.styles.processed.Render("processed"))
	if result.Partition != "" {
		tw.detail("partition", result.Partition)
	}
	if hasContent(result.State) {
		tw.detail("state", string(result.State))
	}
	tw.blank()
}

func (tw *textWriter) WriteError(_ string, code, description string) {
	tw.flushPending()
	tw.corner.status(tw.styles.errStatus.Render(code))
	tw.write("%s%s\n", tw.ind(), tw.styles.errDetail.Render(description))
	tw.blank()
}

func (tw *textWriter) WriteFatalError(fe fatalError) {
	// Fall back to stdout if no stderr was provided - fatal errors should
	// never be silently dropped.
	out := tw.errW
	if out == nil {
		out = tw.w
	}
	_, _ = fmt.Fprintf(out, "\n%s\n%s\n", tw.styles.errStatus.Render(fe.Code), fe.Description)
	if fe.Line != nil {
		col := 0
		if fe.Column != nil {
			col = *fe.Column
		}
		_, _ = fmt.Fprintf(out, "  at %s:%d:%d\n", fe.File, *fe.Line, col)
	}
	if fe.JsStack != "" {
		_, _ = fmt.Fprintln(out, fe.JsStack)
	}
	tw.writeCompatBlock(out, fe)
}

func (tw *textWriter) WriteRunError(_, description string) {
	out := tw.errW
	if out == nil {
		out = tw.w
	}
	_, _ = fmt.Fprintf(out, "\n%s\n%s\n", tw.styles.errStatus.Render("ERROR"), description)
}

func (tw *textWriter) WriteAuthRequired(env string) {
	out := tw.errW
	if out == nil {
		out = tw.w
	}
	_, _ = fmt.Fprintf(out, "\n%s\n", tw.styles.errStatus.Render("Authentication required"))
	_, _ = fmt.Fprintf(out, "Run `gaffer auth --env %s` to sign in.\n", env)
}

// writeCompatBlock renders the "Compat: <code>" hint when the fatal error
// was driven by an upstream-quirk-compat code path. Reads the enriched
// description + fixedIn fields straight off the error (the runtime supplies
// them inline now - no registry round-trip). Stays terse: state the fact
// ("Fixed in KurrentDB X") rather than prescribe ("bump your version").
func (tw *textWriter) writeCompatBlock(out io.Writer, fe fatalError) {
	if fe.CompatCode == "" {
		return
	}
	style := tw.styles.warning
	_, _ = fmt.Fprintf(out, "\n%s %s\n", style.Render("Compat:"), fe.CompatCode)
	// The runtime enriches these only for codes it found in the catalogue; absent
	// them (an out-of-catalogue code) we just name the code rather than assert a
	// behaviour we can't back up.
	if fe.CompatDescription == "" {
		return
	}
	_, _ = fmt.Fprintf(out, "  %s\n", fe.CompatDescription)
	if fe.CompatFixedIn != "" {
		_, _ = fmt.Fprintf(out, "  Fixed in KurrentDB %s.\n", fe.CompatFixedIn)
	} else {
		_, _ = fmt.Fprintln(out, "  Current KurrentDB behaviour.")
	}
}

func (tw *textWriter) statsLine(stats engine.EventStats) {
	gold := tw.styles.skipped.Bold(true).Render
	line := fmt.Sprintf("%s events processed", gold(formatNumber(stats.Handled)))
	if stats.Errors > 0 {
		line += fmt.Sprintf(", %s errors", gold(formatNumber(stats.Errors)))
	}
	tw.write("%s\n", line)

	// Fixture mode: surface skipped with a per-reason breakdown
	// underneath. The user picked these events, so each skip line
	// answers a "why didn't this run?" question.
	if tw.showSkipped && stats.Skipped > 0 {
		tw.write("%s events skipped\n", gold(formatNumber(stats.Skipped)))
		// Stable order so rerun output diffs cleanly.
		reasons := slices.Sorted(maps.Keys(stats.SkippedByReason))
		for _, r := range reasons {
			tw.write("  %s %s\n", gold(formatNumber(stats.SkippedByReason[r])), describeSkipReason(r))
		}
	}

	// Every distinct quirk the run surfaced - compile-time (from the info
	// header) and runtime (per-event) - listed together. Non-fatal, so kept
	// separate from skips and errors.
	seen := map[string]bool{}
	for _, c := range tw.compileQuirks {
		seen[c] = true
	}
	for c := range tw.runtimeQuirks {
		seen[c] = true
	}
	if len(seen) > 0 {
		codes := slices.Sorted(maps.Keys(seen))
		noun := "quirks"
		if len(codes) == 1 {
			noun = "quirk"
		}
		tw.write("%s %s encountered\n", gold(formatNumber(len(codes))), noun)
		for _, c := range codes {
			tw.write("%s%s\n", tw.ind(), tw.styles.warning.Render(tw.linkCode(c)))
		}
		tw.write("%sSee %s\n", tw.ind(), tw.styles.label.Render(diagnosticsReferenceURL))
	}
}

// describeSkipReason maps the runtime's SkipReason tags to short
// user-readable phrases. Falls back to the raw tag if unrecognised
// so a future runtime addition still renders sensibly.
func describeSkipReason(reason string) string {
	switch reason {
	case "unhandled":
		return "no handler for this event type"
	case "no-handler":
		return "no handler returned a result"
	case "no-partition":
		return "partitionBy returned null"
	case "link":
		return "link event ($includeLinks not set)"
	case "no-delete-handler":
		return "stream deletion (no $deleted handler)"
	case "non-json":
		return "non-JSON event (V1 only)"
	default:
		return reason
	}
}

func (tw *textWriter) WriteSummary(stats engine.EventStats, state engine.StateSummary) {
	tw.statsLine(stats)
	tw.blank()

	if state.HasBiState && hasContent(state.SharedState) {
		tw.detail("Shared state", string(state.SharedState))
	}

	if state.Partitioned {
		for partition, data := range state.Partitions {
			tw.heading(partition)
			if hasContent(data.State) {
				tw.detail("state", string(data.State))
			}
			if state.HasTransforms && hasContent(data.Result) {
				tw.detail("result", string(data.Result))
			}
		}
	} else {
		if hasContent(state.State) {
			tw.detail("State", string(state.State))
		}
		if state.HasTransforms && hasContent(state.Result) {
			tw.detail("Result", string(state.Result))
		}
	}
}
