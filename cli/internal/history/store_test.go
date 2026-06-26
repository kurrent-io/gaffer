package history

import (
	"runtime"
	"slices"
	"sync"
	"testing"
)

const (
	testEvent          = `{"eventType":"OrderPlaced","streamId":"order-1","sequenceNumber":0,"data":"{}","isJson":true}`
	testResult         = `{"status":"processed","partition":"order-1","state":{"count":1},"emitted":[],"logs":[]}`
	testResultWithEmit = `{"status":"processed","partition":"order-1","state":{"count":1},"emitted":[{"streamId":"notifications","eventType":"OrderNotification"}],"logs":["hello"]}`
	testResultSkipped  = `{"status":"skipped","reason":"unhandled"}`
	// Two distinct quirk codes plus a repeat of the first, to exercise the
	// dedupe + sort in extractResultFields.
	testResultWithQuirks = `{"status":"processed","partition":"order-1","state":"x","emitted":[],"logs":[],"diagnostics":[{"code":"quirk.log.multiParam","message":"m","severity":2,"range":null},{"code":"quirk.serialize.rawString","message":"m2","severity":2,"range":null},{"code":"quirk.log.multiParam","message":"m","severity":2,"range":null}]}`
)

func mustNew(t *testing.T) *Store {
	t.Helper()
	s, err := New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestInsertAndGet(t *testing.T) {
	s := mustNew(t)

	pos, err := s.Insert(testEvent, testResult)
	if err != nil {
		t.Fatal(err)
	}
	if pos != 1 {
		t.Fatalf("expected position 1, got %d", pos)
	}

	step, err := s.Get(1)
	if err != nil {
		t.Fatal(err)
	}
	if step == nil {
		t.Fatal("expected step, got nil")
	}
	if step.EventType != "OrderPlaced" {
		t.Errorf("EventType = %q, want OrderPlaced", step.EventType)
	}
	if step.StreamID != "order-1" {
		t.Errorf("StreamID = %q, want order-1", step.StreamID)
	}
	if step.Status != "processed" {
		t.Errorf("Status = %q, want processed", step.Status)
	}
	if step.Partition != "order-1" {
		t.Errorf("Partition = %q, want order-1", step.Partition)
	}
}

func TestInsertExtractsEmitAndLog(t *testing.T) {
	s := mustNew(t)

	_, err := s.Insert(testEvent, testResultWithEmit)
	if err != nil {
		t.Fatal(err)
	}

	step, err := s.Get(1)
	if err != nil {
		t.Fatal(err)
	}
	if !step.HasEmit {
		t.Error("expected HasEmit = true")
	}
	if !step.HasLog {
		t.Error("expected HasLog = true")
	}
}

func TestTimelineExtractsQuirks(t *testing.T) {
	s := mustNew(t)

	_, _ = s.Insert(testEvent, testResult)           // no quirks
	_, _ = s.Insert(testEvent, testResultWithQuirks) // two distinct, one repeated

	entries, err := s.Timeline(1, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].Quirks != nil {
		t.Errorf("[0] expected nil Quirks, got %v", entries[0].Quirks)
	}
	want := []string{"quirk.log.multiParam", "quirk.serialize.rawString"}
	if got := entries[1].Quirks; !slices.Equal(got, want) {
		t.Errorf("[1] Quirks = %v, want %v (distinct + sorted)", got, want)
	}
}

func TestInsertSkipped(t *testing.T) {
	s := mustNew(t)

	_, err := s.Insert(testEvent, testResultSkipped)
	if err != nil {
		t.Fatal(err)
	}

	step, err := s.Get(1)
	if err != nil {
		t.Fatal(err)
	}
	if step.Status != "skipped" {
		t.Errorf("Status = %q, want skipped", step.Status)
	}
	if step.HasEmit {
		t.Error("expected HasEmit = false")
	}
}

func TestLatest(t *testing.T) {
	s := mustNew(t)

	_, _ = s.Insert(testEvent, testResult)
	_, _ = s.Insert(testEvent, testResultWithEmit)
	_, _ = s.Insert(testEvent, testResultSkipped)

	step, err := s.Latest()
	if err != nil {
		t.Fatal(err)
	}
	if step.Index != 3 {
		t.Errorf("Index = %d, want 3", step.Index)
	}
	if step.Status != "skipped" {
		t.Errorf("Status = %q, want skipped", step.Status)
	}
}

func TestGetNotFound(t *testing.T) {
	s := mustNew(t)

	step, err := s.Get(999)
	if err != nil {
		t.Fatal(err)
	}
	if step != nil {
		t.Errorf("expected nil, got step at %d", step.Index)
	}
}

func TestTimeline(t *testing.T) {
	s := mustNew(t)

	_, _ = s.Insert(testEvent, testResult)
	_, _ = s.Insert(testEvent, testResultWithEmit)
	_, _ = s.Insert(testEvent, testResultSkipped)

	entries, err := s.Timeline(1, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}
	if entries[0].Status != "processed" {
		t.Errorf("[0] Status = %q, want processed", entries[0].Status)
	}
	if !entries[1].HasEmit {
		t.Error("[1] expected HasEmit = true")
	}
	if entries[2].Status != "skipped" {
		t.Errorf("[2] Status = %q, want skipped", entries[2].Status)
	}
}

func TestEviction(t *testing.T) {
	s, err := NewWithLimit(5)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })

	for range 10 {
		_, _ = s.Insert(testEvent, testResult)
	}

	count, err := s.Count()
	if err != nil {
		t.Fatal(err)
	}
	if count != 5 {
		t.Errorf("Count = %d, want 5", count)
	}

	// Oldest should be evicted
	step, err := s.Get(1)
	if err != nil {
		t.Fatal(err)
	}
	if step != nil {
		t.Error("expected step 1 to be evicted")
	}

	// Latest should exist
	step, err = s.Get(10)
	if err != nil {
		t.Fatal(err)
	}
	if step == nil {
		t.Error("expected step 10 to exist")
	}
}

