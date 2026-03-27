package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/lipgloss"
	gafferruntime "github.com/kurrent-io/gaffer/bindings/go"
)

type eventInfo struct {
	SequenceNumber int64           `json:"sequenceNumber"`
	StreamID       string          `json:"streamId"`
	EventType      string          `json:"eventType"`
	Data           json.RawMessage `json:"data"`
	Metadata       json.RawMessage `json:"metadata"`
}

func (e eventInfo) id() string {
	return fmt.Sprintf("%d@%s", e.SequenceNumber, e.StreamID)
}

func parseEventInfo(eventJSON string) eventInfo {
	var info eventInfo
	_ = json.Unmarshal([]byte(eventJSON), &info)
	return info
}

type eventStats struct {
	handled int
	skipped int
	errors  int
}

func (s eventStats) total() int {
	return s.handled + s.skipped + s.errors
}

type summaryState struct {
	partitioned   bool
	partitions    map[string]partitionData
	state         json.RawMessage
	result        json.RawMessage
	sharedState   json.RawMessage
	hasTransforms bool
	hasBiState    bool
}

type partitionData struct {
	state  json.RawMessage
	result json.RawMessage
}

type outputWriter interface {
	WriteInfo(name string, info projectionInfo, version string)
	WriteDebugListening(addr string, port int)
	WriteEvent(event eventInfo)
	WriteResult(eventID string, result *gafferruntime.FeedResult)
	WriteError(eventID string, code string, description string)
	WriteSummary(stats eventStats, state summaryState)
}

const indentSize = 3

type field struct{ label, value string }

type sessionCallbacks interface {
	OnEmit(cb gafferruntime.EmitCallback)
	OnLog(cb gafferruntime.LogCallback)
}

// --- Text output ---

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

func (tw *textWriter) WriteInfo(name string, info projectionInfo, version string) {
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

	if info.IsBiState {
		tw.detail("BiState", "yes")
	}
	if info.ProducesResults {
		tw.detail("Produces results", "yes")
	}

	if version != "" {
		tw.detail("Engine", version)
	}

	tw.blank()
}

func (tw *textWriter) WriteDebugListening(addr string, port int) {}

func (tw *textWriter) WriteEvent(event eventInfo) {
	tw.heading(event.id())
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

func (tw *textWriter) statsLine(stats eventStats) {
	gold := tw.styles.skipped.Bold(true).Render
	line := fmt.Sprintf("%s events processed (%s handled, %s skipped",
		gold(formatNumber(stats.total())), gold(formatNumber(stats.handled)), gold(formatNumber(stats.skipped)))
	if stats.errors > 0 {
		line += fmt.Sprintf(", %s errors", gold(formatNumber(stats.errors)))
	}
	tw.write("%s)\n", line)
}

func (tw *textWriter) WriteSummary(stats eventStats, state summaryState) {
	tw.statsLine(stats)
	tw.blank()

	if state.hasBiState && hasContent(state.sharedState) {
		tw.detail("Shared state", string(state.sharedState))
	}

	if state.partitioned {
		for partition, data := range state.partitions {
			tw.heading(partition)
			if hasContent(data.state) {
				tw.detail("state", string(data.state))
			}
			if state.hasTransforms && hasContent(data.result) {
				tw.detail("result", string(data.result))
			}
		}
	} else {
		if hasContent(state.state) {
			tw.detail("State", string(state.state))
		}
		if state.hasTransforms && hasContent(state.result) {
			tw.detail("Result", string(state.result))
		}
	}
}

func hasContent(raw json.RawMessage) bool {
	return len(raw) > 0 && string(raw) != "null"
}

func displayJSON(raw json.RawMessage) string {
	if len(raw) > 0 && raw[0] == '"' {
		var s string
		if json.Unmarshal(raw, &s) == nil {
			return s
		}
	}
	return string(raw)
}

func formatNumber(n int) string {
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}
	var b strings.Builder
	offset := len(s) % 3
	if offset > 0 {
		b.WriteString(s[:offset])
	}
	for i := offset; i < len(s); i += 3 {
		if b.Len() > 0 {
			b.WriteByte(',')
		}
		b.WriteString(s[i : i+3])
	}
	return b.String()
}
