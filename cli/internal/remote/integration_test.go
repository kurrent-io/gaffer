//go:build integration

package remote

import (
	"context"
	"errors"
	"fmt"
	"os"
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

// testContext returns a context bounded by a generous deadline, so a stalled
// projections subsystem fails the test rather than hanging it - the remote
// package documents that its RPCs block until the context deadline.
func testContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// waitForStatus polls until pred holds, or fails after a few seconds.
// Projection lifecycle transitions are asynchronous on the server. desc names
// the awaited condition for the timeout message.
func waitForStatus(t *testing.T, ctx context.Context, c *Client, name, desc string, pred func(Status) bool) Status {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	var last Status
	var lastErr error
	for time.Now().Before(deadline) {
		st, err := c.Status(ctx, name)
		if err != nil {
			lastErr = err
		} else {
			last = *st
			if pred(*st) {
				return *st
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("projection %q never %s; last status = %+v, last error = %v", name, desc, last, lastErr)
	return Status{}
}

// waitForState polls until the projection reaches want, or fails after a few
// seconds.
func waitForState(t *testing.T, ctx context.Context, c *Client, name string, want State) Status {
	t.Helper()
	return waitForStatus(t, ctx, c, name, fmt.Sprintf("reached %s", want), func(st Status) bool {
		return st.State == want
	})
}

const countQuery = `fromAll().when({ $init() { return { count: 0 }; }, $any(s, e) { s.count++; return s; } })`

// cleanupProjection best-effort removes a projection and its streams. A
// projection must be disabled before it can be deleted, so disable first.
func cleanupProjection(c *Client, name string) func() {
	return func() {
		// Cleanup runs after the test body, when its context may be done, so use
		// a fresh bounded one.
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = c.Disable(ctx, name)
		_ = c.Delete(ctx, name, DeleteOptions{DeleteEmittedStreams: true, DeleteStateStream: true, DeleteCheckpointStream: true})
	}
}

func TestIntegration_CreateReadUpdateDelete(t *testing.T) {
	c := connectClient(t)
	ctx := testContext(t)
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
	waitForState(t, ctx, c, name, StateStopped)
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
	ctx := testContext(t)
	name := "remoteit" + testutil.TestSuffix()
	t.Cleanup(cleanupProjection(c, name))

	if err := c.Create(ctx, name, countQuery, CreateOptions{}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	waitForState(t, ctx, c, name, StateRunning)

	if err := c.Disable(ctx, name); err != nil {
		t.Fatalf("Disable: %v", err)
	}
	waitForState(t, ctx, c, name, StateStopped)

	if err := c.Enable(ctx, name); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	waitForState(t, ctx, c, name, StateRunning)

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
	ctx := testContext(t)
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

	// The server reports Faulted before it populates the reason: the status
	// passes through a transitional "Running/FaultedStopping" substate where it
	// classifies as faulted but StateReason is still empty. Wait for the reason
	// to settle, not just the state.
	waitForStatus(t, ctx, c, name, "faulted with a reason", func(st Status) bool {
		return st.State == StateFaulted && st.FaultReason != ""
	})
}

// TestIntegration_Ledger is the live round-trip for the deploy ledger. It needs a
// metadata-capable server (the engine caller-metadata change, master / nightly), so
// it's gated on GAFFER_TEST_LEDGER - against a release that ignores the field the
// round-trip assertions can't hold. Run locally with that set and KURRENTDB_URL
// pointed at the nightly; CI exercises it once a metadata-capable image is wired in.
func TestIntegration_Ledger(t *testing.T) {
	if os.Getenv("GAFFER_TEST_LEDGER") == "" {
		t.Skip("set GAFFER_TEST_LEDGER and point KURRENTDB_URL at a metadata-capable KurrentDB (master/nightly)")
	}
	c := connectClient(t)
	ctx := testContext(t)
	name := "ledgerit" + testutil.TestSuffix()
	t.Cleanup(cleanupProjection(c, name))

	// Create stamps a full ledger; ReadLedger returns it with the event's write
	// time and the caller keys intact alongside the server's synthetic $ keys.
	create := Ledger{Tool: ToolName, ToolVersion: "1.2.3", Operation: OpDeploy, Revision: "abc123+changes", Actor: "admin"}
	if err := c.Create(ctx, name, countQuery, CreateOptions{EngineVersion: 2, Ledger: &create}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, def, err := c.ReadLedger(ctx, name)
	if err != nil {
		t.Fatalf("ReadLedger after create: %v", err)
	}
	if got.Tool != ToolName || got.ToolVersion != "1.2.3" || got.Operation != OpDeploy || got.Revision != "abc123+changes" || got.Actor != "admin" {
		t.Errorf("ReadLedger = %+v, want the create ledger round-tripped", got)
	}
	if got.Time.IsZero() {
		t.Error("ReadLedger Time is zero; want the event's write time")
	}
	// The returned definition is the baseline (what this entry deployed) drift
	// attribution compares the current deployed definition against.
	if def == nil || def.Query != countQuery {
		t.Errorf("ReadLedger definition = %+v, want the deployed query %q", def, countQuery)
	}

	// Update writes a newer ledger; ReadLedger returns the latest.
	update := Ledger{Tool: ToolName, ToolVersion: "1.2.4", Operation: OpDeploy, Revision: "def456", Actor: "bob"}
	if err := c.Update(ctx, name, countQuery, UpdateOptions{Emit: testutil.Ptr(false), Ledger: &update}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got, _, err = c.ReadLedger(ctx, name)
	if err != nil {
		t.Fatalf("ReadLedger after update: %v", err)
	}
	if got.Revision != "def456" || got.Actor != "bob" || got.ToolVersion != "1.2.4" {
		t.Errorf("ReadLedger = %+v, want the update ledger (the newest tool entry)", got)
	}

	// The keystone assumption: lifecycle ops write metadata-less $ProjectionUpdated
	// events. After a disable+enable, ReadLedger must scan past them and still
	// return the last deploy's ledger - not ErrNoLedger, not a stale create.
	if err := c.Disable(ctx, name); err != nil {
		t.Fatalf("Disable: %v", err)
	}
	waitForState(t, ctx, c, name, StateStopped)
	if err := c.Enable(ctx, name); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	waitForState(t, ctx, c, name, StateRunning)
	got, _, err = c.ReadLedger(ctx, name)
	if err != nil {
		t.Fatalf("ReadLedger after lifecycle ops: %v (lifecycle writes must not bury the ledger)", err)
	}
	if got.Revision != "def456" || got.Actor != "bob" {
		t.Errorf("ReadLedger after lifecycle = %+v, want the last deploy's ledger (scan past metadata-less writes)", got)
	}
}

// TestIntegration_LedgerAbsent pins the degraded outcomes on a metadata-capable
// server: a projection deployed without a ledger reads back ErrNoLedger, and a
// missing projection reads back ErrNotFound (kept distinct).
func TestIntegration_LedgerAbsent(t *testing.T) {
	if os.Getenv("GAFFER_TEST_LEDGER") == "" {
		t.Skip("set GAFFER_TEST_LEDGER and point KURRENTDB_URL at a metadata-capable KurrentDB (master/nightly)")
	}
	c := connectClient(t)
	ctx := testContext(t)
	name := "ledgerit" + testutil.TestSuffix()
	t.Cleanup(cleanupProjection(c, name))

	if err := c.Create(ctx, name, countQuery, CreateOptions{}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, _, err := c.ReadLedger(ctx, name); !errors.Is(err, ErrNoLedger) {
		t.Errorf("ReadLedger on a ledger-less projection = %v, want ErrNoLedger", err)
	}

	missing := "ledgerit_missing" + testutil.TestSuffix()
	if _, _, err := c.ReadLedger(ctx, missing); !errors.Is(err, ErrNotFound) {
		t.Errorf("ReadLedger on a missing projection = %v, want ErrNotFound", err)
	}
}

func TestIntegration_NotFound(t *testing.T) {
	c := connectClient(t)
	ctx := testContext(t)
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
