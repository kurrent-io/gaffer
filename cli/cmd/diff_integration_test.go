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
	"github.com/kurrent-io/gaffer/cli/internal/deploy"
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
		if got.Verdict.Drift != "in-sync" || got.Right.Hash == "" || got.Right.Hash != got.Left.Hash {
			t.Fatalf("got %+v (verdict %+v), want in-sync with matching hashes", got, got.Verdict)
		}
	})

	t.Run("not deployed", func(t *testing.T) {
		if got := runDiffJSON(t, notDeployed); got.Verdict.Drift != "not-deployed" {
			t.Fatalf("got %+v, want not-deployed", got.Verdict)
		}
	})

	t.Run("untracked", func(t *testing.T) {
		got := runDiffJSON(t, untracked)
		if got.Verdict.Drift != "untracked" || got.Left.Hash == "" || got.Right.Hash != "" {
			t.Fatalf("got %+v (verdict %+v), want untracked with deployed hash only", got, got.Verdict)
		}
	})

	// version diff: resolve the deployed content by its own hash and diff it
	// against deployed. Same content, so all-equal lines, and a two-ref diff
	// carries no drift verdict.
	t.Run("version diff has no verdict", func(t *testing.T) {
		hash := runDiffJSON(t, deployed).Left.Hash
		got := runDiffJSON(t, deployed, "--left", "deployed", "--right", hash)
		if got.Verdict != nil || got.Changes != nil {
			t.Fatalf("version diff should carry no verdict/changes: %+v", got)
		}
		if got.Left.Ref != "deployed" || got.Right.Ref != "version" || got.Right.Hash != hash {
			t.Fatalf("got sides %+v / %+v, want deployed vs version %s", got.Left, got.Right, hash)
		}
		for _, l := range got.Lines {
			if l.Kind != deploy.LineEqual {
				t.Fatalf("deployed vs its own version should be all-equal, got %+v", l)
			}
		}
	})

	t.Run("drifted on query change", func(t *testing.T) {
		entry := filepath.Join(p.Dir, "projections", deployed+".js")
		if err := os.WriteFile(entry, []byte(source+"// local edit\n"), 0o644); err != nil {
			t.Fatalf("rewrite source: %v", err)
		}
		got := runDiffJSON(t, deployed)
		if got.Verdict.Drift != "drifted" || got.Changes == nil || !got.Changes.Query {
			t.Fatalf("got %+v (verdict %+v), want drifted with query change", got, got.Verdict)
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

	runDiffText := func(t *testing.T) string {
		t.Helper()
		root := NewRootCmd()
		root.SetArgs([]string{"diff", name})
		root.SetErr(os.Stderr)
		return testutil.CaptureStdout(t, func() {
			if err := ExecuteRoot(context.Background(), root); err != nil {
				t.Fatalf("diff: %v", err)
			}
		})
	}

	t.Run("inline by default", func(t *testing.T) {
		t.Setenv("GAFFER_EXTERNAL_DIFF", "") // isolate from the invoking shell
		out := runDiffText(t)
		if !strings.Contains(out, name) || !strings.Contains(out, "Query: +") {
			t.Errorf("missing styled drift summary in:\n%s", out)
		}
		if !strings.Contains(out, "+ // edited") {
			t.Errorf("missing the inline query diff (the added local line) in:\n%s", out)
		}
	})

	t.Run("external viewer opt-in", func(t *testing.T) {
		// A no-op external diff that echoes its file-path args, so we can confirm
		// the viewer ran with the two temp files rather than depending on git.
		t.Setenv("GAFFER_EXTERNAL_DIFF", "echo")
		out := runDiffText(t)
		if !strings.Contains(out, ".deployed") || !strings.Contains(out, ".local") {
			t.Errorf("viewer was not invoked with the two temp files:\n%s", out)
		}
	})
}
