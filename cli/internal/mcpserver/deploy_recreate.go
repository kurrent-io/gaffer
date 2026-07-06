package mcpserver

import (
	"context"
	"fmt"
	"strings"

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
		"`gaffer recreate` (the local source must compile and pass the diagnostics " +
		"preflight; there is no validation bypass). Because it embeds a delete, this " +
		"ALWAYS asks the human to confirm via the client (elicitation), production or " +
		"not. The result echoes env, target, and production. Ledger-stamps " +
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
	proj := engine.NewProjection(conn.root, conn.cfg, def, source)
	local, err := engine.LocalDescriptor(proj)
	if err != nil {
		return toolError("projection %q does not compile, so there's nothing to recreate it from: %v", in.Name, err), nil, nil
	}
	// The diagnostics preflight the CLI runs by default: a source that
	// compiles can still carry error-severity diagnostics known to fault on
	// (or be rejected by) the server, and Delete runs before Create - so
	// without this gate a healthy projection would be destroyed and rebuilt
	// into a faulting one.
	diags, err := engine.Preflight(proj)
	if err != nil {
		return toolError("projection %q does not compile, so there's nothing to recreate it from: %v", in.Name, err), nil, nil
	}
	if len(diags) > 0 {
		reasons := make([]string, 0, len(diags))
		for _, d := range diags {
			reasons = append(reasons, d.Code+": "+d.Message)
		}
		return toolError("projection %q would fault on the server, so recreate refuses to rebuild from it: %s", in.Name, strings.Join(reasons, "; ")), nil, nil
	}

	consequence := "Destroys state and rebuilds from zero. No undo."
	if local.Emit && !in.DeleteEmitted {
		consequence += " It emits: recreating re-emits and may duplicate (deleteEmitted wipes the emitted streams first)."
	}
	// No-undo tier, like delete: recreate embeds a delete - state is wiped,
	// and deleteEmitted destroys emitted-stream data - so it always elicits,
	// and a production confirm requires typing the projection name.
	cli := "gaffer recreate " + shellQuote(in.Name)
	if in.DeleteEmitted {
		cli += " --delete-emitted"
	}
	if r := confirmWrite(ctx, req, writeGate{
		Action: fmt.Sprintf("recreate projection %q", in.Name),
		Name:   in.Name, Env: conn.env.Name,
		Target: conn.target, Production: conn.production,
		NoUndo:      true,
		Consequence: consequence,
		CLI:         cli,
	}); r != nil {
		return r, nil, nil
	}

	// The tool-metadata stamped on the rebuild's create, so history attributes
	// the recreate to gaffer instead of showing anonymous lifecycle steps.
	ledger := stamp.Ledger(conn.env, remote.OpRecreate, s.version, conn.root)

	// The destructive Disable -> Delete -> Create sequence, its ordering,
	// per-step RPC bounds, and recovery messages live in internal/deploy
	// (shared with the CLI); here we only bind each step to the client.
	err = deploy.Recreate(ctx, in.Name, deploy.RecreateSteps{
		Disable: func(sctx context.Context) error { return conn.client.Disable(sctx, in.Name) },
		Delete: func(sctx context.Context) error {
			return conn.client.Delete(sctx, in.Name, remote.DeleteOptions{
				DeleteStateStream:      true,
				DeleteCheckpointStream: true,
				DeleteEmittedStreams:   in.DeleteEmitted,
			})
		},
		Create: func(sctx context.Context) error {
			return conn.client.Create(sctx, in.Name, local.Query, remote.CreateOptions{
				EngineVersion:       local.EngineVersion,
				Emit:                local.Emit,
				TrackEmittedStreams: local.TrackEmittedStreams,
				Ledger:              &ledger,
			})
		},
	})
	if err != nil {
		return toolError("%v", err), nil, nil
	}
	return toolResult(conn.result(in.Name, "recreated")), nil, nil
}
