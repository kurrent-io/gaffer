package cmd

import (
	"encoding/json"
	"io"

	gafferruntime "github.com/kurrent-io/gaffer/bindings/go"
	"github.com/kurrent-io/gaffer/cli/internal/engine"
)

type jsonWriter struct {
	enc *json.Encoder
}

func newJSONWriter(w io.Writer) *jsonWriter {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return &jsonWriter{enc: enc}
}

func (jw *jsonWriter) writeLine(v any) {
	_ = jw.enc.Encode(v)
}

func (jw *jsonWriter) WriteInfo(name string, info gafferruntime.QuerySources, version string) {
	src := engine.DescribeSource(info)
	proj := map[string]any{
		"name":   name,
		"source": src["type"],
		"engine": version,
	}
	if cats, ok := src["categories"]; ok {
		proj["categories"] = cats
	}
	if streams, ok := src["streams"]; ok {
		proj["streams"] = streams
	}
	if len(info.Events) > 0 {
		proj["events"] = info.Events
	}
	if p := engine.DescribePartitioning(info); p != "none" {
		proj["partitioning"] = p
	}

	jw.writeLine(map[string]any{
		"type":       "info",
		"projection": proj,
	})
}

func (jw *jsonWriter) WriteDebugListening(addr string, port int) {
	jw.writeLine(map[string]any{
		"type": "debug",
		"addr": addr,
		"port": port,
	})
}

func (jw *jsonWriter) WriteEvent(event eventInfo) {
	line := map[string]any{
		"type":           "event",
		"id":             event.ID(),
		"sequenceNumber": event.SequenceNumber,
		"streamId":       event.StreamID,
		"eventType":      event.EventType,
	}
	if hasContent(event.Data) {
		line["data"] = json.RawMessage(event.Data)
	}
	if hasContent(event.Metadata) {
		line["metadata"] = json.RawMessage(event.Metadata)
	}
	jw.writeLine(line)
}

func (jw *jsonWriter) WriteResult(eventID string, result *gafferruntime.FeedResult) {
	line := map[string]any{
		"type":    "result",
		"eventId": eventID,
		"status":  result.Status,
	}

	if result.Status == "processed" {
		if result.Partition != "" {
			line["partition"] = result.Partition
		}
		if hasContent(result.State) {
			line["state"] = json.RawMessage(result.State)
		}
		line["emitted"] = mapEmitted(result.Emitted)
		if len(result.Logs) > 0 {
			line["logs"] = result.Logs
		} else {
			line["logs"] = []string{}
		}
	} else {
		line["reason"] = result.SkipReason
		if len(result.Logs) > 0 {
			line["logs"] = result.Logs
		}
	}

	jw.writeLine(line)
}

func (jw *jsonWriter) WriteError(eventID string, code, description string) {
	jw.writeLine(map[string]any{
		"type":        "error",
		"eventId":     eventID,
		"code":        code,
		"description": description,
	})
}

func (jw *jsonWriter) WriteSummary(stats engine.EventStats, state engine.StateSummary) {
	line := map[string]any{
		"type":      "summary",
		"processed": stats.Total(),
		"handled":   stats.Handled,
		"skipped":   stats.Skipped,
		"errors":    stats.Errors,
	}

	for k, v := range state.ToMap() {
		line[k] = v
	}

	jw.writeLine(line)
}

func mapEmitted(emitted []gafferruntime.EmittedEvent) []map[string]any {
	result := make([]map[string]any, len(emitted))
	for i, e := range emitted {
		m := map[string]any{
			"streamId":  e.StreamID,
			"eventType": e.EventType,
			"isLink":    e.IsLink,
		}
		if e.Data != nil {
			if e.IsJson {
				m["data"] = json.RawMessage(*e.Data)
			} else {
				m["data"] = *e.Data
			}
		}
		if len(e.Metadata) > 0 {
			m["metadata"] = e.Metadata
		}
		result[i] = m
	}
	if len(result) == 0 {
		return []map[string]any{}
	}
	return result
}
