package mcpserver

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var getTimelineTool = &mcp.Tool{
	Name:        "get_timeline",
	Description: "Get a compact overview of a range of steps. Returns step number, event type, stream ID, status, emit/log flags, and the codes of any runtime quirks that fired (quirks) for each step. Use this to scan for interesting steps - including fired quirks - then drill in with get_step.",
}

type getTimelineInput struct {
	From      int64  `json:"from" jsonschema:"Start step (inclusive). Defaults to 1 if 0."`
	To        int64  `json:"to" jsonschema:"End step (inclusive). Defaults to last step if 0."`
	Partition string `json:"partition,omitempty" jsonschema:"Filter to a specific partition key"`
}

func (s *Server) handleGetTimeline(_ context.Context, _ *mcp.CallToolRequest, input getTimelineInput) (*mcp.CallToolResult, any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	sess, errResult := s.requireSession()
	if errResult != nil {
		return errResult, nil, nil
	}

	_, maxStep, err := sess.runner.HistoryRange()
	if err != nil {
		return toolError("querying timeline: %v", err), nil, nil
	}
	if maxStep == 0 {
		// No steps recorded - e.g. a live run that caught up or timed out
		// without processing an event. Report it plainly instead of
		// returning a bare empty range.
		return toolResult(map[string]any{
			"entries": []any{},
			"message": "No timeline recorded for this session.",
		}), nil, nil
	}

	from, to := s.resolveRange(input.From, input.To)

	entries, err := sess.runner.TimelineFiltered(from, to, input.Partition)
	if err != nil {
		return toolError("querying timeline: %v", err), nil, nil
	}

	return toolResult(map[string]any{
		"entries": entries,
	}), nil, nil
}
