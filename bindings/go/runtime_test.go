package gafferruntime

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func mustCreateSession(t *testing.T, source string) *Session {
	t.Helper()
	session, err := NewSession(source, nil)
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}
	t.Cleanup(func() { session.Destroy() })
	return session
}

func mustFeed(t *testing.T, session *Session, eventJSON string) *FeedResult {
	t.Helper()
	result, err := session.Feed(eventJSON)
	if err != nil {
		t.Fatalf("Feed failed: %v", err)
	}
	return result
}

func mustGetState(t *testing.T, session *Session, partition *string) string {
	t.Helper()
	state := session.GetState(partition)
	if state == nil {
		t.Fatal("GetState returned nil")
		return ""
	}
	return *state
}

func TestSessionCreateAndDestroy(t *testing.T) {
	session, err := NewSession(`
		fromAll().when({
			$init: function() { return {}; },
			Ping: function(s, e) { return s; }
		})
	`, nil)
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}
	session.Destroy()
}

func TestCreateWithInvalidJS(t *testing.T) {
	_, err := NewSession("this is not valid {{{{", nil)
	if err == nil {
		t.Fatal("expected error for invalid JS")
	}
	var projErr *InvalidProjectionError
	if !errors.As(err, &projErr) {
		t.Fatalf("expected InvalidProjectionError, got %T", err)
	}
	if projErr.Location == nil {
		t.Fatal("expected location on parse error")
	}
}

func TestFeedAndGetState(t *testing.T) {
	session := mustCreateSession(t, `
		fromAll().when({
			$init: function() { return { count: 0 }; },
			ItemAdded: function(s, e) { s.count++; return s; }
		})
	`)

	mustFeed(t, session, `{"eventType":"ItemAdded","streamId":"cart-1","sequenceNumber":0,"data":"{}","isJson":true,"eventId":"00000000-0000-0000-0000-000000000000","created":"2026-01-01T00:00:00Z"}`)
	mustFeed(t, session, `{"eventType":"ItemAdded","streamId":"cart-1","sequenceNumber":0,"data":"{}","isJson":true,"eventId":"00000000-0000-0000-0000-000000000000","created":"2026-01-01T00:00:00Z"}`)
	mustFeed(t, session, `{"eventType":"ItemAdded","streamId":"cart-1","sequenceNumber":0,"data":"{}","isJson":true,"eventId":"00000000-0000-0000-0000-000000000000","created":"2026-01-01T00:00:00Z"}`)

	state := mustGetState(t, session, nil)
	if state != `{"count":3}` {
		t.Fatalf("expected {\"count\":3}, got %s", state)
	}
}

func TestEventDataAccessible(t *testing.T) {
	session := mustCreateSession(t, `
		fromAll().when({
			$init: function() { return { total: 0 }; },
			Deposited: function(s, e) { s.total += e.data.amount; return s; }
		})
	`)

	mustFeed(t, session, `{"eventType":"Deposited","streamId":"acc-1","sequenceNumber":0,"data":"{\"amount\":50}","isJson":true,"eventId":"00000000-0000-0000-0000-000000000000","created":"2026-01-01T00:00:00Z"}`)
	mustFeed(t, session, `{"eventType":"Deposited","streamId":"acc-1","sequenceNumber":0,"data":"{\"amount\":30}","isJson":true,"eventId":"00000000-0000-0000-0000-000000000000","created":"2026-01-01T00:00:00Z"}`)

	state := mustGetState(t, session, nil)
	if state != `{"total":80}` {
		t.Fatalf("expected {\"total\":80}, got %s", state)
	}
}

func TestGetSources(t *testing.T) {
	session := mustCreateSession(t, `
		fromAll().foreachStream().when({
			$init: function() { return {}; },
			Ping: function(s, e) { return s; }
		})
	`)

	sources := session.GetSources()
	if sources == nil {
		t.Fatal("GetSources returned nil")
		return
	}
	if !strings.Contains(*sources, `"ByStreams":true`) {
		t.Fatalf("expected ByStreams:true in sources, got %s", *sources)
	}
}

