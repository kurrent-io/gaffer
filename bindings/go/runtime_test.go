package gafferruntime

import (
	"strings"
	"testing"
)

func mustCreateSession(t *testing.T, source string) *Session {
	t.Helper()
	session := SessionCreate(source, nil)
	if session == nil {
		t.Fatal("SessionCreate returned nil")
	}
	t.Cleanup(func() { SessionDestroy(session) })
	return session
}

func mustFeed(t *testing.T, session *Session, eventJSON string) {
	t.Helper()
	result := SessionFeed(session, eventJSON)
	if result != 0 {
		errMsg := SessionGetError(session)
		if errMsg != nil {
			t.Fatalf("SessionFeed failed: %s", *errMsg)
		}
		t.Fatal("SessionFeed failed with unknown error")
	}
}

func mustGetState(t *testing.T, session *Session, partition *string) string {
	t.Helper()
	state := SessionGetState(session, partition)
	if state == nil {
		t.Fatal("SessionGetState returned nil")
		return "" // unreachable, satisfies staticcheck
	}
	return *state
}

func TestSessionCreateAndDestroy(t *testing.T) {
	session := SessionCreate(`
		fromAll().when({
			$init: function() { return {}; },
			Ping: function(s, e) { return s; }
		})
	`, nil)
	if session == nil {
		t.Fatal("SessionCreate returned nil")
	}
	SessionDestroy(session)
}

func TestCreateWithInvalidJS(t *testing.T) {
	session := SessionCreate("this is not valid {{{{", nil)
	if session != nil {
		SessionDestroy(session)
		t.Fatal("expected nil for invalid JS")
	}
}

