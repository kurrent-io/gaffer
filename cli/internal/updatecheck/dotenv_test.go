package updatecheck

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kurrent-io/gaffer/cli/internal/envvar"
)

// Guards the UI-1651 fix on the update-check side: GAFFER_NO_UPDATE_CHECK
// declared in .env (not the shell) is honoured once .env is loaded into
// the process environment at startup. Mirrors
// TestStart_EnvDisable_NoPrintNoFetch but sources the var from .env via
// envvar.Load, so a regression that stops loading .env for this path
// fails here. Not parallel: it mutates the process environment.
func TestStart_DotEnvDisable_NoPrintNoFetch(t *testing.T) {
	orig, had := os.LookupEnv(EnvDisable)
	_ = os.Unsetenv(EnvDisable)
	t.Cleanup(func() {
		if had {
			_ = os.Setenv(EnvDisable, orig)
		} else {
			_ = os.Unsetenv(EnvDisable)
		}
	})

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(EnvDisable+"=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := envvar.Load(dir); err != nil {
		t.Fatal(err)
	}

	fetcher := &stubFetcher{latest: "0.2.0"}
	c, buf := newTestClient(t, "0.1.3", fetcher)
	if err := SaveCache(c.cacheDir, Cache{
		CheckedAt:          time.Now().Add(-time.Hour),
		CheckedWithVersion: "0.1.3",
		LatestVersion:      "0.2.0",
	}); err != nil {
		t.Fatalf("seed cache: %v", err)
	}

	c.Start(false, false)
	flushOrFail(t, c, time.Second)
	if buf.Len() != 0 {
		t.Errorf("dotenv-disabled printed: %q", buf.String())
	}
	if fetcher.callCount() != 0 {
		t.Errorf("dotenv-disabled fetched %d times, want 0", fetcher.callCount())
	}
}
