package mcpserver

import (
	"context"
	"errors"
	"strings"

	gafferruntime "github.com/kurrent-io/gaffer/bindings/go"
	"github.com/kurrent-io/gaffer/cli/internal/engine"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var validateTool = &mcp.Tool{
	Name:        "validate",
	Description: "Compile and check a projection without running it. Returns whether the source is valid and projection metadata (source type, events, partitioning). Does not create or affect any session.",
	Annotations: readOnlyHints(),
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

	compiled, err := s.compileProjection(cfg, root, input.Name, false)
	if err != nil {
		var notFound engine.ProjectionNotFoundError
		if errors.As(err, &notFound) {
			return toolError("%v", err), nil, nil
		}
		var srcErr engine.SourceReadError
		if errors.As(err, &srcErr) {
			return toolError("%v", err), nil, nil
		}
		// A static config error reports valid:false with the raw reason;
		// the config flags never reach the runtime so there's nothing to
		// classify. Checked before the runtime ProjectionError so a config
		// failure isn't run through classifyError.
		var cfgErr engine.ProjectionConfigError
		if errors.As(err, &cfgErr) {
			return toolResult(map[string]any{"valid": false, "lastError": cfgErr.Error()}), nil, nil
		}
		// Compile-time projection failure: compileProjection already fed
		// projection_errors_seen; report valid:false with the classified
		// error.
		var projErr gafferruntime.ProjectionError
		if errors.As(err, &projErr) {
			return toolResult(map[string]any{
				"valid":     false,
				"lastError": classifyError(err),
			}), nil, nil
		}
		return toolError("creating session: %v", err), nil, nil
	}
	proj := compiled.Projection.Def
	info := compiled.Info
	defer compiled.Session.Destroy()

	// The projection compiled but carries error-severity diagnostics for a feature the
	// server rejects or faults on (e.g. a V2-incompatible option), so it is not
	// deployable - the same verdict deploy/recreate preflight reach. Report valid:false
	// with every such diagnostic in lastError (not just the first), so a projection that
	// trips more than one isn't half-reported. Uses the {valid, lastError} key shape the
	// config-error and compile-error paths above return.
	if errs := engine.ErrorDiagnostics(info.Diagnostics); len(errs) > 0 {
		reasons := make([]string, len(errs))
		for i, d := range errs {
			reasons[i] = d.Code + ": " + d.Message
		}
		return toolResult(map[string]any{
			"valid":     false,
			"lastError": strings.Join(reasons, "; "),
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
