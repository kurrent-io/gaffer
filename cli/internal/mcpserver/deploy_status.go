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
		"each compares to local config. Mirrors `gaffer status --json`; the response echoes " +
		"the resolved env, the target server, and whether it is a production target (declared " +
		"by the server itself, or by production = true on the env). " +
		"Omit `name` to list every local and deployed projection. Per " +
		"projection: drift is in-sync / drifted / not-deployed / untracked / invalid (an " +
		"invalid row carries reason, the local compile or config error); owner " +
		"is in-config / orphan (gaffer-deployed but no longer in gaffer.toml - a deletion " +
		"candidate) / foreign (another tool's) / unknown (no readable metadata); attribution " +
		"appears only on drifted rows - local-ahead (local edited since gaffer's deploy), " +
		"changed-by-tool, or changed-server. runtime.state is running / stopped / aborted / " +
		"faulted / unknown. aborted means the projection was killed without a final checkpoint, " +
		"so a resume reprocesses from the last checkpoint written (re-emitting, for an emitting " +
		"projection). The server reports it only in memory, so it reverts to stopped after a " +
		"server restart. Its absence is not proof of a clean pause. A faulted row adds " +
		"runtime.faultReason (the server's fault message). " +
		"runtime.progress is a percentage (0-100; " +
		"negative means the server couldn't report it). configDrift lists [database_config] " +
		"knobs diverging from the target node's live engine settings; it is node-level, not " +
		"per-projection, so it appears even when `name` scopes the report. When the node's " +
		"config couldn't be read, configDriftError carries the reason instead, so an " +
		"absent configDrift alone doesn't mean in sync.",
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

	// No s.mu: comparing local definitions compiles them into throwaway
	// engine sessions that never touch s.session, and the live-run feed
	// goroutine already drives the FFI concurrently with locked handlers.
	// Holding the server mutex across a WAN dial plus bounded reads would
	// block every session tool behind network latency for no protection.
	client, env, cleanup, err := s.connectRemote(cfg, root, in.Env)
	if err != nil {
		return toolError("%v", err), nil, nil
	}
	defer cleanup()

	// The [database_config] drift check runs in the background so its HTTP
	// round-trip overlaps the status RPCs; drained before building the report.
	driftCh := drift.StartConfigDriftCheck(ctx, cfg, root, env)

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
	report := cliout.BuildStatusReport(entries, <-driftCh)
	report.Env = env.Name
	target, prod := client.OperateTarget(ctx, env, deploy.RPCTimeout)
	report.Target, report.Production = target, &prod
	return toolResult(report), nil, nil
}
