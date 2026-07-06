package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/kurrent-io/gaffer/cli/internal/config"
	"github.com/kurrent-io/gaffer/cli/internal/deploy"
	"github.com/kurrent-io/gaffer/cli/internal/remote"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// operateTarget names the write target and whether it reports production,
// mirroring the CLI's resolveOperateTarget: a bounded, best-effort
// $server-info read. The server's own production flag decides the tier -
// never the env label - and an unreadable $server-info degrades to the env
// label and non-production, the same as the CLI, so one server never gates
// differently per surface.
func operateTarget(ctx context.Context, client *remote.Client, envName string) (target string, prod bool) {
	sctx, cancel := context.WithTimeout(ctx, deploy.RPCTimeout)
	defer cancel()
	info, _ := client.ServerInfo(sctx)
	target = envName
	if info != nil && info.Name != "" {
		target = info.Name
	}
	return target, info.IsProduction()
}

// operateRPC bounds one server call like the CLI's rpc helper, so each step
// of a multi-step verb gets a full budget rather than sharing one.
func operateRPC(ctx context.Context, fn func(context.Context) error) error {
	rctx, cancel := context.WithTimeout(ctx, deploy.RPCTimeout)
	defer cancel()
	return fn(rctx)
}

// operateConn is the shared preamble for every operate verb: project gate,
// system-projection refusal, connect, existence check, and target/production
// resolution. A non-nil refuse result short-circuits the handler.
type operateConn struct {
	cfg        *config.Config
	root       string
	client     *remote.Client
	cleanup    func()
	env        config.ResolvedEnv
	target     string
	production bool
}

func (s *Server) operateSetup(ctx context.Context, name, envName string) (*operateConn, *mcp.CallToolResult) {
	cfg, root, r := s.requireProject()
	if r != nil {
		return nil, r
	}
	if name == "" {
		return nil, toolError("name required: pass the projection to operate on")
	}
	// System projections run the database's own plumbing; gaffer doesn't
	// manage them, and disabling or deleting one would break it.
	if strings.HasPrefix(name, "$") {
		return nil, toolError("%q is a system projection, which gaffer does not manage", name)
	}

	client, env, cleanup, err := s.connectRemote(cfg, root, envName)
	if err != nil {
		return nil, toolError("%v", err)
	}

	target, prod := operateTarget(ctx, client, env.Name)

	// A verb against a projection that isn't deployed reports "not deployed"
	// rather than a raw RPC error.
	var exists bool
	err = operateRPC(ctx, func(rctx context.Context) error {
		var eerr error
		exists, eerr = client.Exists(rctx, name)
		return eerr
	})
	if err != nil {
		cleanup()
		return nil, toolError("%v", err)
	}
	if !exists {
		cleanup()
		return nil, toolError("projection %q is not deployed on %s", name, target)
	}

	return &operateConn{cfg: cfg, root: root, client: client, cleanup: cleanup, env: env, target: target, production: prod}, nil
}

// operateResult is the shared envelope: the verb outcome plus the resolved
// env and target/production echo, so responses are self-describing.
func (c *operateConn) result(name, outcome string) map[string]any {
	out := map[string]any{
		"name":       name,
		"outcome":    outcome,
		"env":        c.env.Name,
		"target":     c.target,
		"production": c.production,
	}
	return out
}

// destructiveHints marks a write tool whose effect isn't trivially reversed.
func destructiveHints() *mcp.ToolAnnotations {
	t := true
	return &mcp.ToolAnnotations{DestructiveHint: &t}
}

// reversibleHints marks a write tool whose effect the paired verb undoes.
func reversibleHints() *mcp.ToolAnnotations {
	f := false
	return &mcp.ToolAnnotations{DestructiveHint: &f}
}

type operateInput struct {
	Name string `json:"name" jsonschema:"Projection name."`
	Env  string `json:"env,omitempty" jsonschema:"Environment from gaffer.toml ([env.<name>]); omit for the default env."`
}

var deployPauseTool = &mcp.Tool{
	Name: "deploy_pause",
	Description: "Pause (disable) a deployed projection on a KurrentDB environment: it stops " +
		"processing after writing a final checkpoint and can be resumed with deploy_resume. " +
		"Mirrors `gaffer disable`. On a production target this asks the human to confirm " +
		"via the client (elicitation); the result echoes env, target, and production.",
	Annotations: reversibleHints(),
}

func (s *Server) handleDeployPause(ctx context.Context, req *mcp.CallToolRequest, in operateInput) (*mcp.CallToolResult, any, error) {
	return s.runOperateVerb(ctx, req, in, verbSpec{
		verb: "Pause", outcome: "disabled", cli: "gaffer disable",
		do: func(rctx context.Context, c *remote.Client) error { return c.Disable(rctx, in.Name) },
	})
}

var deployResumeTool = &mcp.Tool{
	Name: "deploy_resume",
	Description: "Resume (enable) a paused projection on a KurrentDB environment. Mirrors " +
		"`gaffer enable`. On a production target this asks the human to confirm via the " +
		"client (elicitation); the result echoes env, target, and production.",
	Annotations: reversibleHints(),
}

