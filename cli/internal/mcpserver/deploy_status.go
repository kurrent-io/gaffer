package mcpserver

import (
	"context"

	"github.com/kurrent-io/gaffer/cli/internal/cliout"
	"github.com/kurrent-io/gaffer/cli/internal/deploy"
	"github.com/kurrent-io/gaffer/cli/internal/drift"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var deployStatusTool = &mcp.Tool{
	Name: "deploy_status",
	Description: "Show the state of projections deployed to a KurrentDB environment and how " +
		"each compares to local config: runtime state and progress, the drift verdict " +
		"(in-sync / drifted / not-deployed / untracked / invalid), who owns and last " +
		"deployed it, and - when gaffer.toml declares a [database_config] - any divergence " +
		"from the target node's live engine settings (configDrift). Mirrors " +
		"`gaffer status --json`. Omit `name` to list every local and deployed projection.",
	Annotations: readOnlyHints(),
}

type deployStatusInput struct {
	Name string `json:"name,omitempty" jsonschema:"Projection name; omit for every local and deployed projection."`
	Env  string `json:"env,omitempty" jsonschema:"Environment from gaffer.toml ([env.<name>]); omit for the default env."`
}

func (s *Server) handleDeployStatus(ctx context.Context, _ *mcp.CallToolRequest, in deployStatusInput) (*mcp.CallToolResult, any, error) {
	cfg, root, r := s.requireProject()
	if r != nil {
		return r, nil, nil
	}

	// Comparing local definitions compiles them, so serialize against the
	// session-owning tools like every other compiling handler.
	s.mu.Lock()
	defer s.mu.Unlock()

	client, env, cleanup, err := s.connectRemote(cfg, root, in.Env)
	if err != nil {
		return toolError("%v", err), nil, nil
	}
	defer cleanup()

	// The [database_config] drift check runs in the background so its HTTP
	// round-trip overlaps the status RPCs; drained before building the report.
	driftCh := drift.StartConfigDriftCheck(ctx, cfg, root, env.Name, env.Connection)

	// Management calls block until their deadline if the projections subsystem
	// is still starting, so bound the read rather than hang the tool call.
	rctx, cancel := context.WithTimeout(ctx, deploy.RPCTimeout)
	defer cancel()

	var entries []drift.StatusEntry
	if in.Name != "" {
		entry, err := drift.StatusOne(rctx, client, cfg, root, in.Name)
		if err != nil {
			return toolError("%v", err), nil, nil
		}
		entries = []drift.StatusEntry{entry}
	} else {
		entries, err = drift.StatusAll(rctx, client, cfg, root)
		if err != nil {
			return toolError("%v", err), nil, nil
		}
	}
	return toolResult(cliout.BuildStatusReport(entries, <-driftCh)), nil, nil
}
