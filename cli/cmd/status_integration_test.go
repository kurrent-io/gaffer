//go:build integration

package cmd

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/kurrent-io/gaffer/cli/internal/remote"
	"github.com/kurrent-io/gaffer/cli/internal/testutil"
)

func runStatusJSON(t *testing.T, args ...string) []statusJSON {
	t.Helper()
	root := NewRootCmd()
	root.SetArgs(append([]string{"status", "--json"}, args...))
	root.SetErr(os.Stderr)
	out := testutil.CaptureStdout(t, func() {
		if err := ExecuteRoot(context.Background(), root); err != nil {
			t.Fatalf("status: %v", err)
		}
	})
	var got []statusJSON
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("unmarshal status json: %v\n%s", err, out)
	}
	return got
}

func TestStatus_Integration(t *testing.T) {
	r := diffSetupClient(t)
	ctx := context.Background()
	suffix := testutil.TestSuffix()
	deployed := "statusdep" + suffix
	notDeployed := "statuslocal" + suffix
	untrackedA := "statusua" + suffix
	untrackedZ := "statusuz" + suffix
	const source = "fromAll().when({ $any: function (s, e) { return s; } })\n"

	p := testutil.NewProject(t).
		WithConnection(testutil.ConnectionString()).
		AddProjection(deployed, source).
		AddProjection(notDeployed, source).
		Save()
	chdirTo(t, p.Dir)
	cleanupRemote(t, r, deployed)
	cleanupRemote(t, r, untrackedA)
	cleanupRemote(t, r, untrackedZ)

	for _, n := range []string{deployed, untrackedA, untrackedZ} {
		if err := r.Create(ctx, n, source, remote.CreateOptions{EngineVersion: 2}); err != nil {
			t.Fatalf("Create %s: %v", n, err)
		}
	}

	t.Run("overview reconciles all", func(t *testing.T) {
		got := runStatusJSON(t)
		byName := make(map[string]statusJSON, len(got))
		index := make(map[string]int, len(got))
		for i, e := range got {
			byName[e.Name] = e
			index[e.Name] = i
		}

		// The two local projections come first, in config order.
		if len(got) < 2 || got[0].Name != deployed || got[1].Name != notDeployed {
			t.Fatalf("local projections should lead in config order; got %+v", got)
		}
		if e := byName[deployed]; e.Drift != "in-sync" || e.Runtime == nil {
			t.Errorf("deployed entry = %+v; want in-sync with runtime", e)
		}
		if e := byName[notDeployed]; e.Drift != "not-deployed" || e.Runtime != nil {
			t.Errorf("not-deployed entry = %+v; want not-deployed, no runtime", e)
		}
		for _, n := range []string{untrackedA, untrackedZ} {
			if e := byName[n]; e.Drift != "untracked" || e.Runtime == nil {
				t.Errorf("untracked %s = %+v; want untracked with runtime", n, e)
			}
		}
		// Untracked projections are sorted among themselves.
		if index[untrackedA] > index[untrackedZ] {
			t.Errorf("untracked not sorted: %s at %d, %s at %d", untrackedA, index[untrackedA], untrackedZ, index[untrackedZ])
		}
	})

	t.Run("single deployed", func(t *testing.T) {
		got := runStatusJSON(t, deployed)
		if len(got) != 1 || got[0].Drift != "in-sync" || got[0].Runtime == nil {
			t.Fatalf("got %+v, want one in-sync entry with runtime", got)
		}
	})

	t.Run("single not deployed has no runtime", func(t *testing.T) {
		got := runStatusJSON(t, notDeployed)
		if len(got) != 1 || got[0].Drift != "not-deployed" || got[0].Runtime != nil {
			t.Fatalf("got %+v, want not-deployed with no runtime", got)
		}
	})
}
