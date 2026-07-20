//go:build integration

package lsp

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sourcegraph/jsonrpc2"

	"github.com/kurrent-io/KurrentDB-Client-Go/kurrentdb"
	"github.com/kurrent-io/gaffer/cli/internal/config"
	"github.com/kurrent-io/gaffer/cli/internal/remote"
	"github.com/kurrent-io/gaffer/cli/internal/testutil"
)

// TestPerformOperate_Integration exercises the real write path (borrow-or-dial →
// remote write → resolve target) against a live KurrentDB, which the unit tests
// can't reach (they inject a fake operateFetch).
func TestPerformOperate_Integration(t *testing.T) {
	root := t.TempDir()
	name := "op" + testutil.TestSuffix()
	const source = "fromAll().when({ $any: function (s, e) { return s; } })\n"

	cfgSrc := "[env.it]\nconnection = \"" + testutil.ConnectionString() + "\"\n\n" +
		"[[projection]]\nname = \"" + name + "\"\nentry = \"" + name + ".js\"\nengine_version = 2\n"
	cfg, err := config.Parse([]byte(cfgSrc))
	if err != nil {
		t.Fatalf("parse config: %v", err)
	}
	const uri = "file:///op/gaffer.toml"

	// Create the projection to operate on, and ensure teardown removes it.
	dbCfg, err := kurrentdb.ParseConnectionString(testutil.ConnectionString())
	if err != nil {
		t.Fatalf("parse connection: %v", err)
	}
	dbCfg.Logger = kurrentdb.NoopLogging()
	db, err := kurrentdb.NewClient(dbCfg)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	r := remote.New(db)
	ctx := context.Background()
	if err := r.Create(ctx, name, source, remote.CreateOptions{EngineVersion: 2}); err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() {
		_ = r.Disable(ctx, name)
		_ = r.Delete(ctx, name, remote.DeleteOptions{DeleteStateStream: true, DeleteCheckpointStream: true, DeleteEmittedStreams: true})
	})

	s := NewServer(ServerOptions{})
	op := func(params OperateProjectionParams) OperateProjectionResult {
		t.Helper()
		octx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		res, jerr := s.performOperate(octx, root, cfg, uri, "it", params)
		if jerr != nil {
			t.Fatalf("%s: %v", params.Verb, jerr.Message)
		}
		if res.Name != params.Name || res.Target == "" {
			t.Fatalf("%s result: %+v", params.Verb, res)
		}
		return res
	}
	verb := func(v string) OperateProjectionParams {
		return OperateProjectionParams{Name: name, Env: "it", Verb: v}
	}

	if got := op(verb(verbPause)); got.Outcome != "paused" {
		t.Errorf("pause outcome = %q, want paused", got.Outcome)
	}
	if got := op(verb(verbResume)); got.Outcome != "resumed" {
		t.Errorf("resume outcome = %q, want resumed", got.Outcome)
	}
	if got := op(verb(verbAbort)); got.Outcome != "aborted" {
		t.Errorf("abort outcome = %q, want aborted", got.Outcome)
	}
	// Delete with the emitted-streams variant, exercising the DeleteEmittedStreams
	// wiring (this projection emits nothing, so it's the code path under test).
	del := verb(verbDelete)
	del.DeleteEmitted = true
	if got := op(del); got.Outcome != "deleted" {
		t.Errorf("delete outcome = %q, want deleted", got.Outcome)
	}
	if _, err := r.Read(ctx, name); !errors.Is(err, remote.ErrNotFound) {
		t.Errorf("expected %q gone after delete, got %v", name, err)
	}

	// A verb on a projection that isn't deployed surfaces a clean invalid-params
	// error, not a raw RPC failure.
	octx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, jerr := s.performOperate(octx, root, cfg, uri, "it", OperateProjectionParams{Name: name, Env: "it", Verb: verbPause})
	if jerr == nil || jerr.Code != jsonrpc2.CodeInvalidParams {
		t.Errorf("operate on a deleted projection: got %v, want CodeInvalidParams", jerr)
	}
}
