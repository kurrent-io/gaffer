package lsp

import (
	"context"
	"fmt"

	"github.com/sourcegraph/jsonrpc2"

	"github.com/kurrent-io/gaffer/cli/internal/cliout"
	"github.com/kurrent-io/gaffer/cli/internal/config"
	"github.com/kurrent-io/gaffer/cli/internal/deploy"
	"github.com/kurrent-io/gaffer/cli/internal/drift"
	"github.com/kurrent-io/gaffer/cli/internal/versiondiff"
)

// diffFetchFunc computes one env's deployed↔local diff for a projection. A
// *jsonrpc2.Error return is surfaced to the editor as-is: CodeAuthRequired asks
// for sign-in, anything else is a generic failure. Injected onto the Server so
// the handler is testable without a live KurrentDB, mirroring statusFetchFunc.
type diffFetchFunc func(ctx context.Context, root string, cfg *config.Config, uri, env, name string) (cliout.DiffJSON, *jsonrpc2.Error)

// versionDiffFetchFunc computes a projection's diff between two arbitrary refs
// (hash / deployed / local). Like diffFetchFunc, a *jsonrpc2.Error is surfaced
// to the editor as-is, and it's a Server field so the handler is testable
// without a live KurrentDB.
type versionDiffFetchFunc func(ctx context.Context, root string, cfg *config.Config, uri, env, name, left, right string) (cliout.DiffJSON, *jsonrpc2.Error)

// handleDiffProjection serves gaffer/diffProjection: the default deployed↔local
// diff for one projection on one env, computed over the server's warm per-env
// connection (borrowed from the definition watch, or a fresh dial when none is
// up) instead of a cold `gaffer diff` spawn. The result is a cliout.DiffJSON,
// the same shape as `gaffer diff --json`, so the editor's diff wiring is
// unchanged. Not-deployed is a normal result (the deployed side has empty
// source), not an error. A sign-in-needed env returns CodeAuthRequired so the
// editor offers a sign-in rather than a generic error toast.
func (s *Server) handleDiffProjection(ctx context.Context, req *jsonrpc2.Request) (any, error) {
	s.stats.diffRequests.Add(1)
	params, jerr := decodeParams[DiffProjectionParams](req, "diffProjection")
	if jerr != nil {
		return nil, jerr
	}
	// loadConfig, not loadStatusConfig: diffProjection is a client-pulled request,
	// so it must not be gated on the vscode-oriented statusLens rendering
	// capability - any editor that opened the gaffer.toml can ask for a diff.
	cfg, root, load := s.loadConfig(params.ConfigURI)
	if load != loadOK {
		// No parseable config for the URI. The caller vouches for the URI (the
		// lens that triggers the diff), so this is an unexpected client state.
		return nil, &jsonrpc2.Error{
			Code:    jsonrpc2.CodeInvalidParams,
			Message: fmt.Sprintf("no parseable gaffer.toml for %q", params.ConfigURI),
		}
	}
	diff, jerr := s.diffFetch(ctx, root, cfg, params.ConfigURI, params.Env, params.Name)
	if jerr != nil {
		return nil, jerr
	}
	return diff, nil
}

