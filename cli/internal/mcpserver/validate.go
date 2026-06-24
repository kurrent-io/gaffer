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
	cfg, root, r := s.requireProject()
	if r != nil {
		return r, nil, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	proj := cfg.FindProjection(input.Name)
	if proj == nil {
		return toolError("projection %q not found in gaffer.toml", input.Name), nil, nil
	}
	// Per-projection config errors are deferred past config.Load, so a bad
	// projection doesn't block the others. Report it as invalid here rather than
	// compiling on past it and wrongly reporting valid (the config flags never
	// reach the runtime).
	if cfgErr := cfg.ProjectionConfigError(input.Name); cfgErr != nil {
		return toolResult(map[string]any{"valid": false, "lastError": cfgErr.Error()}), nil, nil
	}

	source, err := engine.ReadSource(root, proj.Entry)
	if err != nil {
		return toolError("%v", err), nil, nil
	}

	lp := engine.NewProjection(root, cfg, proj, source)
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

	// A projection can compile yet carry error-severity diagnostics for a feature
	// the server rejects or faults on (e.g. a V2-incompatible option). Report it
	// invalid, matching what deploy/recreate preflight would refuse, rather than a
	// bare valid:true that contradicts the diagnostic.
	if errs := engine.ErrorDiagnostics(info.Diagnostics); len(errs) > 0 {
		// Same shape as the config-error and compile-error paths above: valid:false
		// plus lastError (code + message), so every invalid verdict is one shape for
		// the client. The diagnostic detail lives in lastError, not a separate field.
		return toolResult(map[string]any{
			"valid":     false,
			"lastError": errs[0].Code + ": " + errs[0].Message,
		}), nil, nil
	}

	return toolResult(map[string]any{
		"valid":           true,
		"name":            input.Name,
		"entry":           proj.Entry,
		"engineVersion":   cfg.EffectiveEngineVersion(proj),
		"source":          engine.DescribeSource(info),
		"events":          info.Events,
		"partitioning":    engine.DescribePartitioning(info),
		"biState":         info.BiState,
		"producesResults": info.ProducesResults,
		"emitsEvents":     info.EmitsEvents,
	}), nil, nil
}
