//go:build integration

package cmd

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kurrent-io/KurrentDB-Client-Go/kurrentdb"
	"github.com/kurrent-io/gaffer/cli/internal/remote"
	"github.com/kurrent-io/gaffer/cli/internal/testutil"
)

func diffSetupClient(t *testing.T) *remote.Client {
	t.Helper()
	cfg, err := kurrentdb.ParseConnectionString(testutil.ConnectionString())
	if err != nil {
		t.Fatalf("parse connection: %v", err)
	}
	cfg.Logger = kurrentdb.NoopLogging()
	db, err := kurrentdb.NewClient(cfg)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return remote.New(db)
}

func cleanupRemote(t *testing.T, r *remote.Client, name string) {
	t.Cleanup(func() {
		ctx := context.Background()
		_ = r.Disable(ctx, name)
		_ = r.Delete(ctx, name, remote.DeleteOptions{DeleteEmittedStreams: true, DeleteStateStream: true, DeleteCheckpointStream: true})
	})
}

func runDiffJSON(t *testing.T, args ...string) diffJSON {
	t.Helper()
	root := NewRootCmd()
	root.SetArgs(append([]string{"diff", "--json"}, args...))
	root.SetErr(os.Stderr)
	out := testutil.CaptureStdout(t, func() {
		if err := ExecuteRoot(context.Background(), root); err != nil {
			t.Fatalf("diff: %v", err)
		}
	})
	var got diffJSON
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("unmarshal diff json: %v\n%s", err, out)
	}
	return got
}

func TestDiff_Integration(t *testing.T) {
	r := diffSetupClient(t)
	ctx := context.Background()
	suffix := testutil.TestSuffix()
	deployed := "difftest" + suffix
	notDeployed := "difflocal" + suffix
	untracked := "diffuntracked" + suffix
	const source = "fromAll().when({ $init: function () { return { n: 0 }; }, $any: function (s, e) { s.n++; return s; } })\n"

	p := testutil.NewProject(t).
		WithConnection(testutil.ConnectionString()).
		AddProjection(deployed, source).
		AddProjection(notDeployed, source).
		Save()
	chdirTo(t, p.Dir)
	cleanupRemote(t, r, deployed)
	cleanupRemote(t, r, untracked)

	if err := r.Create(ctx, deployed, source, remote.CreateOptions{EngineVersion: 2}); err != nil {
		t.Fatalf("Create deployed: %v", err)
	}
	// untracked: on the server, never in local config.
	if err := r.Create(ctx, untracked, source, remote.CreateOptions{EngineVersion: 2}); err != nil {
		t.Fatalf("Create untracked: %v", err)
	}

	t.Run("in sync", func(t *testing.T) {
		got := runDiffJSON(t, deployed)
		if got.State != "in-sync" || got.LocalHash == "" || got.LocalHash != got.DeployedHash {
			t.Fatalf("got %+v, want in-sync with matching hashes", got)
		}
	})

	t.Run("not deployed", func(t *testing.T) {
		if got := runDiffJSON(t, notDeployed); got.State != "not-deployed" {
			t.Fatalf("got %+v, want not-deployed", got)
		}
	})

	t.Run("untracked", func(t *testing.T) {
		got := runDiffJSON(t, untracked)
		if got.State != "untracked" || got.DeployedHash == "" || got.LocalHash != "" {
			t.Fatalf("got %+v, want untracked with deployed hash only", got)
		}
	})

	t.Run("drifted on query change", func(t *testing.T) {
		entry := filepath.Join(p.Dir, "projections", deployed+".js")
		if err := os.WriteFile(entry, []byte(source+"// local edit\n"), 0o644); err != nil {
			t.Fatalf("rewrite source: %v", err)
		}
		got := runDiffJSON(t, deployed)
		if got.State != "drifted" || got.Drift == nil || !got.Drift.Query {
			t.Fatalf("got %+v, want drifted with query change", got)
		}
	})
}

func TestDiff_Integration_Viewer(t *testing.T) {
	r := diffSetupClient(t)
	ctx := context.Background()
	suffix := testutil.TestSuffix()
	name := "diffview" + suffix
	const source = "fromAll().when({ $any: function (s, e) { return s; } })\n"

	p := testutil.NewProject(t).WithConnection(testutil.ConnectionString()).AddProjection(name, source).Save()
	chdirTo(t, p.Dir)
	cleanupRemote(t, r, name)
	if err := r.Create(ctx, name, source, remote.CreateOptions{EngineVersion: 2}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := os.WriteFile(filepath.Join(p.Dir, "projections", name+".js"), []byte(source+"// edited\n"), 0o644); err != nil {
		t.Fatalf("rewrite: %v", err)
	}

	// A no-op external diff that echoes its file-path args, so we can confirm the
	// viewer ran with the two temp files rather than depending on git's output.
	t.Setenv("GAFFER_EXTERNAL_DIFF", "echo")

	root := NewRootCmd()
	root.SetArgs([]string{"diff", name})
	root.SetErr(os.Stderr)
	out := testutil.CaptureStdout(t, func() {
		if err := ExecuteRoot(context.Background(), root); err != nil {
			t.Fatalf("diff: %v", err)
		}
	})

	if !strings.Contains(out, name) || !strings.Contains(out, "Query: +") {
		t.Errorf("missing styled drift summary in:\n%s", out)
	}
	if !strings.Contains(out, ".remote") || !strings.Contains(out, ".local") {
		t.Errorf("viewer was not invoked with the two temp files:\n%s", out)
	}
}