// fetchDiff is the default diffFetchFunc: borrow the env's warm connection (or
// dial fresh when none is up), read the deployed definition, compile the local
// one, and return the diff. Auth classification mirrors fetchEnvStatus - a
// missing/locked token fails the dial, a token the IdP rejected trips the auth
// flag only on the read.
func (s *Server) fetchDiff(ctx context.Context, root string, cfg *config.Config, uri, env, name string) (cliout.DiffJSON, *jsonrpc2.Error) {
	bc, err := s.envClient(root, cfg, uri, env)
	if err != nil {
		return cliout.DiffJSON{}, dialError(cfg, root, env, err)
	}
	defer bc.release()

	// Bound the read so a stalled projections subsystem doesn't hang the request.
	rctx, cancel := context.WithTimeout(ctx, deploy.RPCTimeout)
	defer cancel()

	entry, err := guardedOp(cfg, root, env, "diff", func() (drift.Comparison, error) {
		return drift.Compare(rctx, bc.client, cfg, root, name)
	})
	// A stored OAuth token the IdP rejected (invalid_grant) trips the auth flag
	// only on the read, not at connect - the credential is dead, not merely
	// unreachable. Surface sign-in rather than the generic error.
	if bc.authInv != nil && bc.authInv.Tripped() {
		return cliout.DiffJSON{}, authRequiredError(env)
	}
	if err != nil {
		return cliout.DiffJSON{}, &jsonrpc2.Error{Code: jsonrpc2.CodeInternalError, Message: userFacingError(cfg, root, env, err)}
	}
	return cliout.ComparisonDiffJSON(entry), nil
}

// handleDiffVersions serves gaffer/diffVersions: a source diff between two
// arbitrary versions of a projection (hash / deployed / local) over the warm
// per-env connection, for the history viewer's per-entry diffs. Like
// diffProjection, the result is a cliout.DiffJSON and a sign-in-needed env
// returns CodeAuthRequired; unlike it, there's no drift verdict.
func (s *Server) handleDiffVersions(ctx context.Context, req *jsonrpc2.Request) (any, error) {
	s.stats.diffRequests.Add(1)
	params, jerr := decodeParams[DiffVersionsParams](req, "diffVersions")
	if jerr != nil {
		return nil, jerr
	}
	cfg, root, load := s.loadConfig(params.ConfigURI)
	if load != loadOK {
		return nil, &jsonrpc2.Error{
			Code:    jsonrpc2.CodeInvalidParams,
			Message: fmt.Sprintf("no parseable gaffer.toml for %q", params.ConfigURI),
		}
	}
	diff, jerr := s.versionDiffFetch(ctx, root, cfg, params.ConfigURI, params.Env, params.Name, params.Left, params.Right)
	if jerr != nil {
		return nil, jerr
	}
	return diff, nil
}

// fetchVersionDiff is the default versionDiffFetchFunc: parse the two refs,
// borrow the env's warm connection, and resolve + diff them via versiondiff (the
// same builder `gaffer diff --left --right` uses). Auth classification mirrors
// fetchDiff.
func (s *Server) fetchVersionDiff(ctx context.Context, root string, cfg *config.Config, uri, env, name, left, right string) (cliout.DiffJSON, *jsonrpc2.Error) {
	lref, err := versiondiff.ParseRef(left)
	if err != nil {
		return cliout.DiffJSON{}, &jsonrpc2.Error{Code: jsonrpc2.CodeInvalidParams, Message: err.Error()}
	}
	rref, err := versiondiff.ParseRef(right)
	if err != nil {
		return cliout.DiffJSON{}, &jsonrpc2.Error{Code: jsonrpc2.CodeInvalidParams, Message: err.Error()}
	}

	bc, err := s.envClient(root, cfg, uri, env)
	if err != nil {
		return cliout.DiffJSON{}, dialError(cfg, root, env, err)
	}
	defer bc.release()

	// Bound the read so a stalled projections subsystem doesn't hang the request.
	rctx, cancel := context.WithTimeout(ctx, deploy.RPCTimeout)
	defer cancel()

	diff, err := guardedOp(cfg, root, env, "diff", func() (cliout.DiffJSON, error) {
		j, _, _, e := versiondiff.Build(rctx, bc.client, cfg, root, name, lref, rref)
		return j, e
	})
	if bc.authInv != nil && bc.authInv.Tripped() {
		return cliout.DiffJSON{}, authRequiredError(env)
	}
	if err != nil {
		return cliout.DiffJSON{}, &jsonrpc2.Error{Code: jsonrpc2.CodeInternalError, Message: userFacingError(cfg, root, env, err)}
	}
	return diff, nil
}
