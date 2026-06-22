//go:build integration

package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kurrent-io/KurrentDB-Client-Go/kurrentdb"
	"github.com/kurrent-io/gaffer/cli/internal/remote"
	"github.com/kurrent-io/gaffer/cli/internal/testutil"
)

func runDeployJSON(t *testing.T, args ...string) []deployJSON {
	t.Helper()
	root := NewRootCmd()
	root.SetArgs(append([]string{"deploy", "--json", "--yes"}, args...))
	root.SetErr(os.Stderr)
	out := testutil.CaptureStdout(t, func() {
		if err := ExecuteRoot(context.Background(), root); err != nil {
			t.Fatalf("deploy: %v", err)
		}
	})
	var got []deployJSON
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("unmarshal deploy json: %v\n%s", err, out)
	}
	return got
}

func deployOutcome(t *testing.T, results []deployJSON, name string) deployJSON {
	t.Helper()
	for _, r := range results {
		if r.Name == name {
			return r
		}
	}
	t.Fatalf("no deploy result for %q in %+v", name, results)
	return deployJSON{}
}

// waitRunning polls until the projection is enabled and running (not stopped or
// faulted), so an assertion doesn't race the reset's re-enable. It deliberately
// doesn't gate on progress: a continuous projection reports 0 until events flow,
// and the orchestration contract is "left running on the new query", not "caught
// up".
func waitRunning(t *testing.T, r *remote.Client, name string) {
	t.Helper()
	ctx := context.Background()
	var last *remote.Status
	for deadline := time.Now().Add(20 * time.Second); time.Now().Before(deadline); time.Sleep(300 * time.Millisecond) {
		s, err := r.Status(ctx, name)
		if err != nil {
			t.Fatalf("status %s: %v", name, err)
		}
		last = s
		if s.State == remote.StateFaulted {
			t.Fatalf("projection %s faulted: %s", name, s.FaultReason)
		}
		if s.State == remote.StateRunning {
			return
		}
	}
	t.Fatalf("projection %s never reached running: %+v", name, last)
}

func seedClient(t *testing.T) *kurrentdb.Client {
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
	return db
}

// TestDeployReset_Integration drives the reset path against a live KurrentDB:
// the full Disable -> Update -> Reset -> Enable orchestration has to succeed on
// a real server and leave the projection running on the new query. It also
// covers the default continue path (a logic change without the flag).
func TestDeployReset_Integration(t *testing.T) {
	r := diffSetupClient(t)
	ctx := context.Background()
	suffix := testutil.TestSuffix()

	db := seedClient(t)
	category := "resetsmoke" + suffix
	events := make([]kurrentdb.EventData, 3)
	for i := range events {
		events[i] = kurrentdb.EventData{
			EventID:     uuid.New(),
			EventType:   "Ping",
			ContentType: kurrentdb.ContentTypeJson,
			Data:        []byte(fmt.Sprintf(`{"seq":%d}`, i)),
		}
	}
	if _, err := db.AppendToStream(ctx, category+"-1", kurrentdb.AppendToStreamOptions{}, events...); err != nil {
		t.Fatalf("seed append: %v", err)
	}

	query := func(field string) string {
		return fmt.Sprintf("fromCategory('%s').foreachStream().when({ $init: function () { return { %s: 0 }; }, Ping: function (s, e) { s.%s++; return s; } })\n", category, field, field)
	}
	v1, v2, v3 := query("count"), query("total"), query("tally")

	name := "resetdep" + suffix
	p := testutil.NewProject(t).
		WithConnection(testutil.ConnectionString()).
		AddProjection(name, v1).
		Save()
	chdirTo(t, p.Dir)
	cleanupRemote(t, r, name)
	source := filepath.Join(p.Dir, "projections", name+".js")

	// Initial deploy creates and enables the projection.
	if got := deployOutcome(t, runDeployJSON(t), name); got.Outcome != "created" {
		t.Fatalf("first deploy outcome = %q, want created", got.Outcome)
	}
	waitRunning(t, r, name)

	// A query change rebuilt with the flag: outcome "rebuilt", and the four-step
	// sequence must leave the projection running on the new query.
	if err := os.WriteFile(source, []byte(v2), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := deployOutcome(t, runDeployJSON(t, "--reset-on-logic-change"), name); got.Outcome != "rebuilt" {
		t.Fatalf("reset deploy outcome = %q, want rebuilt", got.Outcome)
	}
	waitRunning(t, r, name)
	def, err := r.Read(ctx, name)
	if err != nil {
		t.Fatalf("read after reset: %v", err)
	}
	if !strings.Contains(def.Query, "total") {
		t.Errorf("query after reset = %q, want the v2 (total) query", def.Query)
	}
	if !def.Enabled {
		t.Error("projection should be enabled after reset")
	}

	// A query change without the flag is the default continue: outcome "updated"
	// carrying logic_change, projection still running.
	if err := os.WriteFile(source, []byte(v3), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := deployOutcome(t, runDeployJSON(t), name); got.Outcome != "updated" || !got.LogicChange {
		t.Errorf("continue deploy = %+v, want updated with logic_change", got)
	}
	waitRunning(t, r, name)
}
