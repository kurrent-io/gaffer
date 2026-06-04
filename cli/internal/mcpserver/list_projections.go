package mcpserver

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var listProjectionsTool = &mcp.Tool{
	Name:        "list_projections",
	Description: "List all projections defined in the project's gaffer.toml.",
}

type listProjectionsInput struct{}

func (s *Server) handleListProjections(_ context.Context, _ *mcp.CallToolRequest, _ listProjectionsInput) (*mcp.CallToolResult, any, error) {
	cfg, root, r := s.requireProject()
	if r != nil {
		return r, nil, nil
	}

	projections := []map[string]any{}
	for _, proj := range cfg.Projection {
		projections = append(projections, map[string]any{
			"name":          proj.Name,
			"entry":         proj.Entry,
			"engineVersion": cfg.EffectiveEngineVersion(&proj),
		})
	}

	return toolResult(map[string]any{
		"projections": projections,
		"root":        root,
	}), nil, nil
}