func TestLatestEmpty(t *testing.T) {
	s := mustNew(t)

	step, err := s.Latest()
	if err != nil {
		t.Fatal(err)
	}
	if step != nil {
		t.Errorf("expected nil, got step at %d", step.Index)
	}
}

func TestRange(t *testing.T) {
	s := mustNew(t)

	min, max, err := s.Range()
	if err != nil {
		t.Fatal(err)
	}
	if min != 0 || max != 0 {
		t.Errorf("empty Range = (%d, %d), want (0, 0)", min, max)
	}

	_, _ = s.Insert(testEvent, testResult)
	_, _ = s.Insert(testEvent, testResultWithEmit)
	_, _ = s.Insert(testEvent, testResultSkipped)

	min, max, err = s.Range()
	if err != nil {
		t.Fatal(err)
	}
	if min != 1 {
		t.Errorf("min = %d, want 1", min)
	}
	if max != 3 {
		t.Errorf("max = %d, want 3", max)
	}
}

func TestTimelineFiltered(t *testing.T) {
	s := mustNew(t)

	eventA := `{"eventType":"OrderPlaced","streamId":"order-1"}`
	resultA := `{"status":"processed","partition":"partition-a","state":{},"emitted":[],"logs":[]}`
	eventB := `{"eventType":"UserCreated","streamId":"user-1"}`
	resultB := `{"status":"processed","partition":"partition-b","state":{},"emitted":[],"logs":[]}`

	_, _ = s.Insert(eventA, resultA)
	_, _ = s.Insert(eventB, resultB)
	_, _ = s.Insert(eventA, resultA)

	entries, err := s.TimelineFiltered(1, 3, "partition-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	for i, e := range entries {
		if e.Partition != "partition-a" {
			t.Errorf("[%d] Partition = %q, want partition-a", i, e.Partition)
		}
	}

	entries, err = s.TimelineFiltered(1, 3, "partition-b")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Partition != "partition-b" {
		t.Errorf("Partition = %q, want partition-b", entries[0].Partition)
	}
}

func TestCount(t *testing.T) {
	s := mustNew(t)

	count, _ := s.Count()
	if count != 0 {
		t.Errorf("empty Count = %d, want 0", count)
	}

	_, _ = s.Insert(testEvent, testResult)
	_, _ = s.Insert(testEvent, testResult)

	count, _ = s.Count()
	if count != 2 {
		t.Errorf("Count = %d, want 2", count)
	}
}

// A ":memory:" database is private to its connection, so reads and writes
// must share one connection or a query lands on an empty one and fails with
// "no such table: steps". This mirrors a live run (background inserter) being
// queried by a concurrent get_timeline. Without SetMaxOpenConns(1) it fails
// within a few dozen iterations.
func TestConcurrentInsertAndQuery(t *testing.T) {
	s := mustNew(t)

	stop := make(chan struct{})
	insertErr := make(chan error, 1)
	var wg sync.WaitGroup
	wg.Go(func() {
		for {
			select {
			case <-stop:
				return
			default:
				if _, err := s.Insert(testEvent, testResult); err != nil {
					select {
					case insertErr <- err:
					default:
					}
					return
				}
				runtime.Gosched()
			}
		}
	})

	for i := range 2000 {
		if _, err := s.TimelineFiltered(1, 100, ""); err != nil {
			close(stop)
			wg.Wait()
			t.Fatalf("timeline query failed at iteration %d: %v", i, err)
		}
	}
	close(stop)
	wg.Wait()

	select {
	case err := <-insertErr:
		t.Fatalf("insert failed: %v", err)
	default:
	}
}
