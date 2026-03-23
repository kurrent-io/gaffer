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
