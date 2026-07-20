package lsp

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/sourcegraph/jsonrpc2"

	"github.com/kurrent-io/gaffer/cli/internal/config"
	"github.com/kurrent-io/gaffer/cli/internal/deploy"
	"github.com/kurrent-io/gaffer/cli/internal/remote"
)

// operateFetchFunc runs one operate verb on a projection over the env connection.
// A *jsonrpc2.Error return is surfaced to the editor as-is (CodeAuthRequired →
// sign-in). Injected onto the Server so the handler is testable without a live
// KurrentDB, mirroring diffFetchFunc.
type operateFetchFunc func(ctx context.Context, root string, cfg *config.Config, uri, env string, params OperateProjectionParams) (OperateProjectionResult, *jsonrpc2.Error)

// handleOperateProjection serves gaffer/operateProjection: run pause/resume/abort/
// delete on one projection over the server's warm per-env connection. The editor
// renders the confirm tier before calling, so the write is unconditional. A
// sign-in-needed env returns CodeAuthRequired.
func (s *Server) handleOperateProjection(ctx context.Context, req *jsonrpc2.Request) (any, error) {
	s.stats.operateRequests.Add(1)
	params, jerr := decodeParams[OperateProjectionParams](req, "operateProjection")
	if jerr != nil {
		return nil, jerr
	}
	if !validOperateVerb(params.Verb) {
		return nil, &jsonrpc2.Error{
			Code:    jsonrpc2.CodeInvalidParams,
			Message: fmt.Sprintf("unknown operate verb %q", params.Verb),
		}
	}
	// Refuse system projections ($-prefixed), like the MCP operate path - the
	// menu only lists config projections, but never let one drive a write to a
	// server built-in.
	if strings.HasPrefix(params.Name, "$") {
		return nil, &jsonrpc2.Error{
			Code:    jsonrpc2.CodeInvalidParams,
			Message: fmt.Sprintf("cannot operate on system projection %q", params.Name),
		}
	}
	cfg, root, load := s.loadConfig(params.ConfigURI)
	if load != loadOK {
		return nil, &jsonrpc2.Error{
			Code:    jsonrpc2.CodeInvalidParams,
			Message: fmt.Sprintf("no parseable gaffer.toml for %q", params.ConfigURI),
		}
	}
	res, jerr := s.operateFetch(ctx, root, cfg, params.ConfigURI, params.Env, params)
	if jerr != nil {
		return nil, jerr
	}
	return res, nil
}

// performOperate is the default operateFetchFunc: borrow the env's warm connection
// (or dial fresh), run the verb, and resolve the target for the toast. Auth
// classification mirrors fetchDiff.
func (s *Server) performOperate(ctx context.Context, root string, cfg *config.Config, uri, env string, params OperateProjectionParams) (OperateProjectionResult, *jsonrpc2.Error) {
	bc, err := s.envClient(root, cfg, uri, env)
	if err != nil {
		return OperateProjectionResult{}, dialError(err, env)
	}
	defer bc.release()

	// runOperateVerb bounds each management RPC by RPCTimeout on its own - delete
	// is two writes (disable then delete), so a slow first step mustn't starve the
	// second, matching the MCP operate path's per-step budget.
	outcome, err := guardedOp(cfg, root, env, "operate", func() (string, error) {
		return runOperateVerb(ctx, bc.client, params)
	})
	// A rejected token trips the auth flag on the write, like the read paths.
	if bc.authInv != nil && bc.authInv.Tripped() {
		return OperateProjectionResult{}, authRequiredError(env)
	}
	if errors.Is(err, remote.ErrNotFound) {
		// A projection in gaffer.toml but not on the server: a clean message, not
		// a raw RPC error (mirrors the MCP operate path).
		return OperateProjectionResult{}, &jsonrpc2.Error{
			Code:    jsonrpc2.CodeInvalidParams,
			Message: fmt.Sprintf("%q is not deployed on %q", params.Name, env),
		}
	}
	if err != nil {
		return OperateProjectionResult{}, &jsonrpc2.Error{Code: jsonrpc2.CodeInternalError, Message: err.Error()}
	}

	// Resolve the target name for the completion toast; best-effort, the write
	// already succeeded. Each management RPC in runOperateVerb bounded its own
	// budget, so this uses the request ctx directly.
	target := env
	if resolved, rerr := cfg.ResolveEnv(env); rerr == nil {
		target, _ = bc.client.OperateTarget(ctx, resolved, deploy.RPCTimeout)
	}
	return OperateProjectionResult{Name: params.Name, Outcome: outcome, Target: target}, nil
}

// operate verbs. pause = disable with a final checkpoint, abort = disable without
// one, resume = enable, delete = disable-then-delete (the server rejects deleting
// an enabled projection). Mirrors the MCP deploy_* tools and the CLI verbs.
const (
	verbPause  = "pause"
	verbResume = "resume"
	verbAbort  = "abort"
	verbDelete = "delete"
)

func validOperateVerb(v string) bool {
	switch v {
	case verbPause, verbResume, verbAbort, verbDelete:
		return true
	default:
		return false
	}
}

// runOperateVerb dispatches to the remote write, bounding each management RPC by
// its own RPCTimeout. Returns the past-tense outcome for the toast. The verb is
// validated by the caller.
func runOperateVerb(ctx context.Context, r *remote.Client, params OperateProjectionParams) (string, error) {
	switch params.Verb {
	case verbPause:
		return "paused", rpcCall(ctx, func(c context.Context) error { return r.Disable(c, params.Name) })
	case verbResume:
		return "resumed", rpcCall(ctx, func(c context.Context) error { return r.Enable(c, params.Name) })
	case verbAbort:
		return "aborted", rpcCall(ctx, func(c context.Context) error { return r.Abort(c, params.Name) })
	case verbDelete:
		// Disable first: the server rejects deleting an enabled projection. Each
		// write gets its own budget.
		if err := rpcCall(ctx, func(c context.Context) error { return r.Disable(c, params.Name) }); err != nil {
			return "", err
		}
		return "deleted", rpcCall(ctx, func(c context.Context) error {
			return r.Delete(c, params.Name, remote.DeleteOptions{
				DeleteStateStream:      true,
				DeleteCheckpointStream: true,
				DeleteEmittedStreams:   params.DeleteEmitted,
			})
		})
	default:
		return "", fmt.Errorf("unknown operate verb %q", params.Verb)
	}
}

// rpcCall bounds a single management RPC by deploy.RPCTimeout, so a multi-step
// verb gives each step its own budget rather than sharing one.
func rpcCall(ctx context.Context, fn func(context.Context) error) error {
	c, cancel := context.WithTimeout(ctx, deploy.RPCTimeout)
	defer cancel()
	return fn(c)
}