func TestFeedAndGetState(t *testing.T) {
	session := mustCreateSession(t, `
		fromAll().when({
			$init: function() { return { count: 0 }; },
			ItemAdded: function(s, e) { s.count++; return s; }
		})
	`)

	mustFeed(t, session, `{"eventType":"ItemAdded","streamId":"cart-1","data":"{}"}`)
	mustFeed(t, session, `{"eventType":"ItemAdded","streamId":"cart-1","data":"{}"}`)
	mustFeed(t, session, `{"eventType":"ItemAdded","streamId":"cart-1","data":"{}"}`)

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

	mustFeed(t, session, `{"eventType":"Deposited","streamId":"acc-1","data":"{\"amount\":50}"}`)
	mustFeed(t, session, `{"eventType":"Deposited","streamId":"acc-1","data":"{\"amount\":30}"}`)

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

	sources := SessionGetSources(session)
	if sources == nil {
		t.Fatal("SessionGetSources returned nil")
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

	mustFeed(t, session, `{"eventType":"ItemAdded","streamId":"cart-1","data":"{}"}`)
	mustFeed(t, session, `{"eventType":"ItemAdded","streamId":"cart-1","data":"{}"}`)
	mustFeed(t, session, `{"eventType":"ItemAdded","streamId":"cart-2","data":"{}"}`)

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

	SessionSetState(session, nil, `{"count":10}`)
	mustFeed(t, session, `{"eventType":"Ping","streamId":"s-1","data":"{}"}`)

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

	result := SessionFeed(session, `{"eventType":"Bad","streamId":"s-1","data":"{}"}`)
	if result == 0 {
		t.Fatal("expected non-zero for JS error")
	}

	errMsg := SessionGetError(session)
	if errMsg == nil {
		t.Fatal("expected error message")
		return
	}
	if !strings.Contains(*errMsg, "boom") {
		t.Fatalf("expected error to contain 'boom', got: %s", *errMsg)
	}
}

func TestCreateWithOptions(t *testing.T) {
	opts := `{"handlerTimeoutMs":100,"compilationTimeoutMs":10000}`
	session := SessionCreate(`
		fromAll().when({
			$init: function() { return {}; },
			Ping: function(s, e) { return s; }
		})
	`, &opts)
	if session == nil {
		t.Fatal("SessionCreate with options returned nil")
	}
	SessionDestroy(session)
}

func TestUnknownPartitionReturnsNil(t *testing.T) {
	session := mustCreateSession(t, `
		fromAll().foreachStream().when({
			$init: function() { return {}; },
			Ping: function(s, e) { return s; }
		})
	`)

	p := "nonexistent"
	state := SessionGetState(session, &p)
	if state != nil {
		t.Fatalf("expected nil, got %s", *state)
	}
}

func TestDoubleDestroyIsSafe(t *testing.T) {
	session := SessionCreate(`
		fromAll().when({
			$init: function() { return {}; },
			Ping: function(s, e) { return s; }
		})
	`, nil)
	if session == nil {
		t.Fatal("SessionCreate returned nil")
	}
	SessionDestroy(session)
	SessionDestroy(session) // second destroy should not panic
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
	SessionOnEmit(session, func(streamID, eventType, data, _ string) {
		emitted = append(emitted, struct{ streamID, eventType, data string }{streamID, eventType, data})
	})

	mustFeed(t, session, `{"eventType":"OrderPlaced","streamId":"order-1","data":"{\"orderId\":\"ABC\"}"}`)

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
	SessionOnLog(session, func(message string) {
		logs = append(logs, message)
	})

	mustFeed(t, session, `{"eventType":"TestEvent","streamId":"s-1","data":"{}"}`)

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
			Ping: function(s, e) { s.count++; return s; }
		})
	`)

	var changes []string
	SessionOnStateChanged(session, func(_ string, stateJSON string) {
		changes = append(changes, stateJSON)
	})

	mustFeed(t, session, `{"eventType":"Ping","streamId":"s-1","data":"{}"}`)
	mustFeed(t, session, `{"eventType":"Ping","streamId":"s-1","data":"{}"}`)

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

func TestOnSlowHandlerCallback(t *testing.T) {
	opts := `{"handlerTimeoutMs":1}`
	session := SessionCreate(`
		fromAll().when({
			$init: function() { return {}; },
			Slow: function(s, e) {
				var start = Date.now();
				while (Date.now() - start < 10) {}
				return s;
			}
		})
	`, &opts)
	if session == nil {
		t.Fatal("SessionCreate returned nil")
	}
	defer SessionDestroy(session)

	var warnings []struct {
		handler string
		ms      int
	}
	SessionOnSlowHandler(session, func(handler string, ms int) {
		warnings = append(warnings, struct {
			handler string
			ms      int
		}{handler, ms})
	})

	mustFeed(t, session, `{"eventType":"Slow","streamId":"s-1","data":"{}"}`)

	if len(warnings) != 1 {
		t.Fatalf("expected 1 slow handler warning, got %d", len(warnings))
	}
	if warnings[0].handler != "Slow" {
		t.Fatalf("expected handler 'Slow', got %s", warnings[0].handler)
	}
	if warnings[0].ms < 1 {
		t.Fatalf("expected duration >= 1ms, got %d", warnings[0].ms)
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

	mustFeed(t, session, `{"eventType":"Added","streamId":"s-1","data":"{\"amount\":10}"}`)
	mustFeed(t, session, `{"eventType":"Added","streamId":"s-1","data":"{\"amount\":20}"}`)

	state := mustGetState(t, session, nil)
	if !strings.Contains(state, `"count":2`) {
		t.Fatalf("expected count:2 in state, got %s", state)
	}

	shared := SessionGetSharedState(session)
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

	mustFeed(t, session, `{"eventType":"Ping","streamId":"s-1","data":"{}"}`)

	result := SessionGetResult(session, nil)
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

	key := SessionGetPartitionKey(session, `{"eventType":"Event","streamId":"s-1","data":"{\"region\":\"eu\"}"}`)
	if key == nil {
		t.Fatal("expected partition key")
		return
	}
	if *key != "eu" {
		t.Fatalf("expected 'eu', got %s", *key)
	}
}
