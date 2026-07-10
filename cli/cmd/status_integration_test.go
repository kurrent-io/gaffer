//go:build integration

package cmd

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kurrent-io/gaffer/cli/internal/cliout"
	"github.com/kurrent-io/gaffer/cli/internal/remote"
	"github.com/kurrent-io/gaffer/cli/internal/testutil"
)

func runStatusJSON(t *testing.T, args ...string) []cliout.StatusJSON {
	t.Helper()
	root := NewRootCmd()
	root.SetArgs(append([]string{"status", "--json"}, args...))
	root.SetErr(os.Stderr)
	out := testutil.CaptureStdout(t, func() {
		if err := ExecuteRoot(context.Background(), root); err != nil {
			t.Fatalf("status: %v", err)
		}
	})
	var report cliout.StatusReportJSON
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("unmarshal status json: %v\n%s", err, out)
	}
	return report.Projections
}

// runStatusReportJSON is runStatusJSON keeping the whole envelope, for the
// env-level fields alongside the projections.
func runStatusReportJSON(t *testing.T, args ...string) cliout.StatusReportJSON {
	t.Helper()
	root := NewRootCmd()
	root.SetArgs(append([]string{"status", "--json"}, args...))
	root.SetErr(os.Stderr)
	out := testutil.CaptureStdout(t, func() {
		if err := ExecuteRoot(context.Background(), root); err != nil {
			t.Fatalf("status: %v", err)
		}
	})
	var report cliout.StatusReportJSON
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("unmarshal status json: %v\n%s", err, out)
	}
	return report
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
		byName := make(map[string]cliout.StatusJSON, len(got))
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
		if got[0].Hash == "" {
			t.Errorf("a deployed entry should carry the content hash, got %+v", got[0])
		}
	})

	t.Run("single not deployed has no runtime", func(t *testing.T) {
		got := runStatusJSON(t, notDeployed)
		if len(got) != 1 || got[0].Drift != "not-deployed" || got[0].Runtime != nil {
			t.Fatalf("got %+v, want not-deployed with no runtime", got)
		}
		if got[0].Hash != "" {
			t.Errorf("a not-deployed entry should carry no hash, got %q", got[0].Hash)
		}
	})

	t.Run("config drift reaches the json envelope", func(t *testing.T) {
		// A [database_config] whose max_state_size can't match any real server
		// makes the drift check fire against the live /info/options read.
		toml := filepath.Join(p.Dir, "gaffer.toml")
		orig, err := os.ReadFile(toml)
		if err != nil {
			t.Fatal(err)
		}
		defer os.WriteFile(toml, orig, 0o644) //nolint:errcheck // best-effort restore
		if err := os.WriteFile(toml, append(orig, []byte("\n[database_config]\nmax_state_size = 1\n")...), 0o644); err != nil {
			t.Fatal(err)
		}

		// The drift check degrades to "no drift" by design when the
		// /info/options read misses its 3s budget (it's advisory - see
		// nodeOptionsHTTPTimeout), and a race-instrumented CI runner under
		// full parallel-package load can miss that window. Retry the whole
		// command a few times so the assertion tests the envelope plumbing,
		// not one 3-second slot on a loaded runner.
		var report cliout.StatusReportJSON
		for attempt := 1; ; attempt++ {
			report = runStatusReportJSON(t)
			if len(report.ConfigDrift) > 0 || attempt == 3 {
				break
			}
			t.Logf("attempt %d: configDrift empty (configDriftError=%q), retrying", attempt, report.ConfigDriftError)
			time.Sleep(2 * time.Second)
		}
		if len(report.ConfigDrift) != 1 || report.ConfigDrift[0].Knob != "max_state_size" || report.ConfigDrift[0].Local != 1 {
			t.Fatalf("configDrift = %+v, want the max_state_size divergence", report.ConfigDrift)
		}
		if report.ConfigDrift[0].Server <= 0 {
			t.Errorf("server value = %d, want the node's live cap", report.ConfigDrift[0].Server)
		}
		// The CLI now populates the self-describing envelope too, resolved against
		// the live server.
		if report.Env == "" || report.Target == "" || report.Production == nil {
			t.Errorf("envelope should carry env/target/production, got env=%q target=%q production=%v", report.Env, report.Target, report.Production)
		}
	})

	t.Run("config drift authenticates with .env credentials", func(t *testing.T) {
		// The UI-1820 repro shape: credentials only in .env.<env>, none in
		// the connection string. Locks the full command path resolving and
		// attaching the .env login to the node-options read - before the fix
		// the read went out anonymous and the failure read as "no drift".
		// (An insecure test node ignores the login; the resolution path is
		// what this case guards.)
		toml := filepath.Join(p.Dir, "gaffer.toml")
		orig, err := os.ReadFile(toml)
		if err != nil {
			t.Fatal(err)
		}
		defer os.WriteFile(toml, orig, 0o644) //nolint:errcheck // best-effort restore

		conn := testutil.ConnectionString()
		u, err := url.Parse(conn)
		if err != nil {
			t.Fatal(err)
		}
		user, pass := "admin", "changeit"
		if u.User != nil {
			user = u.User.Username()
			if pw, ok := u.User.Password(); ok {
				pass = pw
			}
			u.User = nil
		}
		envFile := filepath.Join(p.Dir, ".env.default")
		if err := os.WriteFile(envFile, []byte("KURRENTDB_USERNAME="+user+"\nKURRENTDB_PASSWORD="+pass+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		defer os.Remove(envFile) //nolint:errcheck // best-effort cleanup

		updated := strings.Replace(string(orig), conn, u.String(), 1) + "\n[database_config]\nmax_state_size = 1\n"
		if err := os.WriteFile(toml, []byte(updated), 0o644); err != nil {
			t.Fatal(err)
		}

		var report cliout.StatusReportJSON
		for attempt := 1; ; attempt++ {
			report = runStatusReportJSON(t)
			if len(report.ConfigDrift) > 0 || attempt == 3 {
				break
			}
			t.Logf("attempt %d: configDrift empty (configDriftError=%q), retrying", attempt, report.ConfigDriftError)
			time.Sleep(2 * time.Second)
		}
		if report.ConfigDriftError != "" {
			t.Fatalf("configDriftError = %q, want the check running on .env credentials", report.ConfigDriftError)
		}
		if len(report.ConfigDrift) != 1 || report.ConfigDrift[0].Knob != "max_state_size" {
			t.Fatalf("configDrift = %+v, want the divergence detected via .env credentials", report.ConfigDrift)
		}
	})
}
