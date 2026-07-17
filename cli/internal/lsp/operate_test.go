package lsp

import (
	"context"
	"testing"

	"github.com/sourcegraph/jsonrpc2"

	"github.com/kurrent-io/gaffer/cli/internal/config"
)

func seedOperateServer(t *testing.T, fetch operateFetchFunc) (*Server, string) {
	t.Helper()
	root := t.TempDir()
	cfg := writeWorkspaceFile(t, root, "gaffer.toml", diffConfig)
	writeWorkspaceFile(t, root, "checkout.js", "function project(){}")
	uri := pathToURI(cfg)
	s := testServer(nil)
	s.operateFetch = fetch
	s.docs.Open(uri, diffConfig)
	return s, uri
}

func operateReq(t *testing.T, uri, name, env, verb string) *jsonrpc2.Request {
	t.Helper()
	req := &jsonrpc2.Request{}
	if err := req.SetParams(OperateProjectionParams{ConfigURI: uri, Name: name, Env: env, Verb: verb}); err != nil {
		t.Fatal(err)
	}
	return req
}

func failOperateFetch(t *testing.T) operateFetchFunc {
	t.Helper()
	return func(context.Context, string, *config.Config, string, string, OperateProjectionParams) (OperateProjectionResult, *jsonrpc2.Error) {
		t.Error("operateFetch should not be reached")
		return OperateProjectionResult{}, nil
	}
}

func TestHandleOperateProjection_ReturnsFetchResult(t *testing.T) {
	var got OperateProjectionParams
	s, uri := seedOperateServer(t, func(_ context.Context, _ string, _ *config.Config, _, _ string, p OperateProjectionParams) (OperateProjectionResult, *jsonrpc2.Error) {
		got = p
		return OperateProjectionResult{Name: p.Name, Outcome: "paused", Target: "prod-cluster"}, nil
	})
	res, err := s.handleOperateProjection(context.Background(), operateReq(t, uri, "checkout", "local", "pause"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	or, ok := res.(OperateProjectionResult)
	if !ok {
		t.Fatalf("expected OperateProjectionResult, got %T (%v)", res, res)
	}
	if or.Outcome != "paused" || or.Target != "prod-cluster" {
		t.Errorf("result: %+v", or)
	}
	if got.Verb != "pause" || got.Name != "checkout" {
		t.Errorf("fetch args: %+v", got)
	}
}

func TestHandleOperateProjection_UnknownVerbRejectedBeforeFetch(t *testing.T) {
	s, uri := seedOperateServer(t, failOperateFetch(t))
	_, err := s.handleOperateProjection(context.Background(), operateReq(t, uri, "checkout", "local", "nuke"))
	assertJSONRPCCode(t, err, jsonrpc2.CodeInvalidParams)
}

func TestHandleOperateProjection_AuthErrorPassesThrough(t *testing.T) {
	s, uri := seedOperateServer(t, func(context.Context, string, *config.Config, string, string, OperateProjectionParams) (OperateProjectionResult, *jsonrpc2.Error) {
		return OperateProjectionResult{}, authRequiredError("local")
	})
	_, err := s.handleOperateProjection(context.Background(), operateReq(t, uri, "checkout", "local", "delete"))
	assertJSONRPCCode(t, err, CodeAuthRequired)
}

func TestHandleOperateProjection_NilParams(t *testing.T) {
	s, _ := seedOperateServer(t, failOperateFetch(t))
	_, err := s.handleOperateProjection(context.Background(), &jsonrpc2.Request{})
	assertJSONRPCCode(t, err, jsonrpc2.CodeInvalidParams)
}

func TestHandleOperateProjection_NoConfigForURI(t *testing.T) {
	s, _ := seedOperateServer(t, failOperateFetch(t))
	_, err := s.handleOperateProjection(context.Background(), operateReq(t, "file:///nope/gaffer.toml", "checkout", "local", "pause"))
	assertJSONRPCCode(t, err, jsonrpc2.CodeInvalidParams)
}

func TestHandleOperateProjection_IncrementsCounter(t *testing.T) {
	s, uri := seedOperateServer(t, func(context.Context, string, *config.Config, string, string, OperateProjectionParams) (OperateProjectionResult, *jsonrpc2.Error) {
		return OperateProjectionResult{}, nil
	})
	for range 3 {
		if _, err := s.handleOperateProjection(context.Background(), operateReq(t, uri, "checkout", "local", "pause")); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if got := s.Stats().OperateRequestCount; got != 3 {
		t.Errorf("OperateRequestCount = %d, want 3", got)
	}
}

func TestValidOperateVerb(t *testing.T) {
	for _, v := range []string{"pause", "resume", "abort", "delete"} {
		if !validOperateVerb(v) {
			t.Errorf("%q should be a valid verb", v)
		}
	}
	// recreate/rollback are separate surfaces, not this menu; case matters.
	for _, v := range []string{"", "nuke", "recreate", "rollback", "Pause"} {
		if validOperateVerb(v) {
			t.Errorf("%q should be rejected", v)
		}
	}
}
