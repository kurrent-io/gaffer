package mcpserver

import (
	"context"
	"encoding/json"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var getHistoryTool = &mcp.Tool{
	Name:        "get_history",
	Description: "Get state snapshots and a compact step summary between two steps. Returns the projection state before the range, the state after the range, and timeline entries for each step in between. Use get_step for full event/result detail at a specific step.",
}

type getHistoryInput struct {
	From      int64  `json:"from" jsonschema:"Start step (inclusive). Defaults to 1 if 0."`
	To        int64  `json:"to" jsonschema:"End step (inclusive). Defaults to last step if 0."`
	Partition string `json:"partition,omitempty" jsonschema:"Filter to a specific partition key"`
}

func (s *Server) handleGetHistory(_ context.Context, _ *mcp.CallToolRequest, input getHistoryInput) (*mcp.CallToolResult, any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	sess, errResult := s.requireSession()
	if errResult != nil {
		return errResult, nil, nil
	}

	from, to := s.resolveRange(input.From, input.To)

	var beforeState json.RawMessage
	if from > 1 {
		beforeStep, err := sess.runner.GetStep(from - 1)
		if err == nil && beforeStep != nil {
			beforeState = extractState(beforeStep.ResultJSON)
		}
	}

	afterStep, err := sess.runner.GetStep(to)
	if err != nil {
		return toolError("querying history: %v", err), nil, nil
	}

	var afterState json.RawMessage
	if afterStep != nil {
		afterState = extractState(afterStep.ResultJSON)
	}

	timeline, err := sess.runner.TimelineFiltered(from, to, input.Partition)
	if err != nil {
		return toolError("querying timeline: %v", err), nil, nil
	}

	return toolResult(map[string]any{
		"before": beforeState,
		"steps":  timeline,
		"after":  afterState,
	}), nil, nil
}
