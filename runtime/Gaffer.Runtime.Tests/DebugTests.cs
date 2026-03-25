using Gaffer.Runtime.Events;

namespace Gaffer.Runtime.Tests;

public class DebugTests {
	private static ProjectionEvent MakeEvent(string type = "ItemAdded", string data = "{}") =>
		new() { EventType = type, StreamId = "stream-1", Data = data };

	[Fact]
	public void Debug_mode_does_not_affect_normal_execution() {
		var source = "fromAll().when({\n$init: function() { return { count: 0 }; },\nItemAdded: function(s, e) { s.count++; return s; }\n})";
		using var session = new ProjectionSession(source, new ProjectionSessionOptions { Debug = true });

		session.Feed(MakeEvent());
		session.Feed(MakeEvent());

		Assert.Contains("\"count\":2", session.GetState()!);
	}

	[Fact]
	public void Breakpoint_pauses_execution() {
		var source = "fromAll().when({\n$init: function() { return { count: 0 }; },\nItemAdded: function(s, e) {\ns.count++;\nreturn s;\n}\n})";
		using var session = new ProjectionSession(source, new ProjectionSessionOptions { Debug = true });

		BreakInfo? breakInfo = null;
		session.OnBreak = info => breakInfo = info;
		session.SetBreakpoint(4); // s.count++; is line 4

		var feedDone = new ManualResetEventSlim(false);
		FeedResult? result = null;

		var feedThread = new Thread(() => {
			result = session.Feed(MakeEvent());
			feedDone.Set();
		});
		feedThread.Start();

		SpinWait.SpinUntil(() => breakInfo != null, TimeSpan.FromSeconds(5));
		Assert.NotNull(breakInfo);
		Assert.Equal("breakpoint", breakInfo.Reason);
		Assert.Equal(4, breakInfo.Line);
		Assert.True(session.IsPaused);
		Assert.False(feedDone.IsSet);

		session.Continue();
		Assert.True(feedDone.Wait(TimeSpan.FromSeconds(5)));

		Assert.NotNull(result);
		Assert.False(session.IsPaused);
		Assert.Contains("\"count\":1", session.GetState()!);
	}

	[Fact]
	public void Debugger_statement_pauses_execution() {
		var source = "fromAll().when({\n$init: function() { return { count: 0 }; },\nItemAdded: function(s, e) {\ndebugger;\ns.count++;\nreturn s;\n}\n})";
		using var session = new ProjectionSession(source, new ProjectionSessionOptions { Debug = true });

		BreakInfo? breakInfo = null;
		session.OnBreak = info => breakInfo = info;

		var feedDone = new ManualResetEventSlim(false);
		var feedThread = new Thread(() => {
			session.Feed(MakeEvent());
			feedDone.Set();
		});
		feedThread.Start();

		SpinWait.SpinUntil(() => breakInfo != null, TimeSpan.FromSeconds(5));
		Assert.NotNull(breakInfo);
		Assert.Equal("debugger_statement", breakInfo.Reason);

		session.Continue();
		Assert.True(feedDone.Wait(TimeSpan.FromSeconds(5)));
		Assert.Contains("\"count\":1", session.GetState()!);
	}

	[Fact]
	public void Clear_breakpoints_removes_all() {
		var source = "fromAll().when({\n$init: function() { return { count: 0 }; },\nItemAdded: function(s, e) {\ns.count++;\nreturn s;\n}\n})";
		using var session = new ProjectionSession(source, new ProjectionSessionOptions { Debug = true });

		session.SetBreakpoint(4);
		session.ClearBreakpoints();

		session.Feed(MakeEvent());
		Assert.Contains("\"count\":1", session.GetState()!);
	}

	[Fact]
	public void Column_values_are_one_based() {
		var source = "fromAll().when({\n$init: function() { return { count: 0 }; },\nItemAdded: function(s, e) {\ns.count++;\nreturn s;\n}\n})";
		using var session = new ProjectionSession(source, new ProjectionSessionOptions { Debug = true });

		BreakInfo? breakInfo = null;
		session.OnBreak = info => breakInfo = info;
		session.SetBreakpoint(4); // s.count++;

		var feedDone = new ManualResetEventSlim(false);
		var feedThread = new Thread(() => {
			session.Feed(MakeEvent());
			feedDone.Set();
		});
		feedThread.Start();

		SpinWait.SpinUntil(() => breakInfo != null, TimeSpan.FromSeconds(5));
		Assert.NotNull(breakInfo);
		Assert.True(breakInfo.Column >= 1, $"Column should be 1-based, got {breakInfo.Column}");

		session.Continue();
		Assert.True(feedDone.Wait(TimeSpan.FromSeconds(5)));
	}

	[Fact]
	public void Multiple_breakpoints_pause_multiple_times() {
		var source = "fromAll().when({\n$init: function() { return { count: 0 }; },\nItemAdded: function(s, e) {\ns.count++;\nreturn s;\n}\n})";
		using var session = new ProjectionSession(source, new ProjectionSessionOptions { Debug = true });

		var breaks = new List<BreakInfo>();
		session.OnBreak = info => breaks.Add(info);
		session.SetBreakpoint(4);

		var feedDone = new ManualResetEventSlim(false);
		var feedThread = new Thread(() => {
			session.Feed(MakeEvent());
			feedDone.Set();
		});
		feedThread.Start();

		SpinWait.SpinUntil(() => breaks.Count >= 1, TimeSpan.FromSeconds(5));
		session.Continue();
		Assert.True(feedDone.Wait(TimeSpan.FromSeconds(5)));

		feedDone.Reset();
		feedThread = new Thread(() => {
			session.Feed(MakeEvent());
			feedDone.Set();
		});
		feedThread.Start();

		SpinWait.SpinUntil(() => breaks.Count >= 2, TimeSpan.FromSeconds(5));
		session.Continue();
		Assert.True(feedDone.Wait(TimeSpan.FromSeconds(5)));

		Assert.Equal(2, breaks.Count);
		Assert.Contains("\"count\":2", session.GetState()!);
	}

	[Fact]
	public void No_debug_mode_ignores_debugger_statement() {
		var source = "fromAll().when({\n$init: function() { return { count: 0 }; },\nItemAdded: function(s, e) {\ndebugger;\ns.count++;\nreturn s;\n}\n})";
		using var session = new ProjectionSession(source);

		session.Feed(MakeEvent());
		Assert.Contains("\"count\":1", session.GetState()!);
	}
}