func (s *Server) handleDeployResume(ctx context.Context, req *mcp.CallToolRequest, in operateInput) (*mcp.CallToolResult, any, error) {
	return s.runOperateVerb(ctx, req, in, verbSpec{
		verb: "Resume", outcome: "enabled", cli: "gaffer enable",
		do: func(rctx context.Context, c *remote.Client) error { return c.Enable(rctx, in.Name) },
	})
}

var deployAbortTool = &mcp.Tool{
	Name: "deploy_abort",
	Description: "Abort a deployed projection on a KurrentDB environment: it stops without " +
		"writing a final checkpoint, so a later resume reprocesses from the last checkpoint " +
		"that was written. Mirrors `gaffer disable --abort`. On a production target this " +
		"asks the human to confirm via the client (elicitation); the result echoes env, " +
		"target, and production.",
	Annotations: destructiveHints(),
}

func (s *Server) handleDeployAbort(ctx context.Context, req *mcp.CallToolRequest, in operateInput) (*mcp.CallToolResult, any, error) {
	return s.runOperateVerb(ctx, req, in, verbSpec{
		verb: "Abort", outcome: "aborted", cli: "gaffer disable --abort",
		warning: "It stops without a final checkpoint.",
		do:      func(rctx context.Context, c *remote.Client) error { return c.Abort(rctx, in.Name) },
	})
}

type deployDeleteInput struct {
	Name          string `json:"name" jsonschema:"Projection name."`
	Env           string `json:"env,omitempty" jsonschema:"Environment from gaffer.toml ([env.<name>]); omit for the default env."`
	DeleteEmitted bool   `json:"deleteEmitted,omitempty" jsonschema:"Also delete the streams the projection emitted."`
}

var deployDeleteTool = &mcp.Tool{
	Name: "deploy_delete",
	Description: "Delete a deployed projection from a KurrentDB environment, including its " +
		"state and checkpoint streams; deleteEmitted also removes the streams it emitted. " +
		"Mirrors `gaffer delete` (the projection is disabled first). There is no undo, so " +
		"this ALWAYS asks the human to confirm via the client (elicitation), production or " +
		"not - a client without elicitation cannot delete. The result echoes env, target, " +
		"and production.",
	Annotations: destructiveHints(),
}

func (s *Server) handleDeployDelete(ctx context.Context, req *mcp.CallToolRequest, in deployDeleteInput) (*mcp.CallToolResult, any, error) {
	warning := "This deletes the projection and its state; there is no undo."
	if in.DeleteEmitted {
		warning = "This deletes the projection, its state, and the streams it emitted; there is no undo."
	}
	return s.runOperateVerb(ctx, req, operateInput{Name: in.Name, Env: in.Env}, verbSpec{
		verb: "Delete", outcome: "deleted", cli: "gaffer delete",
		always:  true,
		warning: warning,
		do: func(octx context.Context, c *remote.Client) error {
			// The server rejects deleting an enabled projection; disable first,
			// like the CLI. Two RPCs, each under its own budget.
			if err := operateRPC(octx, func(rctx context.Context) error { return c.Disable(rctx, in.Name) }); err != nil {
				return fmt.Errorf("disabling before delete: %w", err)
			}
			return operateRPC(octx, func(rctx context.Context) error {
				return c.Delete(rctx, in.Name, remote.DeleteOptions{
					DeleteStateStream:      true,
					DeleteCheckpointStream: true,
					DeleteEmittedStreams:   in.DeleteEmitted,
				})
			})
		},
		perStepBudget: true,
	})
}

// verbSpec is one operate verb's shape: the gate wording, the outcome word,
// the CLI equivalent for refusals, and the write itself.
type verbSpec struct {
	verb    string
	outcome string
	cli     string
	warning string
	always  bool
	// do performs the write. With perStepBudget do runs under the caller's
	// context and bounds each of its own steps via operateRPC; otherwise it
	// runs under one RPC-timeout budget.
	do            func(context.Context, *remote.Client) error
	perStepBudget bool
}

// runOperateVerb is the shared operate flow: setup, gate, write, envelope.
func (s *Server) runOperateVerb(ctx context.Context, req *mcp.CallToolRequest, in operateInput, spec verbSpec) (*mcp.CallToolResult, any, error) {
	conn, r := s.operateSetup(ctx, in.Name, in.Env)
	if r != nil {
		return r, nil, nil
	}
	defer conn.cleanup()

	if r := confirmWrite(ctx, req, writeGate{
		Verb: spec.verb, Name: in.Name,
		Target: conn.target, Production: conn.production,
		Always: spec.always, Warning: spec.warning,
		CLI: fmt.Sprintf("%s %s", spec.cli, in.Name),
	}); r != nil {
		return r, nil, nil
	}

	var err error
	if spec.perStepBudget {
		err = spec.do(ctx, conn.client)
	} else {
		err = operateRPC(ctx, func(rctx context.Context) error { return spec.do(rctx, conn.client) })
	}
	if err != nil {
		if errors.Is(err, remote.ErrNotFound) {
			return toolError("projection %q is not deployed on %s", in.Name, conn.target), nil, nil
		}
		return toolError("%v", err), nil, nil
	}
	return toolResult(conn.result(in.Name, spec.outcome)), nil, nil
}
