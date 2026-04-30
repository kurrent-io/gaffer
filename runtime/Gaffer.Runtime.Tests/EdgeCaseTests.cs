using Gaffer.Runtime.Errors;
using Gaffer.Runtime.Events;

namespace Gaffer.Runtime.Tests;

public class EdgeCaseTests {
	[Fact]
	public void Null_data_event_is_handled() {
		using var session = new ProjectionSession("""
            fromAll().when({
                $init: function() { return { count: 0 }; },
                Ping: function(s, e) { s.count++; return s; }
            })
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		session.Feed(new ProjectionEvent { EventType = "Ping", StreamId = "s-1", Data = null });

		// Null data should be skipped (handler returns state unchanged)
		var state = session.GetState();
		Assert.NotNull(state);
	}

	[Fact]
	public void Empty_data_event_is_handled() {
		using var session = new ProjectionSession("""
            fromAll().when({
                $init: function() { return { count: 0 }; },
                Ping: function(s, e) { s.count++; return s; }
            })
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		session.Feed(new ProjectionEvent { EventType = "Ping", StreamId = "s-1", Data = "" });

		var state = session.GetState();
		Assert.NotNull(state);
	}

	[Fact]
	public void Whitespace_data_event_is_handled() {
		using var session = new ProjectionSession("""
            fromAll().when({
                $init: function() { return { count: 0 }; },
                Ping: function(s, e) { s.count++; return s; }
            })
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		session.Feed(new ProjectionEvent { EventType = "Ping", StreamId = "s-1", Data = "   " });

		var state = session.GetState();
		Assert.NotNull(state);
	}

	[Fact]
	public void Non_json_event_with_data() {
		using var session = new ProjectionSession("""
            fromAll().when({
                $init: function() { return { count: 0 }; },
                $any: function(s, e) { s.count++; return s; }
            })
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		session.Feed(new ProjectionEvent {
			EventType = "BinaryEvent",
			StreamId = "s-1",
			Data = "some binary data",
			IsJson = false,
		});

		Assert.Contains("\"count\":1", session.GetState()!);
	}

	[Fact]
	public void Handler_returning_undefined_preserves_state() {
		using var session = new ProjectionSession("""
            fromAll().when({
                $init: function() { return { count: 0 }; },
                Increment: function(s, e) { s.count++; },
                Noop: function(s, e) { }
            })
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		session.Feed(new ProjectionEvent { EventType = "Increment", StreamId = "s-1", Data = "{}" });
		session.Feed(new ProjectionEvent { EventType = "Noop", StreamId = "s-1", Data = "{}" });
		session.Feed(new ProjectionEvent { EventType = "Increment", StreamId = "s-1", Data = "{}" });

		// Handlers that don't return preserve the state (Jint returns undefined -> keep old state)
		Assert.Contains("\"count\":2", session.GetState()!);
	}

	[Fact]
	public void Multiple_partitions_independent() {
		using var session = new ProjectionSession("""
            fromAll().foreachStream().when({
                $init: function() { return { count: 0 }; },
                Ping: function(s, e) { s.count++; return s; }
            })
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		session.Feed(new ProjectionEvent { EventType = "Ping", StreamId = "a", Data = "{}" });
		session.Feed(new ProjectionEvent { EventType = "Ping", StreamId = "b", Data = "{}" });
		session.Feed(new ProjectionEvent { EventType = "Ping", StreamId = "a", Data = "{}" });
		session.Feed(new ProjectionEvent { EventType = "Ping", StreamId = "a", Data = "{}" });
		session.Feed(new ProjectionEvent { EventType = "Ping", StreamId = "b", Data = "{}" });

		Assert.Contains("\"count\":3", session.GetState("a")!);
		Assert.Contains("\"count\":2", session.GetState("b")!);
	}

	[Fact]
	public void Compilation_timeout_throws() {
		Assert.Throws<CompilationTimeoutException>(() => new ProjectionSession("""
            while(true) {}
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2, CompilationTimeout = TimeSpan.FromMilliseconds(100) }));
	}

	[Fact]
	public void Event_metadata_accessible() {
		using var session = new ProjectionSession("""
            fromAll().when({
                $init: function() { return { user: "" }; },
                TestEvent: function(s, e) {
                    s.user = e.metadata.userId;
                    return s;
                }
            })
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		session.Feed(new ProjectionEvent {
			EventType = "TestEvent",
			StreamId = "s-1",
			Data = "{}",
			Metadata = """{"userId":"alice"}""",
		});

		Assert.Contains("\"user\":\"alice\"", session.GetState()!);
	}

	[Fact]
	public void Event_properties_accessible() {
		using var session = new ProjectionSession("""
            fromAll().when({
                $init: function() { return {}; },
                TestEvent: function(s, e) {
                    s.stream = e.streamId;
                    s.type = e.eventType;
                    s.seq = e.sequenceNumber;
                    s.json = e.isJson;
                    return s;
                }
            })
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		session.Feed(new ProjectionEvent {
			EventType = "TestEvent",
			StreamId = "my-stream",
			Data = "{}",
			SequenceNumber = 42,
		});

		var state = session.GetState()!;
		Assert.Contains("\"stream\":\"my-stream\"", state);
		Assert.Contains("\"type\":\"TestEvent\"", state);
		Assert.Contains("\"seq\":42", state);
		Assert.Contains("\"json\":true", state);
	}

	[Fact]
	public void GetState_returns_null_for_unknown_partition() {
		using var session = new ProjectionSession("""
            fromAll().foreachStream().when({
                $init: function() { return {}; },
                Ping: function(s, e) { return s; }
            })
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		Assert.Null(session.GetState("nonexistent"));
	}

	[Fact]
	public void Invalid_js_throws() {
		Assert.ThrowsAny<Exception>(() => new ProjectionSession("this is not valid javascript {{{{", new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 }));
	}
}
