//go:build integration

package cmd

import (
	"context"
	"os"
	"testing"

	"github.com/kurrent-io/gaffer/cli/internal/cliout"
	"github.com/kurrent-io/gaffer/cli/internal/remote"
	"github.com/kurrent-io/gaffer/cli/internal/testutil"
)

// TestStatus_LedgerAware_Integration drives gaffer status against a metadata-capable
// server and checks the ownership + drift-attribution it reads from the ledger. It's
// gated on GAFFER_TEST_LEDGER (a release that ignores the metadata field can't carry
// the assertions); run with it set and KURRENTDB_URL pointed at the nightly.
func TestStatus_LedgerAware_Integration(t *testing.T) {
	if os.Getenv("GAFFER_TEST_LEDGER") == "" {
		t.Skip("set GAFFER_TEST_LEDGER and point KURRENTDB_URL at a metadata-capable KurrentDB (master/nightly)")
	}
	r := diffSetupClient(t)
	ctx := context.Background()
	suffix := testutil.TestSuffix()
	insync := "ledinsync" + suffix
	ahead := "ledahead" + suffix
	server := "ledserver" + suffix
	orphan := "ledorphan" + suffix
	foreign := "ledforeign" + suffix
	const src = "fromAll().when({ $any: function (s, e) { return s; } })\n"
	const src2 = "fromAll().when({ $any: function (s, e) { return e; } })\n" // compiles, query differs
	gaffer := func() *remote.Ledger {
		return &remote.Ledger{Tool: remote.ToolName, ToolVersion: "0.0.0-test", Operation: remote.OpDeploy, Actor: "admin"}
	}

	// insync + ahead + server are in local config; orphan + foreign are server-only.
	// ahead's local source is edited away from what's deployed; server's deployed def
	// is later changed by a metadata-less write.
	p := testutil.NewProject(t).
		WithConnection(testutil.ConnectionString()).
		AddProjection(insync, src).
		AddProjection(ahead, src2).
		AddProjection(server, src).
		Save()
	chdirTo(t, p.Dir)
	for _, n := range []string{insync, ahead, server, orphan, foreign} {
		cleanupRemote(t, r, n)
	}

	mustCreate := func(name, source string, l *remote.Ledger) {
		t.Helper()
		if err := r.Create(ctx, name, source, remote.CreateOptions{EngineVersion: 2, Ledger: l}); err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
	}
	mustCreate(insync, src, gaffer()) // deployed == local
	mustCreate(ahead, src, gaffer())  // deployed == my last deploy; local (src2) edited since
	mustCreate(server, src, gaffer()) // gaffer deployed src...
	if err := r.Update(ctx, server, src2, remote.UpdateOptions{Emit: testutil.Ptr(false)}); err != nil {
		t.Fatalf("update server (no ledger): %v", err) // ...then a metadata-less write changed it
	}
	mustCreate(orphan, src, gaffer())                                                      // server-only, gaffer's metadata
	mustCreate(foreign, src, &remote.Ledger{Tool: "KurrentDB Embedded UI", Actor: "jane"}) // server-only, another tool's

	byName := make(map[string]cliout.StatusJSON)
	for _, e := range runStatusJSON(t) {
		byName[e.Name] = e
	}
	check := func(name, owner, drift, attr string) {
		t.Helper()
		e, ok := byName[name]
		if !ok {
			t.Fatalf("%s missing from status", name)
		}
		if e.Owner != owner || e.Drift != drift || e.Attribution != attr {
			t.Errorf("%s = owner %q / drift %q / attribution %q; want %q / %q / %q",
				name, e.Owner, e.Drift, e.Attribution, owner, drift, attr)
		}
	}
	check(insync, "in-config", "in-sync", "")
	check(ahead, "in-config", "drifted", "local-ahead")
	check(server, "in-config", "drifted", "changed-server")
	check(orphan, "orphan", "untracked", "")
	check(foreign, "foreign", "untracked", "")

	if e := byName[orphan]; e.LastWrite == nil || e.LastWrite.Tool != remote.ToolName {
		t.Errorf("orphan lastWrite = %+v, want gaffer", e.LastWrite)
	}
	if e := byName[foreign]; e.LastWrite == nil || e.LastWrite.Tool != "KurrentDB Embedded UI" {
		t.Errorf("foreign lastWrite = %+v, want the foreign tool", e.LastWrite)
	}
	// The deploy time round-trips from the event for every deployed projection.
	for _, name := range []string{insync, orphan, foreign} {
		if e := byName[name]; e.LastDeployed == "" {
			t.Errorf("%s lastDeployed empty; want the event time", name)
		}
	}
}
