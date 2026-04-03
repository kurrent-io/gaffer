package engine

import "testing"

func TestParseEvent(t *testing.T) {
	e := ParseEvent(`{"sequenceNumber":5,"streamId":"order-1","eventType":"OrderPlaced","data":{"amount":50},"metadata":{"corr":"abc"}}`)

	if e.SequenceNumber != 5 {
		t.Errorf("sequenceNumber: got %d, want 5", e.SequenceNumber)
	}
	if e.StreamID != "order-1" {
		t.Errorf("streamId: got %q, want %q", e.StreamID, "order-1")
	}
	if e.EventType != "OrderPlaced" {
		t.Errorf("eventType: got %q, want %q", e.EventType, "OrderPlaced")
	}
	if e.ID() != "5@order-1" {
		t.Errorf("ID(): got %q, want %q", e.ID(), "5@order-1")
	}
	if len(e.Data) == 0 {
		t.Error("expected data")
	}
	if len(e.Metadata) == 0 {
		t.Error("expected metadata")
	}
}

func TestParseEvent_Minimal(t *testing.T) {
	e := ParseEvent(`{"eventType":"Test","streamId":"s-1"}`)

	if e.SequenceNumber != 0 {
		t.Errorf("sequenceNumber: got %d, want 0", e.SequenceNumber)
	}
	if e.ID() != "0@s-1" {
		t.Errorf("ID(): got %q, want %q", e.ID(), "0@s-1")
	}
	if len(e.Data) != 0 {
		t.Error("expected no data")
	}
}
