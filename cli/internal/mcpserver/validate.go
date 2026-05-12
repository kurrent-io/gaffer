package mcpserver

import (
	"context"
	"errors"

	gafferruntime "github.com/kurrent-io/gaffer/bindings/go"
	"github.com/kurrent-io/gaffer/cli/internal/engine"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var validateTool = &mcp.Tool{
	Name:        "validate",
	Description: "Compile and check a projection without running it. Returns whether the source is valid and projection metadata (source type, events, partitioning). Does not create or affect any session.",
}

type validateInput struct {
	Name string `json:"name" jsonschema:"Projection name from gaffer.toml"`
}

func (s *Server) handleValidate(_ context.Context, _ *mcp.CallToolRequest, input validateInput) (*mcp.CallToolResult, any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	proj := s.cfg.FindProjection(input.Name)
	if proj == nil {
		return toolError("projection %q not found in gaffer.toml", input.Name), nil, nil
	}

	source, err := engine.ReadSource(s.root, proj.Entry)
	if err != nil {
		return toolError("%v", err), nil, nil
	}

	lp := engine.NewProjection(s.root, s.cfg, proj, source)
	session, info, err := engine.CreateSession(lp, false, false)
	if err != nil {
		var projErr gafferruntime.ProjectionError
		if errors.As(err, &projErr) {
			// Same shape as handleRun: compile-time projection
			// failures feed projection_errors_seen so the
			// session's telemetry reflects user code didn't
			// compile.
			s.recordProjectionError(err)
			return toolResult(map[string]any{
				"valid":     false,
				"lastError": classifyError(err),
			}), nil, nil
		}
		return toolError("creating session: %v", err), nil, nil
	}
	defer session.Destroy()

	return toolResult(map[string]any{
		"valid":           true,
		"name":            input.Name,
		"entry":           proj.Entry,
		"engineVersion":   s.cfg.EffectiveEngineVersion(proj),
		"source":          engine.DescribeSource(info),
		"events":          info.Events,
		"partitioning":    engine.DescribePartitioning(info),
		"biState":         info.BiState,
		"producesResults": info.ProducesResults,
	}), nil, nil
}
