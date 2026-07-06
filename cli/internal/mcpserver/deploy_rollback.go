package mcpserver

import (
	"context"
	"errors"
	"fmt"

	"github.com/kurrent-io/gaffer/cli/internal/deploy"
	"github.com/kurrent-io/gaffer/cli/internal/remote"
	"github.com/kurrent-io/gaffer/cli/internal/stamp"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type deployRollbackInput struct {
	Name string `json:"name" jsonschema:"Projection name."`
	Hash string `json:"hash" jsonschema:"Content hash of the version to roll back to, from deploy_history; a unique prefix of at least 4 hex characters works."`
	Env  string `json:"env,omitempty" jsonschema:"Environment from gaffer.toml ([env.<name>]); omit for the default env."`
}

var deployRollbackTool = &mcp.Tool{
	Name: "deploy_rollback",
	Description: "Roll a deployed projection back to a prior version by content hash, picked " +
		"from deploy_history. It rewrites the live query in place (an update), so code rolls " +
		"back but state does not - state built by the newer query is kept - and local files " +
		"are untouched, so deploy_status shows drift until local is reconciled. A version " +
		"differing in engine version or emitted-stream tracking can't be applied in place " +
		"and is refused (use deploy_recreate after updating local config). Mirrors " +
		"`gaffer rollback`. On a production target this asks the human to confirm via the " +
		"client (elicitation); the result echoes env, target, and production, plus the full " +
		"target hash. Outcome is unchanged when the target version is already deployed. " +
		"Ledger-stamps operation: rollback.",
	Annotations: destructiveHints(),
}

func (s *Server) handleDeployRollback(ctx context.Context, req *mcp.CallToolRequest, in deployRollbackInput) (*mcp.CallToolResult, any, error) {
	prefix, err := remote.NormalizeHashPrefix(in.Hash)
	if err != nil {
		return toolError("%v", err), nil, nil
	}

	conn, r := s.operateSetup(ctx, in.Name, in.Env)
	if r != nil {
		return r, nil, nil
	}
	defer conn.cleanup()

	var current *remote.Definition
	err = operateRPC(ctx, func(rctx context.Context) error {
		var rerr error
		current, rerr = conn.client.Read(rctx, in.Name)
		return rerr
	})
	if err != nil {
		if errors.Is(err, remote.ErrNotFound) {
			return toolError("projection %q is not deployed on %s", in.Name, conn.target), nil, nil
		}
		return toolError("%v", err), nil, nil
	}

	tgt, err := conn.client.FindVersionByHash(ctx, in.Name, prefix)
	if err != nil {
		return toolError("%v", err), nil, nil
	}

	currentDesc := current.Descriptor()
	res := conn.result(in.Name, "unchanged")
	res["hash"] = tgt.Hash
	if tgt.Hash == currentDesc.Hash() {
		return toolResult(res), nil, nil
	}

	tgtDesc := tgt.Def.Descriptor()
	cmp := deploy.Compare(tgtDesc, currentDesc)
	if err := remote.RollbackRefusal(cmp, tgt.Hash, in.Name); err != nil {
		return toolError("%v", err), nil, nil
	}

	if r := confirmWrite(ctx, req, writeGate{
		Verb: "Roll back", Name: in.Name,
		Target: conn.target, Production: conn.production,
		Warning: "Code rolls back, state does not; local files stay untouched and will show as drift.",
		CLI:     fmt.Sprintf("gaffer rollback %s %s", in.Name, prefix),
	}); r != nil {
		return r, nil, nil
	}

	ledger := stamp.Ledger(conn.env, remote.OpRollback, s.version, conn.root)
	err = operateRPC(ctx, func(rctx context.Context) error {
		return conn.client.Update(rctx, in.Name, tgt.Def.Query, remote.UpdateOptions{Emit: &tgt.Def.Emit, Ledger: &ledger})
	})
	if err != nil {
		return toolError("could not roll back %s: %v", in.Name, err), nil, nil
	}
	res["outcome"] = "rolled-back"
	return toolResult(res), nil, nil
}
