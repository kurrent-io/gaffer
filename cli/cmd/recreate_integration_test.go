//go:build integration

package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/kurrent-io/KurrentDB-Client-Go/kurrentdb"
	"github.com/kurrent-io/gaffer/cli/internal/remote"
	"github.com/kurrent-io/gaffer/cli/internal/testutil"
)

// recreateErr runs `gaffer recreate <args>` for the error-path cases and returns
// the command error (no JSON to parse).
func recreateErr(t *testing.T, args ...string) error {
	t.Helper()
	root := NewRootCmd()
	root.SetArgs(append([]string{"recreate"}, args...))
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	return ExecuteRoot(context.Background(), root)
}

// TestRecreate_Integration drives recreate against a live KurrentDB: the
// Disable -> Delete -> Create sequence has to land on a real server and leave the
// projection running, and the guard paths (not-in-config, not-deployed) must
// refuse before touching it.
func TestRecreate_Integration(t *testing.T) {
	r := diffSetupClient(t)
	ctx := context.Background()
	suffix := testutil.TestSuffix()

	db := seedClient(t)
	category := "recreatesmoke" + suffix
	if _, err := db.AppendToStream(ctx, category+"-1", kurrentdb.AppendToStreamOptions{}, kurrentdb.EventData{
		EventID: uuid.New(), EventType: "Ping", ContentType: kurrentdb.ContentTypeJson, Data: []byte(`{}`),
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	query := fmt.Sprintf("fromCategory('%s').foreachStream().when({ $init: function () { return { count: 0 }; }, Ping: function (s, e) { s.count++; return s; } })\n", category)
	name := "recreatedep" + suffix
	notDeployed := "recreatelocal" + suffix // in config, never created on the server

	p := testutil.NewProject(t).
		WithConnection(testutil.ConnectionString()).
		AddProjection(name, query).
		AddProjection(notDeployed, query).
		Save()
	chdirTo(t, p.Dir)

	cleanupRemote(t, r, name)
	if err := r.Create(ctx, name, query, remote.CreateOptions{EngineVersion: 2}); err != nil {
		t.Fatalf("create: %v", err)
	}
	waitRunning(t, r, name)

	t.Run("rebuilds a deployed projection", func(t *testing.T) {
		if got := runOperateJSON(t, "recreate", name, "--yes"); got.Outcome != "recreated" {
			t.Fatalf("recreate outcome = %q, want recreated", got.Outcome)
		}
		waitRunning(t, r, name)
		def, err := r.Read(ctx, name)
		if err != nil {
			t.Fatalf("read after recreate: %v", err)
		}
		if !def.Enabled || !strings.Contains(def.Query, "count") {
			t.Errorf("after recreate: enabled=%v query=%q, want enabled with the count query", def.Enabled, def.Query)
		}
		// The rebuild's create is stamped, so history attributes it to gaffer.
		l, _, err := r.ReadLedger(ctx, name)
		if err != nil {
			t.Fatalf("read ledger after recreate: %v", err)
		}
		if l.Tool != remote.ToolName || l.Operation != remote.OpRecreate {
			t.Errorf("ledger after recreate = tool %q operation %q, want %s/%s", l.Tool, l.Operation, remote.ToolName, remote.OpRecreate)
		}
	})

	t.Run("refuses a projection not in gaffer.toml", func(t *testing.T) {
		err := recreateErr(t, "ghost"+suffix)
		if err == nil || !strings.Contains(err.Error(), "not in gaffer.toml") {
			t.Errorf("recreate of an unknown projection should report 'not in gaffer.toml', got: %v", err)
		}
	})

	t.Run("refuses a projection not deployed", func(t *testing.T) {
		err := recreateErr(t, notDeployed)
		if err == nil || !strings.Contains(err.Error(), "not deployed") {
			t.Errorf("recreate of a not-deployed projection should report 'not deployed', got: %v", err)
		}
	})

	t.Run("refuses on a broken local source and keeps the projection", func(t *testing.T) {
		// The compile gate must catch a non-compiling local before anything is
		// deleted - the deployed projection must survive a refused recreate.
		src := filepath.Join(p.Dir, "projections", name+".js")
		if err := os.WriteFile(src, []byte("this is not valid javascript {{{\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		defer os.WriteFile(src, []byte(query), 0o644) //nolint:errcheck // best-effort restore

		if err := recreateErr(t, name, "--yes"); err == nil {
			t.Fatal("recreate should refuse when the local source doesn't compile")
		}
		if ok, err := r.Exists(ctx, name); err != nil || !ok {
			t.Errorf("projection must survive a refused recreate (exists=%v err=%v)", ok, err)
		}
	})
}
