using Gaffer.Runtime.Errors;
using Gaffer.Runtime.Events;

namespace Gaffer.Runtime.Tests;

// partitionBy, $init, $initShared and $created run user JS before ProcessEvent. These verify
// a throw / timeout / malformed-data in any of them surfaces as a ProjectionException with
// event context, not a raw engine exception that the FFI reports as generic "unexpected".
public class FeedExceptionWrappingTests {
	private static readonly ProjectionEvent PingEvent = new() {
		EventType = "Ping",
		StreamId = "s-1",
		SequenceNumber = 7,
		Data = "{}",
		IsJson = true,
	};

	private static ProjectionSession Session(string source, TimeSpan? executionTimeout = null) =>
		new(source, new ProjectionSessionOptions {
			EngineVersion = ProjectionVersion.V2,
			ExecutionTimeout = executionTimeout ?? TimeSpan.FromSeconds(5),
		});

	[Fact]
	public void PartitionBy_throw_is_wrapped_with_event_context() {
		using var session = Session("""
			fromAll().partitionBy(function(e) { throw new Error("bad partition"); }).when({
				$init: function() { return {}; },
				Ping: function(s, e) { return s; }
			})
			""");

		var ex = Assert.Throws<ProjectionHandlerException>(() => session.Feed(PingEvent));
		Assert.Equal("bad partition", ex.Description);
		Assert.Equal("Ping", ex.EventType);
		Assert.Equal("s-1", ex.StreamId);
		Assert.Equal(7, ex.SequenceNumber);
	}

	[Fact]
	public void PartitionBy_timeout_is_wrapped_as_execution_timeout() {
		// The runtime disables its time constraint when a debugger is attached, so this
		// timeout-driven test would spin forever under one - skip rather than hang.
		if (System.Diagnostics.Debugger.IsAttached)
			return;

		using var session = Session("""
			fromAll().partitionBy(function(e) { while (true) {} }).when({
				$init: function() { return {}; },
				Ping: function(s, e) { return s; }
			})
			""", executionTimeout: TimeSpan.FromMilliseconds(100));

		var ex = Assert.Throws<ExecutionTimeoutException>(() => session.Feed(PingEvent));
		Assert.Equal("Ping", ex.EventType);
		Assert.Equal("s-1", ex.StreamId);
	}

	[Fact]
	public void PartitionBy_malformed_event_data_is_wrapped_as_malformed() {
		using var session = Session("""
			fromAll().partitionBy(function(e) { return e.body.id; }).when({
				$init: function() { return {}; },
				Ping: function(s, e) { return s; }
			})
			""");

		Assert.Throws<MalformedEventException>(() => session.Feed(new ProjectionEvent {
			EventType = "Ping",
			StreamId = "s-1",
			SequenceNumber = 7,
			Data = "not json",
			IsJson = true,
		}));
	}

	[Fact]
	public void Init_throw_is_wrapped_with_event_context() {
		using var session = Session("""
			fromAll().when({
				$init: function() { throw new Error("init boom"); },
				Ping: function(s, e) { return s; }
			})
			""");

		var ex = Assert.Throws<ProjectionHandlerException>(() => session.Feed(PingEvent));
		Assert.Equal("init boom", ex.Description);
		Assert.Equal("Ping", ex.EventType);
		Assert.Equal(7, ex.SequenceNumber);
	}

	[Fact]
	public void Created_handler_throw_is_wrapped_with_event_context() {
		using var session = Session("""
			fromAll().foreachStream().when({
				$init: function() { return {}; },
				$created: function(s, e) { throw new Error("created boom"); },
				Ping: function(s, e) { return s; }
			})
			""");

		var ex = Assert.Throws<ProjectionHandlerException>(() => session.Feed(PingEvent));
		Assert.Equal("created boom", ex.Description);
		Assert.Equal("Ping", ex.EventType);
		Assert.Equal("s-1", ex.Partition);
	}

	[Fact]
	public void Init_shared_throw_is_wrapped_with_event_context() {
		using var session = Session("""
			options({ biState: true });
			fromAll().when({
				$init: function() { return {}; },
				$initShared: function() { throw new Error("shared boom"); },
				Ping: function(s, e) { return s; }
			})
			""");

		var ex = Assert.Throws<ProjectionHandlerException>(() => session.Feed(PingEvent));
		Assert.Equal("shared boom", ex.Description);
		Assert.Equal("Ping", ex.EventType);
		Assert.Equal(7, ex.SequenceNumber);
	}

	[Fact]
	public void Deleted_path_throw_is_wrapped_with_event_context() {
		using var session = Session("""
			fromAll().foreachStream().when({
				$init: function() { return {}; },
				$deleted: function(s, e) { throw new Error("deleted boom"); },
				Ping: function(s, e) { return s; }
			}).outputState()
			""");

		session.Feed(PingEvent);

		var ex = Assert.Throws<ProjectionHandlerException>(() => session.Feed(new ProjectionEvent {
			EventType = ProjectionSession.StreamDeletedEventType,
			StreamId = "s-1",
			SequenceNumber = 8,
			Data = "{}",
		}));
		Assert.Equal("deleted boom", ex.Description);
		Assert.Equal("s-1", ex.Partition);
	}

	[Fact]
	public void GetPartitionKey_throw_is_wrapped_with_event_context() {
		using var session = Session("""
			fromAll().partitionBy(function(e) { throw new Error("bad partition"); }).when({
				$init: function() { return {}; },
				Ping: function(s, e) { return s; }
			})
			""");

		var ex = Assert.Throws<ProjectionHandlerException>(() => session.GetPartitionKey(PingEvent));
		Assert.Equal("bad partition", ex.Description);
		Assert.Equal("Ping", ex.EventType);
		Assert.Equal("s-1", ex.StreamId);
	}
}
