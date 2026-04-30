package cmd

import (
	"encoding/json"
	"fmt"
	"strings"

	gafferruntime "github.com/kurrent-io/gaffer/bindings/go"
	"github.com/kurrent-io/gaffer/cli/internal/engine"
)

type eventInfo = engine.EventEnvelope

func parseEventInfo(eventJSON string) eventInfo {
	return engine.ParseEvent(eventJSON)
}

type outputWriter interface {
	WriteInfo(name string, info gafferruntime.ProjectionInfo, engineVersion int)
	WriteDebugListening(addr string, port int)
	WriteEvent(event eventInfo)
	WriteResult(eventID string, result *gafferruntime.FeedResult)
	WriteError(eventID string, code string, description string)
	WriteSummary(stats engine.EventStats, state engine.StateSummary)
}

type sessionCallbacks interface {
	OnEmit(cb gafferruntime.EmitCallback)
	OnLog(cb gafferruntime.LogCallback)
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
