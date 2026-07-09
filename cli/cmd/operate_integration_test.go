//go:build integration

package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kurrent-io/KurrentDB-Client-Go/kurrentdb"
	"github.com/kurrent-io/gaffer/cli/internal/remote"
	"github.com/kurrent-io/gaffer/cli/internal/testutil"
)

func runOperateJSON(t *testing.T, args ...string) operateJSON {
	t.Helper()
	root := NewRootCmd()
	root.SetArgs(append(args, "--json"))
	root.SetErr(io.Discard)
	out := testutil.CaptureStdout(t, func() {
		if err := ExecuteRoot(context.Background(), root); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
	})
	var got operateJSON
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("unmarshal operate json: %v\n%s", err, out)
	}
	return got
}

func waitState(t *testing.T, r *remote.Client, name string, want remote.State) {
	t.Helper()
	waitStateAny(t, r, name, want)
}

// waitStateAny polls until the projection reaches any of the acceptable states.
// Abort is the reason for the "any": a modern server reports it as StateAborted,
// but accepting StateStopped too keeps the assertion honest against a server that
// only reports plain Stopped, without over-fitting to one status string.
func waitStateAny(t *testing.T, r *remote.Client, name string, want ...remote.State) {
	t.Helper()
	ctx := context.Background()
	var last *remote.Status
	for deadline := time.Now().Add(15 * time.Second); time.Now().Before(deadline); time.Sleep(300 * time.Millisecond) {
		s, err := r.Status(ctx, name)
		if err != nil {
			t.Fatalf("status %s: %v", name, err)
		}
		last = s
		for _, w := range want {
			if s.State == w {
				return
			}
		}
	}
	t.Fatalf("projection %s never reached %v: %+v", name, want, last)
}

// TestOperate_Integration drives enable / disable / abort / delete against a live
// KurrentDB: each verb's RPC has to land on a real server and leave the
// projection in the expected state, and delete has to remove it.
func TestOperate_Integration(t *testing.T) {
	r := diffSetupClient(t)
	ctx := context.Background()
	suffix := testutil.TestSuffix()

	db := seedClient(t)
	category := "opsmoke" + suffix
	if _, err := db.AppendToStream(ctx, category+"-1", kurrentdb.AppendToStreamOptions{}, kurrentdb.EventData{
		EventID: uuid.New(), EventType: "Ping", ContentType: kurrentdb.ContentTypeJson, Data: []byte(`{}`),
	}); err != nil {
		t.Fatalf("seed append: %v", err)
	}

	name := "opdep" + suffix
	query := fmt.Sprintf("fromCategory('%s').foreachStream().when({ $init: function () { return { n: 0 }; }, Ping: function (s, e) { s.n++; return s; } })\n", category)
	cleanupRemote(t, r, name)
	if err := r.Create(ctx, name, query, remote.CreateOptions{EngineVersion: 2}); err != nil {
		t.Fatalf("create: %v", err)
	}
	waitRunning(t, r, name)

	// The project supplies the default connection; operate verbs target the
	// server by name and don't need the projection in gaffer.toml.
	p := testutil.NewProject(t).WithConnection(testutil.ConnectionString()).Save()
	chdirTo(t, p.Dir)

	t.Run("disable", func(t *testing.T) {
		if got := runOperateJSON(t, "disable", name); got.Outcome != "disabled" {
			t.Fatalf("disable outcome = %q, want disabled", got.Outcome)
		}
		waitState(t, r, name, remote.StateStopped)
	})

	t.Run("enable", func(t *testing.T) {
		if got := runOperateJSON(t, "enable", name); got.Outcome != "enabled" {
			t.Fatalf("enable outcome = %q, want enabled", got.Outcome)
		}
		waitState(t, r, name, remote.StateRunning)
	})

	t.Run("disable --abort", func(t *testing.T) {
		if got := runOperateJSON(t, "disable", name, "--abort"); got.Outcome != "aborted" {
			t.Fatalf("abort outcome = %q, want aborted", got.Outcome)
		}
		waitStateAny(t, r, name, remote.StateAborted, remote.StateStopped)
	})

	t.Run("delete", func(t *testing.T) {
		// Delete a running projection, so the disable-first step is exercised (not
		// a projection the prior subtest already stopped).
		if err := r.Enable(ctx, name); err != nil {
			t.Fatalf("re-enable before delete: %v", err)
		}
		waitRunning(t, r, name)

		// delete always confirms; --yes is required non-interactively.
		if got := runOperateJSON(t, "delete", name, "--yes"); got.Outcome != "deleted" {
			t.Fatalf("delete outcome = %q, want deleted", got.Outcome)
		}
		ok, err := r.Exists(ctx, name)
		if err != nil {
			t.Fatalf("exists after delete: %v", err)
		}
		if ok {
			t.Error("projection should not exist after delete")
		}
	})

	t.Run("missing projection reports not deployed", func(t *testing.T) {
		root := NewRootCmd()
		root.SetArgs([]string{"disable", "nope" + suffix})
		root.SetOut(io.Discard)
		root.SetErr(io.Discard)
		err := ExecuteRoot(context.Background(), root)
		if err == nil || !strings.Contains(err.Error(), "not deployed") {
			t.Errorf("disable of a missing projection should report 'not deployed', got: %v", err)
		}
	})
}
