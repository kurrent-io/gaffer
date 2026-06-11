package mcpserver

import (
	"context"
	"encoding/json"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var getStateTool = &mcp.Tool{
	Name:        "get_state",
	Description: "Get the current projection state from the active session. Returns state per partition (or global state if unpartitioned), shared state if biState, and result if transforms are defined.",
}

type getStateInput struct {
	Partition string `json:"partition,omitempty" jsonschema:"Get state for a specific partition. Omit for all partitions or global state."`
}

func (s *Server) handleGetState(_ context.Context, _ *mcp.CallToolRequest, input getStateInput) (*mcp.CallToolResult, any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	sess, errResult := s.requireSession()
	if errResult != nil {
		return errResult, nil, nil
	}

	if input.Partition != "" {
		result := map[string]any{"partition": input.Partition}
		state, r, err := sess.runner.GetPartitionState(input.Partition)
		if err != nil {
			return toolError("%v", err), nil, nil
		}
		if state != nil {
			result["state"] = json.RawMessage(*state)
		}
		if r != nil {
			result["result"] = json.RawMessage(*r)
		}
		return toolResult(result), nil, nil
	}

	summary, err := sess.runner.CollectState()
	if err != nil {
		return toolError("%v", err), nil, nil
	}
	return toolResult(summary.ToMap()), nil, nil
}
