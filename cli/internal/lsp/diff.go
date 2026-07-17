package lsp

import (
	"context"
	"errors"
	"fmt"
	"log"

	"github.com/sourcegraph/jsonrpc2"

	"github.com/kurrent-io/gaffer/cli/internal/cliout"
	"github.com/kurrent-io/gaffer/cli/internal/config"
	"github.com/kurrent-io/gaffer/cli/internal/deploy"
	"github.com/kurrent-io/gaffer/cli/internal/drift"
	"github.com/kurrent-io/gaffer/cli/internal/target"
)

// diffFetchFunc computes one env's deployed↔local diff for a projection. A
// *jsonrpc2.Error return is surfaced to the editor as-is: CodeAuthRequired asks
// for sign-in, anything else is a generic failure. Injected onto the Server so
// the handler is testable without a live KurrentDB, mirroring statusFetchFunc.
type diffFetchFunc func(ctx context.Context, root string, cfg *config.Config, uri, env, name string) (cliout.DiffJSON, *jsonrpc2.Error)

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
	cfg, root, load := s.loadStatusConfig(params.ConfigURI)
	if load != loadOK {
		// No parseable config for the URI (or the client didn't opt into the
		// status surface). The lens that triggers the diff already vouches for
		// the URI, so this is an unexpected client state, not a user action.
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
		return cliout.DiffJSON{}, diffDialError(err, env)
	}
	defer bc.release()

	// Bound the read so a stalled projections subsystem doesn't hang the request.
	rctx, cancel := context.WithTimeout(ctx, deploy.RPCTimeout)
	defer cancel()

	entry, err := diffCompareGuarded(cfg, root, env, func() (drift.Comparison, error) {
		return drift.Compare(rctx, bc.client, cfg, root, name)
	})
	// A stored OAuth token the IdP rejected (invalid_grant) trips the auth flag
	// only on the read, not at connect - the credential is dead, not merely
	// unreachable. Surface sign-in rather than the generic error.
	if bc.authInv != nil && bc.authInv.Tripped() {
		return cliout.DiffJSON{}, authRequiredError(env)
	}
	if err != nil {
		return cliout.DiffJSON{}, &jsonrpc2.Error{Code: jsonrpc2.CodeInternalError, Message: err.Error()}
	}
	return cliout.ComparisonDiffJSON(entry), nil
}

// diffCompareGuarded runs compare with the same panic guard as safeFetch: a
// crash deep in the KurrentDB client (e.g. a nil-deref on an unready projection
// subsystem) surfaces as an error instead of taking down the language server. A
// handler panic is unrecovered whether it runs on the read loop or, for the diff,
// on its own goroutine (see offloadBlocking), so it's fatal either way without
// this. The panic value is scrubbed of the env's connection secret before
// logging. compare is a parameter so the guard is testable without a live client.
func diffCompareGuarded(cfg *config.Config, root, env string, compare func() (drift.Comparison, error)) (entry drift.Comparison, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			msg := fmt.Sprint(rec)
			if resolved, rerr := cfg.ResolveEnv(env); rerr == nil {
				msg = scrubConnection(msg, root, resolved)
			}
			log.Printf("lsp: diff for env %q panicked: %s", env, msg)
			err = errors.New("diff read failed unexpectedly")
		}
	}()
	return compare()
}

// diffDialError classifies a dial/connect failure: a missing or locked token the
// dial can't satisfy needs sign-in (CodeAuthRequired); anything else is a generic
// internal error. Mirrors dialErrStatus on the status path.
func diffDialError(err error, env string) *jsonrpc2.Error {
	var authErr *target.AuthRequiredError
	if errors.As(err, &authErr) {
		return authRequiredError(env)
	}
	return &jsonrpc2.Error{Code: jsonrpc2.CodeInternalError, Message: err.Error()}
}

func authRequiredError(env string) *jsonrpc2.Error {
	return &jsonrpc2.Error{
		Code:    CodeAuthRequired,
		Message: fmt.Sprintf("sign-in required for env %q", env),
	}
}