func TestForeachStreamPartitioning(t *testing.T) {
	session := mustCreateSession(t, `
		fromCategory("cart").foreachStream().when({
			$init: function() { return { items: 0 }; },
			ItemAdded: function(s, e) { s.items++; return s; }
		})
	`)

	mustFeed(t, session, `{"eventType":"ItemAdded","streamId":"cart-1","sequenceNumber":0,"data":"{}","isJson":true,"eventId":"00000000-0000-0000-0000-000000000000","created":"2026-01-01T00:00:00Z"}`)
	mustFeed(t, session, `{"eventType":"ItemAdded","streamId":"cart-1","sequenceNumber":0,"data":"{}","isJson":true,"eventId":"00000000-0000-0000-0000-000000000000","created":"2026-01-01T00:00:00Z"}`)
	mustFeed(t, session, `{"eventType":"ItemAdded","streamId":"cart-2","sequenceNumber":0,"data":"{}","isJson":true,"eventId":"00000000-0000-0000-0000-000000000000","created":"2026-01-01T00:00:00Z"}`)

	p1 := "cart-1"
	state1 := mustGetState(t, session, &p1)
	if state1 != `{"items":2}` {
		t.Fatalf("cart-1: expected {\"items\":2}, got %s", state1)
	}

	p2 := "cart-2"
	state2 := mustGetState(t, session, &p2)
	if state2 != `{"items":1}` {
		t.Fatalf("cart-2: expected {\"items\":1}, got %s", state2)
	}
}

func TestSetAndRestoreState(t *testing.T) {
	session := mustCreateSession(t, `
		fromAll().when({
			$init: function() { return { count: 0 }; },
			Ping: function(s, e) { s.count++; return s; }
		})
	`)

	session.SetState(nil, `{"count":10}`)
	mustFeed(t, session, `{"eventType":"Ping","streamId":"s-1","sequenceNumber":0,"data":"{}","isJson":true,"eventId":"00000000-0000-0000-0000-000000000000","created":"2026-01-01T00:00:00Z"}`)

	state := mustGetState(t, session, nil)
	if state != `{"count":11}` {
		t.Fatalf("expected {\"count\":11}, got %s", state)
	}
}

func TestFeedError(t *testing.T) {
	session := mustCreateSession(t, `
		fromAll().when({
			$init: function() { return {}; },
			Bad: function(s, e) { throw "boom"; }
		})
	`)

	_, err := session.Feed(`{"eventType":"Bad","streamId":"s-1","sequenceNumber":0,"data":"{}","isJson":true,"eventId":"00000000-0000-0000-0000-000000000000","created":"2026-01-01T00:00:00Z"}`)
	if err == nil {
		t.Fatal("expected error")
	}
	var handlerErr *ProjectionHandlerError
	if !errors.As(err, &handlerErr) {
		t.Fatalf("expected ProjectionHandlerError, got %T", err)
	}
	if handlerErr.Desc != "boom" {
		t.Fatalf("expected description 'boom', got %s", handlerErr.Desc)
	}
	if handlerErr.Event.EventType != "Bad" {
		t.Fatalf("expected eventType 'Bad', got %s", handlerErr.Event.EventType)
	}
}

func TestCreateWithOptions(t *testing.T) {
	opts := `{"compilationTimeoutMs":10000}`
	session, err := NewSession(`
		fromAll().when({
			$init: function() { return {}; },
			Ping: function(s, e) { return s; }
		})
	`, &opts)
	if err != nil {
		t.Fatalf("NewSession with options failed: %v", err)
	}
	session.Destroy()
}

func TestUnknownPartitionReturnsNil(t *testing.T) {
	session := mustCreateSession(t, `
		fromAll().foreachStream().when({
			$init: function() { return {}; },
			Ping: function(s, e) { return s; }
		})
	`)

	p := "nonexistent"
	state := session.GetState(&p)
	if state != nil {
		t.Fatalf("expected nil, got %s", *state)
	}
}

func TestDoubleDestroyIsSafe(t *testing.T) {
	session, err := NewSession(`
		fromAll().when({
			$init: function() { return {}; },
			Ping: function(s, e) { return s; }
		})
	`, nil)
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}
	session.Destroy()
	session.Destroy()
}

func TestOnEmitCallback(t *testing.T) {
	session := mustCreateSession(t, `
		fromAll().when({
			$init: function() { return {}; },
			OrderPlaced: function(s, e) {
				emit("notifications", "OrderNotification", { orderId: e.data.orderId });
				return s;
			}
		})
	`)

	var emitted []struct{ streamID, eventType, data string }
	session.OnEmit(func(streamID, eventType, data, _ string, _, _ bool) {
		emitted = append(emitted, struct{ streamID, eventType, data string }{streamID, eventType, data})
	})

	mustFeed(t, session, `{"eventType":"OrderPlaced","streamId":"order-1","sequenceNumber":0,"data":"{\"orderId\":\"ABC\"}","isJson":true,"eventId":"00000000-0000-0000-0000-000000000000","created":"2026-01-01T00:00:00Z"}`)

	if len(emitted) != 1 {
		t.Fatalf("expected 1 emitted event, got %d", len(emitted))
	}
	if emitted[0].streamID != "notifications" {
		t.Fatalf("expected stream 'notifications', got %s", emitted[0].streamID)
	}
	if emitted[0].eventType != "OrderNotification" {
		t.Fatalf("expected type 'OrderNotification', got %s", emitted[0].eventType)
	}
	if !strings.Contains(emitted[0].data, "ABC") {
		t.Fatalf("expected data to contain 'ABC', got %s", emitted[0].data)
	}
}

