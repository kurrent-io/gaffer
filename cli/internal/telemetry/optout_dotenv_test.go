package telemetry

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kurrent-io/gaffer/cli/internal/envvar"
)

// Guards the UI-1651 fix: a .env-declared opt-out is honoured. Before
// .env was loaded into the process environment at startup, CheckOptOut
// (which reads the process env) never saw it - .env only reached the DB
// connection path. This exercises the real (non-seam) env layer so a
// regression that stops loading .env, or reorders it after opt-out
// resolution, fails here.
//
// Not parallel: it mutates the process environment via envvar.Load. The
// other opt-out tests use the checkOptOutWithEnv lookup seam and never
// touch these vars, so there's no collision.
func TestCheckOptOut_HonoursDotEnvOptOut(t *testing.T) {
	const key = "GAFFER_TELEMETRY_OPTOUT"
	orig, had := os.LookupEnv(key)
	_ = os.Unsetenv(key)
	t.Cleanup(func() {
		if had {
			_ = os.Setenv(key, orig)
		} else {
			_ = os.Unsetenv(key)
		}
	})

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(key+"=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Before loading .env, the env layer has no opinion.
	if got := CheckOptOut(nil, dir, "").Env.State; got != LayerUnset {
		t.Fatalf("pre-load: expected env layer unset, got %v", got)
	}

	if err := envvar.Load(dir); err != nil {
		t.Fatal(err)
	}

	// After loading, the .env opt-out is visible to the env layer.
	if resolved := CheckOptOut(nil, dir, ""); !resolved.IsDisabled() {
		t.Fatalf("expected telemetry disabled after loading .env opt-out, got %v", resolved.Env.State)
	}
}
