package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	gafferruntime "github.com/kurrent-io/gaffer/bindings/go"
	"github.com/kurrent-io/gaffer/cli/internal/deploy"
	"github.com/kurrent-io/gaffer/cli/internal/engine"
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
	logLabel  lipgloss.Style
	emitted   lipgloss.Style
	processed lipgloss.Style
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
			logLabel:  r.NewStyle().Foreground(lipgloss.Color("4")),
			emitted:   r.NewStyle(),
			processed: r.NewStyle().Faint(true).Foreground(lipgloss.Color("2")),
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
func (tw *textWriter) WriteDiff(e diffEntry) {
	tw.heading(e.Name)
	switch e.State {
	case stateNotDeployed:
		tw.status(tw.styles.warning.Render("not deployed (local only)"))
	case stateUntracked:
		tw.status(tw.styles.warning.Render("untracked (deployed, not in gaffer.toml)"))
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

// The per-dimension helpers show the value when local and deployed agree (a
// single value implies in sync) and the change when they differ. The query has
// no scalar value, so it shows "in sync" or a +added -removed line stat; the
// full source diff is the external viewer's job.

func (tw *textWriter) queryStatus(e diffEntry) string {
	if !e.Cmp.QueryDiffers {
		return "in sync"
	}
	added, removed := deploy.LineStat(e.Deployed.Query, e.Local.Query)
	return tw.styles.processed.Render(fmt.Sprintf("+%d", added)) + " " +
		tw.styles.errDetail.Render(fmt.Sprintf("-%d", removed))
}

func (tw *textWriter) versionStatus(e diffEntry) string {
	if !e.Cmp.EngineVersionDiffers {
		return fmt.Sprintf("%d", e.Local.EngineVersion)
	}
	return tw.styles.warning.Render(fmt.Sprintf("remote %d, local %d", e.Deployed.EngineVersion, e.Local.EngineVersion))
}

func (tw *textWriter) flagStatus(differs, remote, local bool) string {
	if !differs {
		return enabledStr(local)
	}
	return tw.styles.warning.Render(fmt.Sprintf("remote %s, local %s", enabledStr(remote), enabledStr(local)))
}

func enabledStr(b bool) string {
	if b {
		return "enabled"
	}
	return "disabled"
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
		reasons := make([]string, 0, len(stats.SkippedByReason))
		for r := range stats.SkippedByReason {
			reasons = append(reasons, r)
		}
		sort.Strings(reasons)
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
		codes := make([]string, 0, len(seen))
		for c := range seen {
			codes = append(codes, c)
		}
		sort.Strings(codes)
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
