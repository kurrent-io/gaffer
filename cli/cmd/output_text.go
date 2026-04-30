package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/lipgloss"
	gafferruntime "github.com/kurrent-io/gaffer/bindings/go"
	"github.com/kurrent-io/gaffer/cli/internal/engine"
)

const indentSize = 3

type field struct{ label, value string }

type textWriter struct {
	prefixed
	w      io.Writer
	line   prefixed
	corner prefixed
	styles textStyles
}

type textStyles struct {
	label     lipgloss.Style
	pipe      lipgloss.Style
	emitted   lipgloss.Style
	processed lipgloss.Style
	skipped   lipgloss.Style
	errStatus lipgloss.Style
	errDetail lipgloss.Style
	heading   lipgloss.Style
}

type prefixed struct {
	tw  *textWriter
	pfx string
}

func newTextWriter(w io.Writer) *textWriter {
	r := lipgloss.NewRenderer(w)
	tw := &textWriter{
		w: w,
		styles: textStyles{
			label:     r.NewStyle().Foreground(lipgloss.Color("6")),
			pipe:      r.NewStyle().Faint(true).Foreground(lipgloss.Color("6")),
			emitted:   r.NewStyle(),
			processed: r.NewStyle().Faint(true).Foreground(lipgloss.Color("2")),
			skipped:   r.NewStyle().Foreground(lipgloss.Color("3")),
			errStatus: r.NewStyle().Foreground(lipgloss.Color("9")),
			errDetail: r.NewStyle().Foreground(lipgloss.Color("1")),
			heading:   r.NewStyle().Bold(true),
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
		tw.write("%s %s\n", tw.lineSub(tw.styles.skipped.Render("[log]")), message)
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

func (tw *textWriter) WriteInfo(name string, info gafferruntime.ProjectionInfo, engineVersion int) {
	tw.heading(name)

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

	if engineVersion != 0 {
		tw.detail("Engine", fmt.Sprintf("v%d", engineVersion))
	}

	tw.blank()
}

func (tw *textWriter) WriteDebugListening(addr string, port int) {}

func (tw *textWriter) WriteEvent(event eventInfo) {
	tw.heading(event.ID())
	tw.line.detail("type", event.EventType)

	if hasContent(event.Data) {
		tw.line.detail("data", displayJSON(event.Data))
	}
	if hasContent(event.Metadata) {
		tw.line.detail("metadata", displayJSON(event.Metadata))
	}
}

func (tw *textWriter) WriteResult(eventID string, result *gafferruntime.FeedResult) {
	s := tw.styles
	if result.Status == "processed" {
		tw.corner.status(s.processed.Render("processed"))
		if result.Partition != "" {
			tw.detail("partition", result.Partition)
		}
		if hasContent(result.State) {
			tw.detail("state", string(result.State))
		}
	} else {
		tw.corner.status(s.skipped.Render("skipped"))
		tw.detail("reason", result.SkipReason)
	}

	tw.blank()
}

func (tw *textWriter) WriteError(eventID string, code, description string) {
	tw.corner.status(tw.styles.errStatus.Render(code))
	tw.write("%s%s\n", tw.ind(), tw.styles.errDetail.Render(description))
	tw.blank()
}

func (tw *textWriter) statsLine(stats engine.EventStats) {
	gold := tw.styles.skipped.Bold(true).Render
	line := fmt.Sprintf("%s events processed (%s handled, %s skipped",
		gold(formatNumber(stats.Total())), gold(formatNumber(stats.Handled)), gold(formatNumber(stats.Skipped)))
	if stats.Errors > 0 {
		line += fmt.Sprintf(", %s errors", gold(formatNumber(stats.Errors)))
	}
	tw.write("%s)\n", line)
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
