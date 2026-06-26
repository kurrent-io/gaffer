package mcpserver

import (
	"context"
	"errors"

	"github.com/kurrent-io/gaffer/cli/internal/cliout"
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
	Annotations: readOnlyHints(),
}

type infoInput struct {
	Name string `json:"name,omitempty" jsonschema:"Projection name. Defaults to the only configured projection when one exists."`
}

func (s *Server) handleInfo(_ context.Context, _ *mcp.CallToolRequest, in infoInput) (*mcp.CallToolResult, any, error) {
	cfg, root, r := s.requireProject()
	if r != nil {
		return r, nil, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	name := in.Name
	if name == "" {
		switch len(cfg.Projection) {
		case 0:
			return toolError("no projections configured in gaffer.toml"), nil, nil
		case 1:
			name = cfg.Projection[0].Name
		default:
			return toolError("name required: project has %d projections; call list_projections to discover names", len(cfg.Projection)), nil, nil
		}
	}

	compiled, err := s.compileProjection(cfg, root, name, false)
	if err != nil {
		var notFound engine.ProjectionNotFoundError
		if errors.As(err, &notFound) {
			return toolError("projection %q not found in gaffer.toml; call list_projections to discover names", name), nil, nil
		}
		// compileProjection already recorded a runtime projection error
		// into projection_errors_seen; every other phase is surfaced bare.
		return toolError("%v", err), nil, nil
	}
	defer compiled.Session.Destroy()

	return toolResult(cliout.BuildInfoJSON(compiled.Projection, compiled.Info)), nil, nil
}
