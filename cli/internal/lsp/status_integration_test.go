//go:build integration

package lsp

import (
	"context"
	"testing"
	"time"

	"github.com/kurrent-io/gaffer/cli/internal/config"
	"github.com/kurrent-io/gaffer/cli/internal/testutil"
)

// TestFetchEnvStatus_Integration exercises the real fetch path (dial ->
// StatusAll -> OperateTarget) against a live KurrentDB, which the unit tests
// can't reach (they inject a fake fetcher).
func TestFetchEnvStatus_Integration(t *testing.T) {
	root := t.TempDir()

	t.Run("reachable env yields entries and a target", func(t *testing.T) {
		cfg, err := config.Parse([]byte("[env.it]\nconnection = \"" + testutil.ConnectionString() + "\"\n"))
		if err != nil {
			t.Fatalf("parse config: %v", err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		st := fetchEnvStatus(ctx, root, cfg, "it")
		if st.Err != nil {
			t.Fatalf("unexpected error against the integration DB: %v", st.Err)
		}
		if st.Unauthenticated {
			t.Fatal("an insecure integration env should not read as unauthenticated")
		}
		if st.Target == "" {
			t.Error("expected a resolved target (cluster name or env name fallback)")
		}
		// Entries may legitimately be empty (nothing deployed); the point is
		// that the read completed without error and resolved the target.
	})

	t.Run("unreachable env yields an error, not a panic or hang", func(t *testing.T) {
		cfg, err := config.Parse([]byte("[env.dead]\nconnection = \"esdb://127.0.0.1:1?tls=false\"\n"))
		if err != nil {
			t.Fatalf("parse config: %v", err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		st := fetchEnvStatus(ctx, root, cfg, "dead")
		if st.Err == nil {
			t.Fatal("expected an error dialing an unreachable host")
		}
	})
}
