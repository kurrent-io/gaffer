package subscription

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/kurrent-io/KurrentDB-Client-Go/kurrentdb"
)

func TestMapEvent_JSON(t *testing.T) {
	resolved := &kurrentdb.ResolvedEvent{
		Event: &kurrentdb.RecordedEvent{
			EventID:     uuid.MustParse("550e8400-e29b-41d4-a716-446655440000"),
			EventType:   "OrderPlaced",
			ContentType: "application/json",
			StreamID:    "order-1",
			EventNumber: 5,
			Data:        []byte(`{"amount":50}`),
		},
	}

	result, err := MapEvent(resolved)
	if err != nil {
		t.Fatal(err)
	}

	var obj map[string]any
	if err := json.Unmarshal([]byte(result), &obj); err != nil {
		t.Fatal(err)
	}

	assertEqual(t, "eventType", "OrderPlaced", obj["eventType"])
	assertEqual(t, "streamId", "order-1", obj["streamId"])
	assertEqualFloat(t, "sequenceNumber", 5, obj["sequenceNumber"])
	assertEqual(t, "isJson", true, obj["isJson"])

	if _, ok := obj["linkMetadata"]; ok {
		t.Error("expected no linkMetadata for non-link event")
	}
}

func TestMapEvent_NonJSON(t *testing.T) {
	resolved := &kurrentdb.ResolvedEvent{
		Event: &kurrentdb.RecordedEvent{
			EventType:   "BinaryEvent",
			ContentType: "application/octet-stream",
			StreamID:    "stream-1",
			EventNumber: 3,
			Data:        []byte("raw binary data"),
		},
	}

	result, err := MapEvent(resolved)
	if err != nil {
		t.Fatal(err)
	}

	var obj map[string]any
	if err := json.Unmarshal([]byte(result), &obj); err != nil {
		t.Fatal(err)
	}

	assertEqual(t, "isJson", false, obj["isJson"])
	assertEqual(t, "data", "raw binary data", obj["data"])
}

func TestMapEvent_NoData(t *testing.T) {
	resolved := &kurrentdb.ResolvedEvent{
		Event: &kurrentdb.RecordedEvent{
			EventType:   "Empty",
			ContentType: "application/json",
			StreamID:    "stream-1",
			EventNumber: 0,
		},
	}

	result, err := MapEvent(resolved)
	if err != nil {
		t.Fatal(err)
	}

	var obj map[string]any
	if err := json.Unmarshal([]byte(result), &obj); err != nil {
		t.Fatal(err)
	}

	if obj["data"] != nil {
		t.Errorf("expected null data, got %v", obj["data"])
	}
}

func TestMapEvent_WithMetadata(t *testing.T) {
	resolved := &kurrentdb.ResolvedEvent{
		Event: &kurrentdb.RecordedEvent{
			EventType:    "Test",
			ContentType:  "application/json",
			StreamID:     "stream-1",
			EventNumber:  1,
			Data:         []byte(`{"x":1}`),
			UserMetadata: []byte(`{"correlationId":"abc"}`),
		},
	}

	result, err := MapEvent(resolved)
	if err != nil {
		t.Fatal(err)
	}

	var obj map[string]any
	if err := json.Unmarshal([]byte(result), &obj); err != nil {
		t.Fatal(err)
	}

	if _, ok := obj["metadata"]; !ok {
		t.Error("expected metadata field")
	}
}

func TestMapEvent_WithLink(t *testing.T) {
	resolved := &kurrentdb.ResolvedEvent{
		Event: &kurrentdb.RecordedEvent{
			EventType:   "OrderPlaced",
			ContentType: "application/json",
			StreamID:    "order-1",
			EventNumber: 5,
			Data:        []byte(`{"amount":50}`),
		},
		Link: &kurrentdb.RecordedEvent{
			EventType:   "$>",
			StreamID:    "$ce-order",
			EventNumber: 10,
			Data:        []byte("5@order-1"),
		},
	}

	result, err := MapEvent(resolved)
	if err != nil {
		t.Fatal(err)
	}

	var obj map[string]any
	if err := json.Unmarshal([]byte(result), &obj); err != nil {
		t.Fatal(err)
	}

	linkMeta, ok := obj["linkMetadata"].(map[string]any)
	if !ok {
		t.Fatal("expected linkMetadata object")
	}
	assertEqual(t, "linkMetadata.streamId", "$ce-order", linkMeta["streamId"])
}

func TestMapEvent_NilEvent(t *testing.T) {
	resolved := &kurrentdb.ResolvedEvent{}

	result, err := MapEvent(resolved)
	if err != nil {
		t.Fatal(err)
	}
	if result != "" {
		t.Errorf("expected empty string for nil event, got %q", result)
	}
}

func assertEqual[T comparable](t *testing.T, name string, want, got T) {
	t.Helper()
	if got != want {
		t.Errorf("%s: got %v, want %v", name, got, want)
	}
}

func assertEqualFloat(t *testing.T, name string, want float64, got any) {
	t.Helper()
	f, ok := got.(float64)
	if !ok {
		t.Errorf("%s: expected float64, got %T", name, got)
		return
	}
	if f != want {
		t.Errorf("%s: got %v, want %v", name, f, want)
	}
}