func TestOnLogCallback(t *testing.T) {
	session := mustCreateSession(t, `
		fromAll().when({
			TestEvent: function(s, e) {
				log("hello from projection");
				return s;
			}
		})
	`)

	var logs []string
	session.OnLog(func(message string) {
		logs = append(logs, message)
	})

	mustFeed(t, session, `{"eventType":"TestEvent","streamId":"s-1","sequenceNumber":0,"data":"{}","isJson":true,"eventId":"00000000-0000-0000-0000-000000000000","created":"2026-01-01T00:00:00Z"}`)

	if len(logs) != 1 {
		t.Fatalf("expected 1 log, got %d", len(logs))
	}
	if logs[0] != "hello from projection" {
		t.Fatalf("expected 'hello from projection', got %s", logs[0])
	}
}

func TestOnStateChangedCallback(t *testing.T) {
	session := mustCreateSession(t, `
		fromAll().when({
			$init: function() { return { count: 0 }; },
			ItemAdded: function(s, e) { s.count++; return s; }
		})
	`)

	var changes []string
	session.OnStateChanged(func(_ string, stateJSON string) {
		changes = append(changes, stateJSON)
	})

	mustFeed(t, session, `{"eventType":"ItemAdded","streamId":"s-1","sequenceNumber":0,"data":"{}","isJson":true,"eventId":"00000000-0000-0000-0000-000000000000","created":"2026-01-01T00:00:00Z"}`)
	mustFeed(t, session, `{"eventType":"ItemAdded","streamId":"s-1","sequenceNumber":0,"data":"{}","isJson":true,"eventId":"00000000-0000-0000-0000-000000000000","created":"2026-01-01T00:00:00Z"}`)

	if len(changes) != 2 {
		t.Fatalf("expected 2 state changes, got %d", len(changes))
	}
	if !strings.Contains(changes[0], `"count":1`) {
		t.Fatalf("expected count:1, got %s", changes[0])
	}
	if !strings.Contains(changes[1], `"count":2`) {
		t.Fatalf("expected count:2, got %s", changes[1])
	}
}

func TestBiStateSharedState(t *testing.T) {
	session := mustCreateSession(t, `
		options({ biState: true });
		fromAll().when({
			$init: function() { return { count: 0 }; },
			$initShared: function() { return { total: 0 }; },
			Added: function(s, e) {
				s[0].count++;
				s[1].total += e.data.amount;
				return s;
			}
		})
	`)

	mustFeed(t, session, `{"eventType":"Added","streamId":"s-1","sequenceNumber":0,"data":"{\"amount\":10}","isJson":true,"eventId":"00000000-0000-0000-0000-000000000000","created":"2026-01-01T00:00:00Z"}`)
	mustFeed(t, session, `{"eventType":"Added","streamId":"s-1","sequenceNumber":0,"data":"{\"amount\":20}","isJson":true,"eventId":"00000000-0000-0000-0000-000000000000","created":"2026-01-01T00:00:00Z"}`)

	state := mustGetState(t, session, nil)
	if !strings.Contains(state, `"count":2`) {
		t.Fatalf("expected count:2 in state, got %s", state)
	}

	shared := session.GetSharedState()
	if shared == nil {
		t.Fatal("expected shared state")
		return
	}
	if !strings.Contains(*shared, `"total":30`) {
		t.Fatalf("expected total:30 in shared state, got %s", *shared)
	}
}

func TestGetResultWithTransformBy(t *testing.T) {
	session := mustCreateSession(t, `
		fromAll().when({
			$init: function() { return { count: 0 }; },
			Ping: function(s, e) { s.count++; return s; }
		}).transformBy(function(s) {
			return { total: s.count * 2 };
		}).outputState()
	`)

	mustFeed(t, session, `{"eventType":"Ping","streamId":"s-1","sequenceNumber":0,"data":"{}","isJson":true,"eventId":"00000000-0000-0000-0000-000000000000","created":"2026-01-01T00:00:00Z"}`)

	result, err := session.GetResult(nil)
	if err != nil {
		t.Fatalf("GetResult failed: %v", err)
	}
	if result == nil {
		t.Fatal("expected result")
		return
	}
	if !strings.Contains(*result, `"total":2`) {
		t.Fatalf("expected total:2, got %s", *result)
	}
}

