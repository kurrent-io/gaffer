//go:build integration

package lsp

import (
	"context"
	"testing"
	"time"

	"github.com/sourcegraph/jsonrpc2"

	"github.com/kurrent-io/gaffer/cli/internal/config"
	"github.com/kurrent-io/gaffer/cli/internal/testutil"
)

// TestFetchDiff_Integration exercises the real diff path (borrow-or-dial ->
// drift.Compare -> serialize) against a live KurrentDB, which the unit tests
// can't reach (they inject a fake fetcher). With no watch connection up for the
// URI, fetchDiff dials a fresh one (the borrow fallback).
func TestFetchDiff_Integration(t *testing.T) {
	root := t.TempDir()
	writeWorkspaceFile(t, root, "checkout.js", "function project(){}")
	s := NewServer(ServerOptions{})

	t.Run("reachable env returns a deployed-vs-local diff", func(t *testing.T) {
		cfg, err := config.Parse([]byte("[env.it]\nconnection = \"" + testutil.ConnectionString() + "\"\n\n" +
			"[[projection]]\nname = \"checkout\"\nentry = \"checkout.js\"\nengine_version = 2\n"))
		if err != nil {
			t.Fatalf("parse config: %v", err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		diff, jerr := s.fetchDiff(ctx, root, cfg, "file:///it/gaffer.toml", "it", "checkout")
		if jerr != nil {
			t.Fatalf("unexpected error against the integration DB: %v", jerr)
		}
		// The sides name themselves regardless of whether checkout is deployed;
		// an undeployed projection is a normal result with an empty deployed side.
		if diff.Left.Ref != "deployed" || diff.Right.Ref != "local" {
			t.Errorf("sides: left=%q right=%q", diff.Left.Ref, diff.Right.Ref)
		}
		if diff.Right.Source == "" {
			t.Error("local side should carry the compiled source")
		}
	})

	t.Run("unreachable env yields an error, not a panic or hang", func(t *testing.T) {
		cfg, err := config.Parse([]byte("[env.dead]\nconnection = \"esdb://127.0.0.1:1?tls=false\"\n\n" +
			"[[projection]]\nname = \"checkout\"\nentry = \"checkout.js\"\nengine_version = 2\n"))
		if err != nil {
			t.Fatalf("parse config: %v", err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		_, jerr := s.fetchDiff(ctx, root, cfg, "file:///dead/gaffer.toml", "dead", "checkout")
		if jerr == nil {
			t.Fatal("expected an error dialing an unreachable host")
		}
		if jerr.Code != jsonrpc2.CodeInternalError {
			t.Errorf("unreachable host should be a generic error, got code %d (%s)", jerr.Code, jerr.Message)
		}
	})
}
