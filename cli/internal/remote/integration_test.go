//go:build integration

package remote

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kurrent-io/KurrentDB-Client-Go/kurrentdb"
	"github.com/kurrent-io/gaffer/cli/internal/testutil"
)

// connectClient dials the integration KurrentDB (KURRENTDB_URL or localhost) and
// wraps it in a remote.Client.
func connectClient(t *testing.T) *Client {
	t.Helper()
	cfg, err := kurrentdb.ParseConnectionString(testutil.ConnectionString())
	if err != nil {
		t.Fatalf("parse connection: %v", err)
	}
	cfg.Logger = kurrentdb.NoopLogging()
	db, err := kurrentdb.NewClient(cfg)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return New(db)
}

// waitForState polls until the projection reaches want, or fails after a few
// seconds. Projection lifecycle transitions are asynchronous on the server.
func waitForState(t *testing.T, c *Client, name string, want State) Status {
	t.Helper()
	ctx := context.Background()
	deadline := time.Now().Add(15 * time.Second)
	var last Status
	for time.Now().Before(deadline) {
		st, err := c.Status(ctx, name)
		if err == nil {
			last = *st
			if st.State == want {
				return *st
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("projection %q never reached %s; last status = %+v", name, want, last)
	return Status{}
}

const countQuery = `fromAll().when({ $init() { return { count: 0 }; }, $any(s, e) { s.count++; return s; } })`

// cleanupProjection best-effort removes a projection and its streams. A
// projection must be disabled before it can be deleted, so disable first.
func cleanupProjection(c *Client, name string) func() {
	return func() {
		ctx := context.Background()
		_ = c.Disable(ctx, name)
		_ = c.Delete(ctx, name, DeleteOptions{DeleteEmittedStreams: true, DeleteStateStream: true, DeleteCheckpointStream: true})
	}
}

func TestIntegration_CreateReadUpdateDelete(t *testing.T) {
	c := connectClient(t)
	ctx := context.Background()
	name := "remoteit" + testutil.TestSuffix()
	t.Cleanup(cleanupProjection(c, name))

	if err := c.Create(ctx, name, countQuery, CreateOptions{EngineVersion: 2, Emit: false}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	ok, err := c.Exists(ctx, name)
	if err != nil || !ok {
		t.Fatalf("Exists after create = %v, %v; want true", ok, err)
	}

	def, err := c.Read(ctx, name)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if def.Query != countQuery {
		t.Errorf("Read query = %q, want the deployed query", def.Query)
	}
	if def.EngineVersion != 2 {
		t.Errorf("Read engineVersion = %d, want 2 (the version Create requested)", def.EngineVersion)
	}
	if def.Emit {
		t.Errorf("Read emit = true, want false")
	}

	// A duplicate create is NOT reported as ErrAlreadyExists - the subsystem
	// replies Conflict, which surfaces unclassified. Pin that documented reality.
	dupErr := c.Create(ctx, name, countQuery, CreateOptions{})
	if dupErr == nil {
		t.Errorf("duplicate Create unexpectedly succeeded")
	}
	if errors.Is(dupErr, ErrAlreadyExists) {
		t.Errorf("duplicate Create classified as ErrAlreadyExists; expected unclassified (got %v)", dupErr)
	}

	// Update flips emit on; Read reflects it.
	if err := c.Update(ctx, name, countQuery, UpdateOptions{Emit: testutil.Ptr(true)}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	def, err = c.Read(ctx, name)
	if err != nil {
		t.Fatalf("Read after update: %v", err)
	}
	if !def.Emit {
		t.Errorf("Read emit after update = false, want true")
	}

	// A projection must be disabled before it can be deleted; deleting an
	// enabled one is rejected by the server.
	if err := c.Disable(ctx, name); err != nil {
		t.Fatalf("Disable before delete: %v", err)
	}
	waitForState(t, c, name, StateStopped)
	if err := c.Delete(ctx, name, DeleteOptions{}); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	// After delete the persisted state carries deleting/deleted, which Read maps
	// to ErrNotFound.
	deadline := time.Now().Add(10 * time.Second)
	for {
		_, err = c.Read(ctx, name)
		if errors.Is(err, ErrNotFound) || time.Now().After(deadline) {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("Read after delete = %v, want ErrNotFound", err)
	}
}

func TestIntegration_StatusAndList(t *testing.T) {
	c := connectClient(t)
	ctx := context.Background()
	name := "remoteit" + testutil.TestSuffix()
	t.Cleanup(cleanupProjection(c, name))

	if err := c.Create(ctx, name, countQuery, CreateOptions{}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	waitForState(t, c, name, StateRunning)

	if err := c.Disable(ctx, name); err != nil {
		t.Fatalf("Disable: %v", err)
	}
	waitForState(t, c, name, StateStopped)

	if err := c.Enable(ctx, name); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	waitForState(t, c, name, StateRunning)

	list, err := c.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	var found bool
	for _, s := range list {
		if strings.HasPrefix(s.Name, "$") {
			t.Errorf("List leaked a system projection: %q", s.Name)
		}
		if s.Name == name {
			found = true
		}
	}
	if !found {
		t.Errorf("List did not include %q", name)
	}

	// Status still resolves a system projection by exact name, even though List
	// hides them - proves statuses fetches the full set. Standard projections are
	// started by the integration KurrentDB config.
	if _, err := c.Status(ctx, "$by_category"); err != nil {
		t.Errorf("Status($by_category) = %v, want it found", err)
	}
}

func TestIntegration_FaultedProjection(t *testing.T) {
	c := connectClient(t)
	ctx := context.Background()
	suffix := testutil.TestSuffix()
	name := "remoteit" + suffix
	t.Cleanup(cleanupProjection(c, name))

	// Append an event the projection will choke on, then deploy a projection that
	// throws when it processes it.
	streamID := fmt.Sprintf("fault%s-1", suffix)
	_, err := c.db.AppendToStream(ctx, streamID, kurrentdb.AppendToStreamOptions{}, kurrentdb.EventData{
		EventID:     uuid.New(),
		EventType:   "Boom",
		ContentType: kurrentdb.ContentTypeJson,
		Data:        []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("append: %v", err)
	}

	throwQuery := fmt.Sprintf(`fromCategory('fault%s').when({ Boom(s, e) { throw new Error("kaboom"); } })`, suffix)
	if err := c.Create(ctx, name, throwQuery, CreateOptions{}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	st := waitForState(t, c, name, StateFaulted)
	if st.FaultReason == "" {
		t.Errorf("faulted projection has empty FaultReason; raw status = %q", st.Raw)
	}
}

func TestIntegration_NotFound(t *testing.T) {
	c := connectClient(t)
	ctx := context.Background()
	missing := "remoteit_missing" + testutil.TestSuffix()

	if _, err := c.Status(ctx, missing); !errors.Is(err, ErrNotFound) {
		t.Errorf("Status(missing) = %v, want ErrNotFound", err)
	}
	if _, err := c.Read(ctx, missing); !errors.Is(err, ErrNotFound) {
		t.Errorf("Read(missing) = %v, want ErrNotFound", err)
	}
	if ok, err := c.Exists(ctx, missing); err != nil || ok {
		t.Errorf("Exists(missing) = %v, %v; want false, nil", ok, err)
	}
}
