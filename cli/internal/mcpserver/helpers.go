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

	var result map[string]any
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
	// gaffer://docs/quirks resource. Omitted when none fired. Pulled from the
	// already-parsed result rather than re-parsing ResultJSON.
	if diags, ok := result["diagnostics"].([]any); ok && len(diags) > 0 {
		out["diagnostics"] = diags
	}
	return out
}

func extractState(resultJSON string) json.RawMessage {
	var obj struct {
		State json.RawMessage `json:"state"`
	}
	_ = json.Unmarshal([]byte(resultJSON), &obj)
	return obj.State
}

// readOnlyHints is the annotation shared by the query tools - the
// get_*/list_* readers and validate. None of them create or mutate a
// session, write files, or change server-side state, so a client can
// surface them without a write-confirmation gate; repeating a call has no
// additional effect, so they are idempotent too. Reading from KurrentDB
// (as list_events does) is still read-only.
func readOnlyHints() *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{
		ReadOnlyHint:   true,
		IdempotentHint: true,
	}
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

// putStateError records a CollectState failure on a summary map as a soft
// field. The run/debug/wait paths embed state as one field among
// completed/processed/errors: the run itself succeeded, only the state read
// failed, so failing the whole tool call would hide the counts the caller
// wants. get_state hard-errors instead (toolError) because state is its entire
// payload. Omitted when err is nil.
func putStateError(summary map[string]any, err error) {
	if err != nil {
		summary["stateError"] = err.Error()
	}
}

func toolError(format string, args ...any) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf(format, args...)}},
		IsError: true,
	}
}
