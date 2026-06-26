package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"slices"

	gafferruntime "github.com/kurrent-io/gaffer/bindings/go"
	"github.com/kurrent-io/gaffer/cli/internal/engine"
)

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
	line := gold(formatNumber(stats.Handled)) + " events processed"
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
