//go:build integration

package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/kurrent-io/KurrentDB-Client-Go/kurrentdb"
	"github.com/kurrent-io/gaffer/cli/internal/remote"
	"github.com/kurrent-io/gaffer/cli/internal/testutil"
)

func runRollbackJSON(t *testing.T, args ...string) rollbackJSON {
	t.Helper()
	root := NewRootCmd()
	root.SetArgs(append(append([]string{"rollback"}, args...), "--json", "--yes"))
	root.SetErr(io.Discard)
	out := testutil.CaptureStdout(t, func() {
		if err := ExecuteRoot(context.Background(), root); err != nil {
			t.Fatalf("rollback %v: %v", args, err)
		}
	})
	var got rollbackJSON
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("unmarshal rollback json: %v\n%s", err, out)
	}
	return got
}

// TestRollback_Integration drives rollback against a live KurrentDB: the target
// version has to be found by hash in the real history, the Update has to land the
// old query back, and the guard paths must refuse cleanly.
func TestRollback_Integration(t *testing.T) {
	r := diffSetupClient(t)
	ctx := context.Background()
	suffix := testutil.TestSuffix()

	db := seedClient(t)
	category := "rbsmoke" + suffix
	if _, err := db.AppendToStream(ctx, category+"-1", kurrentdb.AppendToStreamOptions{}, kurrentdb.EventData{
		EventID: uuid.New(), EventType: "Ping", ContentType: kurrentdb.ContentTypeJson, Data: []byte(`{}`),
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	queryV1 := fmt.Sprintf("fromCategory('%s').foreachStream().when({ $init: function () { return { n: 0 }; }, Ping: function (s, e) { s.n++; return s; } })\n", category)
	queryV2 := strings.Replace(queryV1, "s.n++", "s.n += 2", 1)
	name := "rbdep" + suffix

	cleanupRemote(t, r, name)
	if err := r.Create(ctx, name, queryV1, remote.CreateOptions{EngineVersion: 2}); err != nil {
		t.Fatalf("create: %v", err)
	}
	waitRunning(t, r, name)
	v1, err := r.Read(ctx, name)
	if err != nil {
		t.Fatalf("read v1: %v", err)
	}
	v1Hash := v1.Descriptor().Hash()

	if err := r.Update(ctx, name, queryV2, remote.UpdateOptions{}); err != nil {
		t.Fatalf("update to v2: %v", err)
	}
	waitRunning(t, r, name)

	p := testutil.NewProject(t).WithConnection(testutil.ConnectionString()).Save()
	chdirTo(t, p.Dir)

	t.Run("rolls back to the hash's content", func(t *testing.T) {
		got := runRollbackJSON(t, name, v1Hash[:7])
		if got.Outcome != "rolled-back" || got.Hash != v1Hash {
			t.Fatalf("rollback = %+v, want rolled-back to %s", got, v1Hash)
		}
		def, err := r.Read(ctx, name)
		if err != nil {
			t.Fatalf("read after rollback: %v", err)
		}
		if def.Query != queryV1 {
			t.Errorf("query after rollback = %q, want v1", def.Query)
		}
		// The write is stamped as a rollback. Gated like the other ledger
		// assertions: a release that ignores the metadata field can't carry it.
		if os.Getenv("GAFFER_TEST_LEDGER") != "" {
			l, _, err := r.ReadLedger(ctx, name)
			if err != nil {
				t.Fatalf("read ledger after rollback: %v", err)
			}
			if l.Operation != remote.OpRollback {
				t.Errorf("ledger operation = %q, want rollback", l.Operation)
			}
		}
	})

	t.Run("already at the target is a no-op", func(t *testing.T) {
		if got := runRollbackJSON(t, name, v1Hash[:7]); got.Outcome != "unchanged" {
			t.Fatalf("outcome = %q, want unchanged", got.Outcome)
		}
	})

	t.Run("unknown hash reports no match", func(t *testing.T) {
		root := NewRootCmd()
		root.SetArgs([]string{"rollback", name, "ffffffff", "--yes"})
		root.SetOut(io.Discard)
		root.SetErr(io.Discard)
		err := ExecuteRoot(context.Background(), root)
		if err == nil || !strings.Contains(err.Error(), "no version matching") {
			t.Errorf("err = %v, want no-version-matching", err)
		}
	})

	t.Run("missing projection reports not deployed", func(t *testing.T) {
		root := NewRootCmd()
		root.SetArgs([]string{"rollback", "nope" + suffix, "abcd1234", "--yes"})
		root.SetOut(io.Discard)
		root.SetErr(io.Discard)
		err := ExecuteRoot(context.Background(), root)
		if err == nil || !strings.Contains(err.Error(), "not deployed") {
			t.Errorf("err = %v, want not-deployed", err)
		}
	})
}
