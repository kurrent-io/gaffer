package mcpserver

import (
	"context"

	"github.com/kurrent-io/gaffer/cli/internal/deploy"
	"github.com/kurrent-io/gaffer/cli/internal/engine"
	"github.com/kurrent-io/gaffer/cli/internal/remote"
	"github.com/kurrent-io/gaffer/cli/internal/stamp"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type deployRecreateInput struct {
	Name          string `json:"name" jsonschema:"Projection name; must be in gaffer.toml (recreate rebuilds from local config)."`
	Env           string `json:"env,omitempty" jsonschema:"Environment from gaffer.toml ([env.<name>]); omit for the default env."`
	DeleteEmitted bool   `json:"deleteEmitted,omitempty" jsonschema:"Also delete the streams the projection emitted, so the rebuild doesn't re-emit duplicates into them."`
}

var deployRecreateTool = &mcp.Tool{
	Name: "deploy_recreate",
	Description: "Recreate a deployed projection from local config on a KurrentDB environment: " +
		"disable, delete (state and checkpoints included), then create from the compiled " +
		"local source - the only way to change create-time settings like engine version or " +
		"emitted-stream tracking. State is destroyed and rebuilt from zero; an emitting " +
		"projection re-emits unless deleteEmitted wipes its target streams first. Mirrors " +
		"`gaffer recreate` (the local source must compile; there is no validation bypass). " +
		"On a production target this asks the human to confirm via the client " +
		"(elicitation); the result echoes env, target, and production. Ledger-stamps " +
		"operation: recreate.",
	Annotations: destructiveHints(),
}

func (s *Server) handleDeployRecreate(ctx context.Context, req *mcp.CallToolRequest, in deployRecreateInput) (*mcp.CallToolResult, any, error) {
	conn, r := s.operateSetup(ctx, in.Name, in.Env)
	if r != nil {
		return r, nil, nil
	}
	defer conn.cleanup()

	// Recreate rebuilds from local config, so the projection must be there,
	// its per-projection config must be valid, and the source must compile -
	// Delete runs before Create, so anything caught later would leave the
	// projection gone with nothing to rebuild. No validation bypass exists
	// here: the CLI's --no-validate is an operator's flag, not an agent's.
	def := conn.cfg.FindProjection(in.Name)
	if def == nil {
		return toolError("projection %q is not in gaffer.toml; recreate rebuilds from local config", in.Name), nil, nil
	}
	if err := conn.cfg.ProjectionConfigError(in.Name); err != nil {
		return toolError("%v", err), nil, nil
	}
	source, err := engine.ReadSource(conn.root, def.Entry)
	if err != nil {
		return toolError("%v", err), nil, nil
	}
	local, err := engine.LocalDescriptor(engine.NewProjection(conn.root, conn.cfg, def, source))
	if err != nil {
		return toolError("projection %q does not compile, so there's nothing to recreate it from: %v", in.Name, err), nil, nil
	}

	warning := "State is destroyed and rebuilt from zero."
	if local.Emit && !in.DeleteEmitted {
		warning += " It emits: recreating re-emits and may duplicate into its target streams (deleteEmitted wipes them first)."
	}
	if r := confirmWrite(ctx, req, writeGate{
		Verb: "Recreate", Name: in.Name,
		Target: conn.target, Production: conn.production,
		Warning: warning,
		CLI:     "gaffer recreate " + in.Name,
	}); r != nil {
		return r, nil, nil
	}

	// The tool-metadata stamped on the rebuild's create, so history attributes
	// the recreate to gaffer instead of showing anonymous lifecycle steps.
	ledger := stamp.Ledger(conn.env, remote.OpRecreate, s.version, conn.root)

	// The destructive Disable -> Delete -> Create sequence, its ordering and
	// recovery messages, live in internal/deploy (shared with the CLI); here we
	// only bind each step to the client, each under its own RPC budget.
	err = deploy.Recreate(ctx, in.Name, deploy.RecreateSteps{
		Disable: func(sctx context.Context) error {
			return operateRPC(sctx, func(rctx context.Context) error { return conn.client.Disable(rctx, in.Name) })
		},
		Delete: func(sctx context.Context) error {
			return operateRPC(sctx, func(rctx context.Context) error {
				return conn.client.Delete(rctx, in.Name, remote.DeleteOptions{
					DeleteStateStream:      true,
					DeleteCheckpointStream: true,
					DeleteEmittedStreams:   in.DeleteEmitted,
				})
			})
		},
		Create: func(sctx context.Context) error {
			return operateRPC(sctx, func(rctx context.Context) error {
				return conn.client.Create(rctx, in.Name, local.Query, remote.CreateOptions{
					EngineVersion:       local.EngineVersion,
					Emit:                local.Emit,
					TrackEmittedStreams: local.TrackEmittedStreams,
					Ledger:              &ledger,
				})
			})
		},
	})
	if err != nil {
		return toolError("%v", err), nil, nil
	}
	return toolResult(conn.result(in.Name, "recreated")), nil, nil
}
