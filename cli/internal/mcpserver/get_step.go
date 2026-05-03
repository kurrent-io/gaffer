package mcpserver

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var getStepTool = &mcp.Tool{
	Name:        "get_step",
	Description: "Get full detail for a specific step in the active session's history. Returns the event, status, state, emitted events, and logs.",
}

type getStepInput struct {
	Step int64 `json:"step" jsonschema:"Step number (1-based) from the session history"`
}

func (s *Server) handleGetStep(_ context.Context, _ *mcp.CallToolRequest, input getStepInput) (*mcp.CallToolResult, any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	sess, errResult := s.requireSession()
	if errResult != nil {
		return errResult, nil, nil
	}

	step, err := sess.runner.GetStep(input.Step)
	if err != nil {
		return toolError("querying history: %v", err), nil, nil
	}
	if step == nil {
		return toolError("no step at %d", input.Step), nil, nil
	}

	return toolResult(formatStep(step)), nil, nil
}