func TestGetPartitionKey(t *testing.T) {
	session := mustCreateSession(t, `
		fromAll().partitionBy(function(e) {
			return e.data.region;
		}).when({
			$init: function() { return {}; },
			Event: function(s, e) { return s; }
		})
	`)

	key := session.GetPartitionKey(`{"eventType":"Event","streamId":"s-1","sequenceNumber":0,"data":"{\"region\":\"eu\"}","isJson":true,"eventId":"00000000-0000-0000-0000-000000000000","created":"2026-01-01T00:00:00Z"}`)
	if key == nil {
		t.Fatal("expected partition key")
		return
	}
	if *key != "eu" {
		t.Fatalf("expected 'eu', got %s", *key)
	}
}

func TestFeedResultProcessed(t *testing.T) {
	session := mustCreateSession(t, `
		fromAll().when({
			$init: function() { return { count: 0 }; },
			ItemAdded: function(s, e) { s.count++; return s; }
		})
	`)

	result := mustFeed(t, session, `{"eventType":"ItemAdded","streamId":"cart-1","sequenceNumber":0,"data":"{}","isJson":true,"eventId":"00000000-0000-0000-0000-000000000000","created":"2026-01-01T00:00:00Z"}`)

	if result.Status != "processed" {
		t.Fatalf("expected status 'processed', got %q", result.Status)
	}
	if len(result.State) == 0 || string(result.State) == "null" {
		t.Fatal("expected non-null state")
	}
	var state map[string]interface{}
	if err := json.Unmarshal(result.State, &state); err != nil {
		t.Fatalf("failed to parse state: %v", err)
	}
	if state["count"] != float64(1) {
		t.Fatalf("expected count 1, got %v", state["count"])
	}
}

func TestFeedResultSkipped(t *testing.T) {
	session := mustCreateSession(t, `
		fromAll().when({
			$init: function() { return {}; },
			Handled: function(s, e) { return s; }
		})
	`)

	result := mustFeed(t, session, `{"eventType":"Unhandled","streamId":"s-1","sequenceNumber":0,"data":"{}","isJson":true,"eventId":"00000000-0000-0000-0000-000000000000","created":"2026-01-01T00:00:00Z"}`)

	if result.Status != "skipped" {
		t.Fatalf("expected status 'skipped', got %q", result.Status)
	}
	if result.SkipReason == "" {
		t.Fatal("expected non-empty skip reason")
	}
}

func TestFeedResultEmittedEvents(t *testing.T) {
	session := mustCreateSession(t, `
		fromAll().when({
			$init: function() { return {}; },
			OrderPlaced: function(s, e) {
				emit("notifications", "OrderNotification", { orderId: e.data.orderId });
				return s;
			}
		})
	`)

	result := mustFeed(t, session, `{"eventType":"OrderPlaced","streamId":"order-1","sequenceNumber":0,"data":"{\"orderId\":\"ABC\"}","isJson":true,"eventId":"00000000-0000-0000-0000-000000000000","created":"2026-01-01T00:00:00Z"}`)

	if result.Status != "processed" {
		t.Fatalf("expected status 'processed', got %q", result.Status)
	}
	if len(result.Emitted) != 1 {
		t.Fatalf("expected 1 emitted event, got %d", len(result.Emitted))
	}
	if result.Emitted[0].StreamID != "notifications" {
		t.Fatalf("expected stream 'notifications', got %q", result.Emitted[0].StreamID)
	}
	if result.Emitted[0].EventType != "OrderNotification" {
		t.Fatalf("expected type 'OrderNotification', got %q", result.Emitted[0].EventType)
	}
	if result.Emitted[0].Data == nil || !strings.Contains(*result.Emitted[0].Data, "ABC") {
		t.Fatalf("expected data containing 'ABC', got %v", result.Emitted[0].Data)
	}
}

func TestFeedResultPartition(t *testing.T) {
	session := mustCreateSession(t, `
		fromAll().foreachStream().when({
			$init: function() { return { count: 0 }; },
			Ping: function(s, e) { s.count++; return s; }
		})
	`)

	result := mustFeed(t, session, `{"eventType":"Ping","streamId":"order-42","sequenceNumber":0,"data":"{}","isJson":true,"eventId":"00000000-0000-0000-0000-000000000000","created":"2026-01-01T00:00:00Z"}`)

	if result.Status != "processed" {
		t.Fatalf("expected status 'processed', got %q", result.Status)
	}
	if result.Partition != "order-42" {
		t.Fatalf("expected partition 'order-42', got %q", result.Partition)
	}
}
