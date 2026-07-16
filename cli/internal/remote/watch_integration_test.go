//go:build integration

package remote

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kurrent-io/gaffer/cli/internal/testutil"
)

// TestIntegration_WatchDefinition exercises the real subscription path: a
// projection update writes a $ProjectionUpdated to $projections-<name>, which
// the watch should deliver. Unit tests can't reach this (WatchDefinition drives
// the concrete kurrentdb client).
func TestIntegration_WatchDefinition(t *testing.T) {
	c := connectClient(t)
	ctx := testContext(t)
	name := "watchit" + testutil.TestSuffix()
	t.Cleanup(cleanupProjection(c, name))

	if err := c.Create(ctx, name, countQuery, CreateOptions{EngineVersion: 2}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	fired := make(chan struct{}, 8)
	watchCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() { _ = c.WatchDefinition(watchCtx, name, func() { fired <- struct{}{} }) }()

	// Update on a ticker until the watch (subscribed from the stream's current
	// end) delivers an event, tolerating the race between the subscription
	// establishing and the first update landing.
	deadline := time.After(15 * time.Second)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-fired:
			return
		case <-deadline:
			t.Fatal("WatchDefinition did not fire on a projection update")
		case <-ticker.C:
			if err := c.Update(ctx, name, countQuery, UpdateOptions{Emit: testutil.Ptr(true)}); err != nil {
				t.Fatalf("Update: %v", err)
			}
		}
	}
}

// TestIntegration_WatchDefinition_NonexistentStreamWaits confirms subscribing to
// a not-yet-created definition stream (a projection not deployed on this env)
// waits quietly rather than erroring - so the watch doesn't spin on reconnect.
func TestIntegration_WatchDefinition_NonexistentStreamWaits(t *testing.T) {
	c := connectClient(t)
	name := "watchmissing" + testutil.TestSuffix() // never created

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := c.WatchDefinition(ctx, name, func() {
		t.Error("no event expected for a stream that was never created")
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("a nonexistent stream should subscribe and wait, returning the ctx deadline; got %v", err)
	}
}
