using Gaffer.Runtime.Events;

namespace Gaffer.Runtime.Tests;

public class AlternativeSyntaxTests {
	[Fact]
	public void FromCategories_array_syntax() {
		using var session = new ProjectionSession("""
            fromCategory(["orders", "invoices"]).when({
                $init: function() { return {}; },
                TestEvent: function(s, e) { return s; }
            })
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		Assert.NotNull(session.Sources.Streams);
		Assert.Contains("$ce-orders", session.Sources.Streams);
		Assert.Contains("$ce-invoices", session.Sources.Streams);
	}

	[Fact]
	public void FromCategories_varargs_syntax() {
		using var session = new ProjectionSession("""
            fromCategory("orders", "invoices").when({
                $init: function() { return {}; },
                TestEvent: function(s, e) { return s; }
            })
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		Assert.NotNull(session.Sources.Streams);
		Assert.Contains("$ce-orders", session.Sources.Streams);
		Assert.Contains("$ce-invoices", session.Sources.Streams);
	}

	[Fact]
	public void FromStreams_varargs_syntax() {
		using var session = new ProjectionSession("""
            fromStreams("stream-a", "stream-b", "stream-c").when({
                $init: function() { return {}; },
                TestEvent: function(s, e) { return s; }
            })
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		Assert.NotNull(session.Sources.Streams);
		Assert.Contains("stream-a", session.Sources.Streams);
		Assert.Contains("stream-b", session.Sources.Streams);
		Assert.Contains("stream-c", session.Sources.Streams);
	}

	[Fact]
	public void On_event_alternative_registration() {
		using var session = new ProjectionSession("""
            fromAll();
            on_event("Ping", function(s, e) { s.count = (s.count || 0) + 1; return s; });
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		session.Feed(new ProjectionEvent { EventType = "Ping", StreamId = "s-1", Data = "{}" });
		session.Feed(new ProjectionEvent { EventType = "Ping", StreamId = "s-1", Data = "{}" });

		var state = session.GetState();
		Assert.NotNull(state);
		Assert.Contains("\"count\":2", state);
	}

	[Fact]
	public void On_any_alternative_registration() {
		using var session = new ProjectionSession("""
            fromAll();
            on_any(function(s, e) { s.count = (s.count || 0) + 1; return s; });
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		session.Feed(new ProjectionEvent { EventType = "Foo", StreamId = "s-1", Data = "{}" });
		session.Feed(new ProjectionEvent { EventType = "Bar", StreamId = "s-1", Data = "{}" });

		Assert.Contains("\"count\":2", session.GetState()!);
	}

	[Fact]
	public void Chained_transforms() {
		using var session = new ProjectionSession("""
            fromAll().when({
                $init: function() { return { count: 0, extra: "keep" }; },
                Ping: function(s, e) { s.count++; return s; }
            }).transformBy(function(s) {
                return { total: s.count, extra: s.extra };
            }).transformBy(function(s) {
                return { total: s.total };
            }).outputState()
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		session.Feed(new ProjectionEvent { EventType = "Ping", StreamId = "s-1", Data = "{}" });
		session.Feed(new ProjectionEvent { EventType = "Ping", StreamId = "s-1", Data = "{}" });

		var result = session.GetResult();
		Assert.NotNull(result);
		Assert.Contains("\"total\":2", result);
		Assert.DoesNotContain("extra", result);
	}

	[Fact]
	public void FilterBy_excludes_results() {
		using var session = new ProjectionSession("""
            fromAll().foreachStream().when({
                $init: function() { return { count: 0 }; },
                Ping: function(s, e) { s.count++; return s; }
            }).filterBy(function(s) {
                return s.count > 2;
            }).outputState()
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		session.Feed(new ProjectionEvent { EventType = "Ping", StreamId = "low", Data = "{}" });
		session.Feed(new ProjectionEvent { EventType = "Ping", StreamId = "high", Data = "{}" });
		session.Feed(new ProjectionEvent { EventType = "Ping", StreamId = "high", Data = "{}" });
		session.Feed(new ProjectionEvent { EventType = "Ping", StreamId = "high", Data = "{}" });

		// "low" has count=1, filtered out
		Assert.Null(session.GetResult("low"));

		// "high" has count=3, passes filter
		var result = session.GetResult("high");
		Assert.NotNull(result);
		Assert.Contains("\"count\":3", result);
	}

	[Fact]
	public void TransformBy_then_filterBy() {
		using var session = new ProjectionSession("""
            fromAll().when({
                $init: function() { return { count: 0 }; },
                Ping: function(s, e) { s.count++; return s; }
            }).transformBy(function(s) {
                return { total: s.count, active: s.count > 0 };
            }).filterBy(function(s) {
                return s.active;
            }).outputState()
        """, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		// No events - count is 0, active is false, should be filtered
		Assert.Null(session.GetResult());

		session.Feed(new ProjectionEvent { EventType = "Ping", StreamId = "s-1", Data = "{}" });

		var result = session.GetResult();
		Assert.NotNull(result);
		Assert.Contains("\"total\":1", result);
	}
}
