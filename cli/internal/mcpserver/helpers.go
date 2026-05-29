package mcpserver

import (
	"encoding/json"
	"fmt"

	"github.com/kurrent-io/gaffer/cli/internal/history"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func (s *Server) resolveRange(from, to int64) (int64, int64) {
	minStep, maxStep, _ := s.session.runner.HistoryRange()
	if from <= 0 {
		from = minStep
	}
	if to <= 0 {
		to = maxStep
	}
	if from < minStep {
		from = minStep
	}
	if to < from {
		to = from
	}
	return from, to
}

func formatStep(step *history.Step) map[string]any {
	var event any
	_ = json.Unmarshal([]byte(step.EventJSON), &event)

	var result any
	_ = json.Unmarshal([]byte(step.ResultJSON), &result)

	out := map[string]any{
		"step":      step.Index,
		"eventType": step.EventType,
		"streamId":  step.StreamID,
		"status":    step.Status,
		"partition": step.Partition,
		"event":     event,
		"result":    result,
	}
	// Promote the runtime quirks that fired to a top-level field so the
	// assistant sees them without digging into the full result. Each is a
	// full Diagnostic object; cross-reference its code against the
	// gaffer://docs/quirks resource. Omitted when none fired.
	if diags := extractDiagnostics(step.ResultJSON); len(diags) > 0 {
		out["diagnostics"] = diags
	}
	return out
}

func extractDiagnostics(resultJSON string) []json.RawMessage {
	var obj struct {
		Diagnostics []json.RawMessage `json:"diagnostics"`
	}
	_ = json.Unmarshal([]byte(resultJSON), &obj)
	return obj.Diagnostics
}

func extractState(resultJSON string) json.RawMessage {
	var obj struct {
		State json.RawMessage `json:"state"`
	}
	_ = json.Unmarshal([]byte(resultJSON), &obj)
	return obj.State
}

func toolResult(data any) *mcp.CallToolResult {
	jsonBytes, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("error marshaling result: %v", err)}},
			IsError: true,
		}
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(jsonBytes)}},
	}
}

func toolError(format string, args ...any) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf(format, args...)}},
		IsError: true,
	}
}
