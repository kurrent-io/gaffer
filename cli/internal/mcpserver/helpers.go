package mcpserver

import (
	"encoding/json"
	"fmt"

	"github.com/kurrent-io/gaffer/cli/internal/history"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func (s *Server) resolveRange(from, to int64) (int64, int64) {
	minPos, maxPos, _ := s.session.runner.HistoryRange()
	if from <= 0 {
		from = minPos
	}
	if to <= 0 {
		to = maxPos
	}
	if from < minPos {
		from = minPos
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

	return map[string]any{
		"position":  step.Position,
		"eventType": step.EventType,
		"streamId":  step.StreamID,
		"status":    step.Status,
		"partition": step.Partition,
		"event":     event,
		"result":    result,
	}
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
