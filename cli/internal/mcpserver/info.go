package mcpserver

import (
	"context"

	"github.com/kurrent-io/gaffer/cli/internal/engine"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var infoTool = &mcp.Tool{
	Name: "get_projection_info",
	Description: "Show details for a projection: parsed structure, sources, " +
		"partition mode, emit declarations, effective engine version. " +
		"Mirrors `gaffer info <name> --json`. When the project has a single " +
		"configured projection, `name` may be omitted; call list_projections " +
		"to discover names otherwise.",
	Annotations: &mcp.ToolAnnotations{
		ReadOnlyHint:   true,
		IdempotentHint: true,
	},
}

type infoInput struct {
	Name string `json:"name,omitempty" jsonschema:"Projection name. Defaults to the only configured projection when one exists."`
}

func (s *Server) handleInfo(_ context.Context, _ *mcp.CallToolRequest, in infoInput) (*mcp.CallToolResult, any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	name := in.Name
	if name == "" {
		switch len(s.cfg.Projection) {
		case 0:
			return toolError("no projections configured in gaffer.toml"), nil, nil
		case 1:
			name = s.cfg.Projection[0].Name
		default:
			return toolError("name required: project has %d projections; call list_projections to discover names", len(s.cfg.Projection)), nil, nil
		}
	}

	proj := s.cfg.FindProjection(name)
	if proj == nil {
		return toolError("projection %q not found in gaffer.toml; call list_projections to discover names", name), nil, nil
	}

	source, err := engine.ReadSource(s.root, proj.Entry)
	if err != nil {
		return toolError("%v", err), nil, nil
	}

	lp := engine.NewProjection(s.root, s.cfg, proj, source)
	session, info, err := engine.CreateSession(lp, false, false)
	if err != nil {
		return toolError("%v", err), nil, nil
	}
	defer session.Destroy()

	return toolResult(engine.BuildInfoJSON(lp, info)), nil, nil
}
