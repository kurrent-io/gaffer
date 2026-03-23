using Gaffer.Runtime.Events;

namespace Gaffer.Runtime.Tests;

public class DeletedStreamTests {
	private static ProjectionEvent DeleteEvent(string streamId) => new() {
		EventType = ProjectionSession.StreamDeletedEventType,
		StreamId = streamId,
		Data = "{}",
	};

	[Fact]
	public void Deleted_handler_modifies_state() {
		using var session = new ProjectionSession("""
            fromAll().foreachStream().when({
                $init: function() { return { a: 0 }; },
                type1: function(s, e) { s.a++; return s; },
                $deleted: function(s, e) { s.deleted = 1; return s; }
            }).outputState()
        """);

		session.Feed(new ProjectionEvent { EventType = "type1", StreamId = "stream-1", Data = "{}" });
		session.Feed(new ProjectionEvent { EventType = "type1", StreamId = "stream-1", Data = "{}" });

		Assert.Contains("\"a\":2", session.GetState("stream-1")!);

		session.Feed(DeleteEvent("stream-1"));

		var state = session.GetState("stream-1");
		Assert.NotNull(state);
		Assert.Contains("\"deleted\":1", state);
	}

	[Fact]
	public void Deleted_handler_on_uninitialized_partition() {
		using var session = new ProjectionSession("""
            fromAll().foreachStream().when({
                $init: function() { return {}; },
                type1: function(s, e) { s.a = 1; return s; },
                $deleted: function(s, e) { s.deleted = 1; return s; }
            }).outputState()
        """);

		session.Feed(DeleteEvent("never-seen"));

		var state = session.GetState("never-seen");
		Assert.NotNull(state);
		Assert.Contains("\"deleted\":1", state);
	}

	[Fact]
	public void Deleted_event_ignored_when_no_handler() {
		using var session = new ProjectionSession("""
            fromAll().foreachStream().when({
                $init: function() { return { a: 0 }; },
                type1: function(s, e) { s.a++; return s; }
            }).outputState()
        """);

		session.Feed(new ProjectionEvent { EventType = "type1", StreamId = "stream-1", Data = "{}" });
		session.Feed(DeleteEvent("stream-1"));

		// State should be unchanged
		Assert.Contains("\"a\":1", session.GetState("stream-1")!);
	}

	[Fact]
	public void Soft_delete_via_metadata_event() {
		using var session = new ProjectionSession("""
            fromAll().foreachStream().when({
                $init: function() { return { a: 0 }; },
                type1: function(s, e) { s.a++; return s; },
                $deleted: function(s, e) { s.deleted = 1; return s; }
            }).outputState()
        """);

		session.Feed(new ProjectionEvent { EventType = "type1", StreamId = "stream-1", Data = "{}" });

		// Soft delete: $metadata on metastream with $tb = long.MaxValue
		session.Feed(new ProjectionEvent {
			EventType = "$metadata",
			StreamId = "$$stream-1",
			Data = """{"$tb":9223372036854775807}""",
		});

		var state = session.GetState("stream-1");
		Assert.NotNull(state);
		Assert.Contains("\"deleted\":1", state);
	}

	[Fact]
	public void Metadata_event_without_truncate_is_not_delete() {
		using var session = new ProjectionSession("""
            fromAll().foreachStream().when({
                $init: function() { return { a: 0 }; },
                type1: function(s, e) { s.a++; return s; },
                $deleted: function(s, e) { s.deleted = 1; return s; }
            }).outputState()
        """);

		session.Feed(new ProjectionEvent { EventType = "type1", StreamId = "stream-1", Data = "{}" });

		// Regular metadata update, not a delete
		session.Feed(new ProjectionEvent {
			EventType = "$metadata",
			StreamId = "$$stream-1",
			Data = """{"$maxAge":3600}""",
		});

		var state = session.GetState("stream-1");
		Assert.NotNull(state);
		Assert.DoesNotContain("deleted", state);
	}

	[Fact]
	public void Deleted_not_allowed_in_bistate() {
		Assert.Throws<Exception>(() => new ProjectionSession("""
            options({ biState: true });
            fromAll().when({
                $init: function() { return {}; },
                $initShared: function() { return {}; },
                $deleted: function(s, e) { return s; }
            })
        """));
	}

	[Fact]
	public void Source_definition_reflects_deleted_handler() {
		using var session = new ProjectionSession("""
            fromAll().foreachStream().when({
                $init: function() { return {}; },
                $deleted: function(s, e) { s.deleted = 1; return s; }
            }).outputState()
        """);

		Assert.True(session.Sources.HandlesDeletedNotifications);
	}

	[Fact]
	public void Source_definition_no_deleted_handler() {
		using var session = new ProjectionSession("""
            fromAll().foreachStream().when({
                $init: function() { return {}; },
                type1: function(s, e) { return s; }
            }).outputState()
        """);

		Assert.False(session.Sources.HandlesDeletedNotifications);
	}
}
