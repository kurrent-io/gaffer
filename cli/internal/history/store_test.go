package history

import (
	"testing"
)

const (
	testEvent          = `{"eventType":"OrderPlaced","streamId":"order-1","sequenceNumber":0,"data":"{}","isJson":true}`
	testResult         = `{"status":"processed","partition":"order-1","state":{"count":1},"emitted":[],"logs":[]}`
	testResultWithEmit = `{"status":"processed","partition":"order-1","state":{"count":1},"emitted":[{"streamId":"notifications","eventType":"OrderNotification"}],"logs":["hello"]}`
	testResultSkipped  = `{"status":"skipped","reason":"unhandled"}`
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
	if step.Position != 3 {
		t.Errorf("Position = %d, want 3", step.Position)
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
		t.Errorf("expected nil, got step at position %d", step.Position)
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

	for i := 0; i < 10; i++ {
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
		t.Error("expected position 1 to be evicted")
	}

	// Latest should exist
	step, err = s.Get(10)
	if err != nil {
		t.Fatal(err)
	}
	if step == nil {
		t.Error("expected position 10 to exist")
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
