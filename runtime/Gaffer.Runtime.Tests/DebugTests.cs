using Gaffer.Runtime.Events;

namespace Gaffer.Runtime.Tests;

public class DebugTests {
	private static ProjectionEvent MakeEvent(string type = "ItemAdded", string data = "{}") =>
		new() { EventType = type, StreamId = "stream-1", Data = data };

	[Fact]
	public void Debug_mode_does_not_affect_normal_execution() {
		var source = "fromAll().when({\n$init: function() { return { count: 0 }; },\nItemAdded: function(s, e) { s.count++; return s; }\n})";
		using var session = new ProjectionSession(source, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2, Debug = true });

		session.Feed(MakeEvent());
		session.Feed(MakeEvent());

		Assert.Contains("\"count\":2", session.GetState()!);
	}

	[Fact]
	public void Breakpoint_pauses_execution() {
		var source = "fromAll().when({\n$init: function() { return { count: 0 }; },\nItemAdded: function(s, e) {\ns.count++;\nreturn s;\n}\n})";
		using var session = new ProjectionSession(source, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2, Debug = true });

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
		using var session = new ProjectionSession(source, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2, Debug = true });

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
		using var session = new ProjectionSession(source, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2, Debug = true });

		session.SetBreakpoint(4);
		session.ClearBreakpoints();

		session.Feed(MakeEvent());
		Assert.Contains("\"count\":1", session.GetState()!);
	}

	[Fact]
	public void Column_values_are_one_based() {
		var source = "fromAll().when({\n$init: function() { return { count: 0 }; },\nItemAdded: function(s, e) {\ns.count++;\nreturn s;\n}\n})";
		using var session = new ProjectionSession(source, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2, Debug = true });

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
		using var session = new ProjectionSession(source, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2, Debug = true });

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
	public void Breakpoint_snaps_to_nearest_statement() {
		// Line 1: fromAll()
		// Line 2: .partitionBy(function(event) {  <-- not a breakable statement
		// Line 3:   return event.eventType;        <-- breakable (return statement)
		// Line 4: })
		// Line 5: .when({
		// Line 6:   $init: function() {
		// Line 7:     return { count: 0 };          <-- breakable
		var source = "fromAll()\n.partitionBy(function(event) {\n  return event.eventType;\n})\n.when({\n  $init: function() {\n    return { count: 0 };\n  },\n  ItemAdded: function(s, e) {\n    s.count++;\n    return s;\n  }\n})";
		using var session = new ProjectionSession(source, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2, Debug = true });

		// Breakpoint on line 2 (not breakable) should snap to line 3 (return statement)
		var snapped = session.SetBreakpoint(2);
		Assert.NotNull(snapped);
		Assert.Equal(3, snapped.Value.Line);
	}

	[Fact]
	public void Breakpoint_on_exact_statement_stays() {
		var source = "fromAll().when({\n$init: function() { return { count: 0 }; },\nItemAdded: function(s, e) {\ns.count++;\nreturn s;\n}\n})";
		using var session = new ProjectionSession(source, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2, Debug = true });

		// Line 4 is s.count++ - an exact breakable position
		var snapped = session.SetBreakpoint(4);
		Assert.NotNull(snapped);
		Assert.Equal(4, snapped.Value.Line);
	}

	[Fact]
	public void Breakpoint_past_end_returns_null() {
		var source = "fromAll().when({\n$init: function() { return { count: 0 }; },\nItemAdded: function(s, e) {\ns.count++;\nreturn s;\n}\n})";
		using var session = new ProjectionSession(source, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2, Debug = true });

		var snapped = session.SetBreakpoint(999);
		Assert.Null(snapped);
	}

	[Fact]
	public void Breakpoint_column_snapping() {
		// Two statements on different columns of the same concept:
		// Line 3: "  s.count++;" - statement starts at column 3 (1-based)
		var source = "fromAll().when({\n$init: function() { return { count: 0 }; },\nItemAdded: function(s, e) {\n  s.count++;\n  return s;\n}\n})";
		using var session = new ProjectionSession(source, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2, Debug = true });

		// Column 1 on line 4 should snap to the statement at its actual column
		var snapped = session.SetBreakpoint(4, 1);
		Assert.NotNull(snapped);
		Assert.Equal(4, snapped.Value.Line);
		Assert.True(snapped.Value.Column >= 1);
	}

	[Fact]
	public void Step_over_advances_to_next_statement() {
		// Line 1: fromAll().when({
		// Line 2: $init: function() { return { count: 0 }; },
		// Line 3: ItemAdded: function(s, e) {
		// Line 4: s.count++;
		// Line 5: return s;
		// Line 6: }
		// Line 7: })
		var source = "fromAll().when({\n$init: function() { return { count: 0 }; },\nItemAdded: function(s, e) {\ns.count++;\nreturn s;\n}\n})";
		using var session = new ProjectionSession(source, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2, Debug = true });

		var breaks = new List<BreakInfo>();
		session.OnBreak = info => breaks.Add(info);
		session.SetBreakpoint(4); // s.count++;

		var feedDone = new ManualResetEventSlim(false);
		var feedThread = new Thread(() => {
			session.Feed(MakeEvent());
			feedDone.Set();
		});
		feedThread.Start();

		SpinWait.SpinUntil(() => breaks.Count >= 1, TimeSpan.FromSeconds(5));
		Assert.Equal(4, breaks[0].Line);

		// Step over should advance to line 5 (return s;)
		session.StepOver();
		SpinWait.SpinUntil(() => breaks.Count >= 2, TimeSpan.FromSeconds(5));
		Assert.Equal("step", breaks[1].Reason);
		Assert.Equal(5, breaks[1].Line);

		session.Continue();
		Assert.True(feedDone.Wait(TimeSpan.FromSeconds(5)));
	}

	[Fact]
	public void Step_into_enters_function() {
		// Source with a helper function called from the handler
		var source = "function helper(x) {\nreturn x + 1;\n}\nfromAll().when({\n$init: function() { return { count: 0 }; },\nItemAdded: function(s, e) {\ns.count = helper(s.count);\nreturn s;\n}\n})";
		using var session = new ProjectionSession(source, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2, Debug = true });

		var breaks = new List<BreakInfo>();
		session.OnBreak = info => breaks.Add(info);
		session.SetBreakpoint(7); // s.count = helper(s.count);

		var feedDone = new ManualResetEventSlim(false);
		var feedThread = new Thread(() => {
			session.Feed(MakeEvent());
			feedDone.Set();
		});
		feedThread.Start();

		SpinWait.SpinUntil(() => breaks.Count >= 1, TimeSpan.FromSeconds(5));
		Assert.Equal(7, breaks[0].Line);

		// Step into should enter helper function (line 2: return x + 1;)
		session.StepInto();
		SpinWait.SpinUntil(() => breaks.Count >= 2, TimeSpan.FromSeconds(5));
		Assert.Equal("step", breaks[1].Reason);
		Assert.Equal(2, breaks[1].Line);

		session.Continue();
		Assert.True(feedDone.Wait(TimeSpan.FromSeconds(5)));
		Assert.Contains("\"count\":1", session.GetState()!);
	}

	[Fact]
	public void Step_out_exits_function() {
		var source = "function helper(x) {\nreturn x + 1;\n}\nfromAll().when({\n$init: function() { return { count: 0 }; },\nItemAdded: function(s, e) {\ns.count = helper(s.count);\nreturn s;\n}\n})";
		using var session = new ProjectionSession(source, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2, Debug = true });

		var breaks = new List<BreakInfo>();
		session.OnBreak = info => breaks.Add(info);
		session.SetBreakpoint(2); // return x + 1; (inside helper)

		var feedDone = new ManualResetEventSlim(false);
		var feedThread = new Thread(() => {
			session.Feed(MakeEvent());
			feedDone.Set();
		});
		feedThread.Start();

		SpinWait.SpinUntil(() => breaks.Count >= 1, TimeSpan.FromSeconds(5));
		Assert.Equal(2, breaks[0].Line);

		// Step out should return to the caller
		session.StepOut();
		SpinWait.SpinUntil(() => breaks.Count >= 2, TimeSpan.FromSeconds(5));
		Assert.Equal("step", breaks[1].Reason);
		// Should be back in the handler, past the helper call
		Assert.True(breaks[1].Line >= 7);

		session.Continue();
		Assert.True(feedDone.Wait(TimeSpan.FromSeconds(5)));
	}

	[Fact]
	public void Conditional_breakpoint_pauses_when_true() {
		var source = "fromAll().when({\n$init: function() { return { count: 0 }; },\nItemAdded: function(s, e) {\ns.count++;\nreturn s;\n}\n})";
		using var session = new ProjectionSession(source, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2, Debug = true });

		var breaks = new List<BreakInfo>();
		session.OnBreak = info => breaks.Add(info);
		session.SetBreakpoint(4, 1, "s.count >= 2");

		// First two feeds - condition false (count is 0 then 1)
		session.Feed(MakeEvent());
		session.Feed(MakeEvent());
		Assert.Empty(breaks);
		Assert.Contains("\"count\":2", session.GetState()!);

		// Third feed - condition true (count is 2 before increment)
		var feedDone = new ManualResetEventSlim(false);
		var feedThread = new Thread(() => {
			session.Feed(MakeEvent());
			feedDone.Set();
		});
		feedThread.Start();

		SpinWait.SpinUntil(() => breaks.Count >= 1, TimeSpan.FromSeconds(5));
		Assert.Single(breaks);
		Assert.Equal("breakpoint", breaks[0].Reason);

		session.Continue();
		Assert.True(feedDone.Wait(TimeSpan.FromSeconds(5)));
		Assert.Contains("\"count\":3", session.GetState()!);
	}

	[Fact]
	public void Conditional_breakpoint_never_pauses_when_false() {
		var source = "fromAll().when({\n$init: function() { return { count: 0 }; },\nItemAdded: function(s, e) {\ns.count++;\nreturn s;\n}\n})";
		using var session = new ProjectionSession(source, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2, Debug = true });

		var breaks = new List<BreakInfo>();
		session.OnBreak = info => breaks.Add(info);
		session.SetBreakpoint(4, 1, "false");

		session.Feed(MakeEvent());
		session.Feed(MakeEvent());
		session.Feed(MakeEvent());

		Assert.Empty(breaks);
		Assert.Contains("\"count\":3", session.GetState()!);
	}

	[Fact]
	public void Hit_count_breakpoint() {
		var source = "fromAll().when({\n$init: function() { return { count: 0 }; },\nItemAdded: function(s, e) {\ns.count++;\nreturn s;\n}\n})";
		using var session = new ProjectionSession(source, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2, Debug = true });

		var breaks = new List<BreakInfo>();
		session.OnBreak = info => breaks.Add(info);
		session.SetBreakpoint(4, 1, hitCondition: "= 3");

		// First two hits don't pause
		session.Feed(MakeEvent());
		session.Feed(MakeEvent());
		Assert.Empty(breaks);

		// Third hit pauses
		var feedDone = new ManualResetEventSlim(false);
		var feedThread = new Thread(() => {
			session.Feed(MakeEvent());
			feedDone.Set();
		});
		feedThread.Start();

		SpinWait.SpinUntil(() => breaks.Count >= 1, TimeSpan.FromSeconds(5));
		Assert.Single(breaks);

		session.Continue();
		Assert.True(feedDone.Wait(TimeSpan.FromSeconds(5)));
	}

	[Fact]
	public void Hit_count_modulo() {
		var source = "fromAll().when({\n$init: function() { return { count: 0 }; },\nItemAdded: function(s, e) {\ns.count++;\nreturn s;\n}\n})";
		using var session = new ProjectionSession(source, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2, Debug = true });

		var breaks = new List<BreakInfo>();
		session.OnBreak = info => breaks.Add(info);
		session.SetBreakpoint(4, 1, hitCondition: "% 2");

		// Hit 1: no pause. Hit 2: pause. Hit 3: no pause. Hit 4: pause.
		session.Feed(MakeEvent());
		Assert.Empty(breaks);

		var feedDone = new ManualResetEventSlim(false);
		var feedThread = new Thread(() => {
			session.Feed(MakeEvent());
			feedDone.Set();
		});
		feedThread.Start();
		SpinWait.SpinUntil(() => breaks.Count >= 1, TimeSpan.FromSeconds(5));
		Assert.Single(breaks);
		session.Continue();
		Assert.True(feedDone.Wait(TimeSpan.FromSeconds(5)));
	}

	[Fact]
	public void Logpoint_logs_instead_of_pausing() {
		var source = "fromAll().when({\n$init: function() { return { count: 0 }; },\nItemAdded: function(s, e) {\ns.count++;\nreturn s;\n}\n})";
		using var session = new ProjectionSession(source, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2, Debug = true });

		var logs = new List<string>();
		session.OnLog = msg => logs.Add(msg);
		var breaks = new List<BreakInfo>();
		session.OnBreak = info => breaks.Add(info);

		session.SetBreakpoint(4, 1, logMessage: "count is {s.count}");

		session.Feed(MakeEvent());
		session.Feed(MakeEvent());

		Assert.Empty(breaks);
		Assert.Equal(2, logs.Count);
		Assert.Equal("count is 0", logs[0]);
		Assert.Equal("count is 1", logs[1]);
	}

	[Fact]
	public void Pause_stops_at_first_statement_of_next_event() {
		var source = "fromAll().when({\n$init: function() { return { count: 0 }; },\nItemAdded: function(s, e) {\ns.count++;\nreturn s;\n}\n})";
		using var session = new ProjectionSession(source, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2, Debug = true });

		// Feed first event normally
		session.Feed(MakeEvent());
		Assert.Contains("\"count\":1", session.GetState()!);

		// Request pause before next event
		session.Pause();

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
		Assert.Equal("pause", breakInfo.Reason);
		Assert.True(breakInfo.Line > 0, "Should have a real line number with execution context");
		Assert.True(session.IsPaused);
		Assert.False(feedDone.IsSet);

		// Should have call stack (we're inside the handler)
		var frames = session.GetCallStack();
		Assert.True(frames.Length >= 1);

		// Continue - feed should complete
		session.Continue();
		Assert.True(feedDone.Wait(TimeSpan.FromSeconds(5)));
		Assert.Contains("\"count\":2", session.GetState()!);
	}

	[Fact]
	public void No_debug_mode_ignores_debugger_statement() {
		var source = "fromAll().when({\n$init: function() { return { count: 0 }; },\nItemAdded: function(s, e) {\ndebugger;\ns.count++;\nreturn s;\n}\n})";
		using var session = new ProjectionSession(source, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		session.Feed(MakeEvent());
		Assert.Contains("\"count\":1", session.GetState()!);
	}

	[Fact]
	public void Get_call_stack_during_pause() {
		var source = "fromAll().when({\n$init: function() { return { count: 0 }; },\nItemAdded: function handler(s, e) {\ns.count++;\nreturn s;\n}\n})";
		using var session = new ProjectionSession(source, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2, Debug = true });

		session.SetBreakpoint(4);
		DebugCallFrame[]? frames = null;

		var feedDone = new ManualResetEventSlim(false);
		var feedThread = new Thread(() => {
			session.Feed(MakeEvent());
			feedDone.Set();
		});
		feedThread.Start();

		SpinWait.SpinUntil(() => session.IsPaused, TimeSpan.FromSeconds(5));

		frames = session.GetCallStack();
		Assert.NotNull(frames);
		Assert.True(frames.Length >= 1);
		Assert.Equal("handler", frames[0].Name);
		Assert.Equal(4, frames[0].Line);
		Assert.True(frames[0].Column >= 1);

		session.Continue();
		Assert.True(feedDone.Wait(TimeSpan.FromSeconds(5)));
	}

	[Fact]
	public void Get_scopes_during_pause() {
		var source = "fromAll().when({\n$init: function() { return { count: 0 }; },\nItemAdded: function(s, e) {\ns.count++;\nreturn s;\n}\n})";
		using var session = new ProjectionSession(source, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2, Debug = true });

		session.SetBreakpoint(4);

		var feedDone = new ManualResetEventSlim(false);
		var feedThread = new Thread(() => {
			session.Feed(MakeEvent());
			feedDone.Set();
		});
		feedThread.Start();

		SpinWait.SpinUntil(() => session.IsPaused, TimeSpan.FromSeconds(5));

		var scopes = session.GetScopes(0);
		Assert.NotNull(scopes);
		Assert.True(scopes.Length >= 1);
		Assert.True(scopes[0].VariablesReference > 0);

		session.Continue();
		Assert.True(feedDone.Wait(TimeSpan.FromSeconds(5)));
	}

	[Fact]
	public void Get_variables_during_pause() {
		var source = "fromAll().when({\n$init: function() { return { count: 0 }; },\nItemAdded: function(s, e) {\ns.count++;\nreturn s;\n}\n})";
		using var session = new ProjectionSession(source, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2, Debug = true });

		session.SetBreakpoint(4);

		var feedDone = new ManualResetEventSlim(false);
		var feedThread = new Thread(() => {
			session.Feed(MakeEvent());
			feedDone.Set();
		});
		feedThread.Start();

		SpinWait.SpinUntil(() => session.IsPaused, TimeSpan.FromSeconds(5));

		var scopes = session.GetScopes(0);
		var localScope = scopes[0];
		var variables = session.GetVariables(localScope.VariablesReference);

		Assert.NotNull(variables);
		Assert.True(variables.Length >= 1);
		var sParam = Assert.Single(variables, v => v.Name == "s");
		Assert.Equal("object", sParam.Type);
		var eParam = Assert.Single(variables, v => v.Name == "e");
		Assert.Equal("object", eParam.Type);

		session.Continue();
		Assert.True(feedDone.Wait(TimeSpan.FromSeconds(5)));
	}

	[Fact]
	public void Expand_object_properties() {
		var source = "fromAll().when({\n$init: function() { return { count: 0, name: \"test\" }; },\nItemAdded: function(s, e) {\ns.count++;\nreturn s;\n}\n})";
		using var session = new ProjectionSession(source, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2, Debug = true });

		session.SetBreakpoint(4);

		var feedDone = new ManualResetEventSlim(false);
		var feedThread = new Thread(() => {
			session.Feed(MakeEvent());
			feedDone.Set();
		});
		feedThread.Start();

		SpinWait.SpinUntil(() => session.IsPaused, TimeSpan.FromSeconds(5));

		var scopes = session.GetScopes(0);
		var variables = session.GetVariables(scopes[0].VariablesReference);

		// Find 's' which should be expandable (it's an object with count and name)
		var sVar = Assert.Single(variables, v => v.Name == "s");
		Assert.Equal("object", sVar.Type);
		Assert.True(sVar.VariablesReference > 0, "Object should be expandable");

		// Expand 's' to see its properties
		var props = session.GetVariables(sVar.VariablesReference);
		var countProp = Assert.Single(props, p => p.Name == "count");
		Assert.Equal("0", countProp.Value);
		Assert.Equal("number", countProp.Type);
		Assert.Equal(0, countProp.VariablesReference); // primitive, not expandable

		var nameProp = Assert.Single(props, p => p.Name == "name");
		Assert.Equal("\"test\"", nameProp.Value);
		Assert.Equal("string", nameProp.Type);

		session.Continue();
		Assert.True(feedDone.Wait(TimeSpan.FromSeconds(5)));
	}

	[Fact]
	public void Expand_array_elements() {
		var source = "fromAll().when({\n$init: function() { return { items: [10, 20, 30] }; },\nItemAdded: function(s, e) {\nreturn s;\n}\n})";
		using var session = new ProjectionSession(source, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2, Debug = true });

		session.SetBreakpoint(4);

		var feedDone = new ManualResetEventSlim(false);
		var feedThread = new Thread(() => {
			session.Feed(MakeEvent());
			feedDone.Set();
		});
		feedThread.Start();

		SpinWait.SpinUntil(() => session.IsPaused, TimeSpan.FromSeconds(5));

		var scopes = session.GetScopes(0);
		var variables = session.GetVariables(scopes[0].VariablesReference);

		var sVar = Assert.Single(variables, v => v.Name == "s");
		var sProps = session.GetVariables(sVar.VariablesReference);

		var itemsProp = Assert.Single(sProps, p => p.Name == "items");
		Assert.True(itemsProp.VariablesReference > 0, "Array should be expandable");
		Assert.Contains("[10, 20, 30]", itemsProp.Value);

		// Expand the array
		var elements = session.GetVariables(itemsProp.VariablesReference);
		Assert.Equal("10", Assert.Single(elements, e => e.Name == "0").Value);
		Assert.Equal("20", Assert.Single(elements, e => e.Name == "1").Value);
		Assert.Equal("30", Assert.Single(elements, e => e.Name == "2").Value);
		Assert.Equal("3", Assert.Single(elements, e => e.Name == "length").Value);

		session.Continue();
		Assert.True(feedDone.Wait(TimeSpan.FromSeconds(5)));
	}

	[Fact]
	public void Nested_object_expansion() {
		var source = "fromAll().when({\n$init: function() { return { nested: { a: 1, b: { c: 2 } } }; },\nItemAdded: function(s, e) {\nreturn s;\n}\n})";
		using var session = new ProjectionSession(source, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2, Debug = true });

		session.SetBreakpoint(4);

		var feedDone = new ManualResetEventSlim(false);
		var feedThread = new Thread(() => {
			session.Feed(MakeEvent());
			feedDone.Set();
		});
		feedThread.Start();

		SpinWait.SpinUntil(() => session.IsPaused, TimeSpan.FromSeconds(5));

		var scopes = session.GetScopes(0);
		var variables = session.GetVariables(scopes[0].VariablesReference);

		var sVar = Assert.Single(variables, v => v.Name == "s");
		var sProps = session.GetVariables(sVar.VariablesReference);

		var nestedProp = Assert.Single(sProps, p => p.Name == "nested");
		Assert.True(nestedProp.VariablesReference > 0);

		var nestedProps = session.GetVariables(nestedProp.VariablesReference);
		Assert.Equal("1", Assert.Single(nestedProps, p => p.Name == "a").Value);

		var bProp = Assert.Single(nestedProps, p => p.Name == "b");
		Assert.True(bProp.VariablesReference > 0);

		var bProps = session.GetVariables(bProp.VariablesReference);
		Assert.Equal("2", Assert.Single(bProps, p => p.Name == "c").Value);

		session.Continue();
		Assert.True(feedDone.Wait(TimeSpan.FromSeconds(5)));
	}

	[Fact]
	public void Evaluate_expression_returns_result() {
		var source = "fromAll().when({\n$init: function() { return { count: 5 }; },\nItemAdded: function(s, e) {\ns.count++;\nreturn s;\n}\n})";
		using var session = new ProjectionSession(source, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2, Debug = true });

		session.SetBreakpoint(4);

		var feedDone = new ManualResetEventSlim(false);
		var feedThread = new Thread(() => {
			session.Feed(MakeEvent());
			feedDone.Set();
		});
		feedThread.Start();

		SpinWait.SpinUntil(() => session.IsPaused, TimeSpan.FromSeconds(5));

		// Evaluate a simple expression
		var result = session.Evaluate("1 + 2");
		Assert.Equal("3", result.Value);
		Assert.Equal("number", result.Type);
		Assert.Equal(0, result.VariablesReference);

		// Evaluate accessing local variable
		var stateResult = session.Evaluate("s.count");
		Assert.Equal("5", stateResult.Value);

		// Evaluate returning an object (should be expandable)
		var objResult = session.Evaluate("s");
		Assert.Equal("object", objResult.Type);
		Assert.True(objResult.VariablesReference > 0);

		// Expand the evaluated object
		var props = session.GetVariables(objResult.VariablesReference);
		var countProp = Assert.Single(props, p => p.Name == "count");
		Assert.Equal("5", countProp.Value);

		session.Continue();
		Assert.True(feedDone.Wait(TimeSpan.FromSeconds(5)));
	}

	[Fact]
	public void Evaluate_invalid_expression_throws() {
		var source = "fromAll().when({\n$init: function() { return { count: 0 }; },\nItemAdded: function(s, e) {\ns.count++;\nreturn s;\n}\n})";
		using var session = new ProjectionSession(source, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2, Debug = true });

		session.SetBreakpoint(4);

		var feedDone = new ManualResetEventSlim(false);
		var feedThread = new Thread(() => {
			session.Feed(MakeEvent());
			feedDone.Set();
		});
		feedThread.Start();

		SpinWait.SpinUntil(() => session.IsPaused, TimeSpan.FromSeconds(5));

		Assert.ThrowsAny<Exception>(() => session.Evaluate("this is not valid {{{{"));

		// Session should still be functional after eval error
		var result = session.Evaluate("1 + 1");
		Assert.Equal("2", result.Value);

		session.Continue();
		Assert.True(feedDone.Wait(TimeSpan.FromSeconds(5)));
	}

	[Fact]
	public void Inspect_when_not_paused_throws() {
		var source = "fromAll().when({\n$init: function() { return { count: 0 }; },\nItemAdded: function(s, e) { s.count++; return s; }\n})";
		using var session = new ProjectionSession(source, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2, Debug = true });

		Assert.Throws<InvalidOperationException>(() => session.GetCallStack());
		Assert.Throws<InvalidOperationException>(() => session.GetScopes(0));
		Assert.Throws<InvalidOperationException>(() => session.GetVariables(1));
		Assert.Throws<InvalidOperationException>(() => session.Continue());
	}

	[Fact]
	public void Invalid_variable_reference_throws_during_pause() {
		var source = "fromAll().when({\n$init: function() { return { count: 0 }; },\nItemAdded: function(s, e) {\ns.count++;\nreturn s;\n}\n})";
		using var session = new ProjectionSession(source, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2, Debug = true });

		session.SetBreakpoint(4);

		var feedDone = new ManualResetEventSlim(false);
		var feedThread = new Thread(() => {
			session.Feed(MakeEvent());
			feedDone.Set();
		});
		feedThread.Start();

		SpinWait.SpinUntil(() => session.IsPaused, TimeSpan.FromSeconds(5));

		Assert.Throws<InvalidOperationException>(() => session.GetVariables(999));
		Assert.Throws<ArgumentOutOfRangeException>(() => session.GetScopes(999));

		// Session should still be paused and functional after errors
		Assert.True(session.IsPaused);
		var frames = session.GetCallStack();
		Assert.NotNull(frames);

		session.Continue();
		Assert.True(feedDone.Wait(TimeSpan.FromSeconds(5)));
	}

	[Fact]
	public void Timeout_does_not_fire_during_pause() {
		var source = "fromAll().when({\n$init: function() { return { count: 0 }; },\nItemAdded: function(s, e) {\ns.count++;\nreturn s;\n}\n})";
		using var session = new ProjectionSession(source, new ProjectionSessionOptions {
			EngineVersion = ProjectionVersion.V2,
			Debug = true,
			ExecutionTimeout = TimeSpan.FromMilliseconds(500),
		});

		session.SetBreakpoint(4);

		var feedDone = new ManualResetEventSlim(false);
		FeedResult? result = null;
		Exception? feedEx = null;

		var feedThread = new Thread(() => {
			try {
				result = session.Feed(MakeEvent());
			} catch (Exception ex) {
				feedEx = ex;
			}
			feedDone.Set();
		});
		feedThread.Start();

		SpinWait.SpinUntil(() => session.IsPaused, TimeSpan.FromSeconds(5));

		// Wait longer than the execution timeout while paused
		Thread.Sleep(1000);

		session.Continue();
		Assert.True(feedDone.Wait(TimeSpan.FromSeconds(5)));

		Assert.Null(feedEx);
		Assert.NotNull(result);
	}

	[Fact]
	public void Racing_resume_verbs_never_strand_a_caller() {
		// UI-1822: a verb that passed the paused check could enqueue its
		// command just as the command loop consumed another caller's resume
		// and exited - the command's Done was never signalled and the caller
		// blocked forever. The enqueue and the resume-drain are now atomic:
		// every racer either wins the resume or gets an error, and always
		// returns. The Join timeouts are the strand detectors.
		var source = "fromAll().when({\n$init: function() { return { count: 0 }; },\nItemAdded: function(s, e) {\ns.count++;\nreturn s;\n}\n})";
		using var session = new ProjectionSession(source, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2, Debug = true });

		var paused = new ManualResetEventSlim(false);
		session.OnBreak = _ => paused.Set();
		session.SetBreakpoint(4);

		for (var i = 0; i < 25; i++) {
			paused.Reset();
			Exception? feedEx = null;
			var feedThread = new Thread(() => {
				try {
					session.Feed(MakeEvent());
				} catch (Exception ex) {
					feedEx = ex;
				}
			});
			feedThread.Start();
			Assert.True(paused.Wait(TimeSpan.FromSeconds(5)));

			var racers = new Thread[3];
			for (var r = 0; r < racers.Length; r++) {
				racers[r] = new Thread(() => {
					try {
						session.Continue();
					} catch (InvalidOperationException) {
						// Lost the race: "not paused", or drained with
						// "resumed before the command ran". Both fine -
						// only hanging is a bug.
					}
				});
				racers[r].Start();
			}
			foreach (var racer in racers)
				Assert.True(racer.Join(TimeSpan.FromSeconds(10)), "a resume verb stranded: its Done was never signalled");
			Assert.True(feedThread.Join(TimeSpan.FromSeconds(10)));
			Assert.Null(feedEx);
		}

		Assert.Contains("\"count\":25", session.GetState()!);
	}

	[Fact]
	public void Resume_race_does_not_replay_into_the_next_pause() {
		// The other half of UI-1822: a command left in the queue when the
		// loop exited replayed at the NEXT pause - a stale Continue from a
		// lost race would silently resume a breakpoint the user never
		// continued. The resume-drain must leave the queue empty.
		var source = "fromAll().when({\n$init: function() { return { count: 0 }; },\nItemAdded: function(s, e) {\ns.count++;\nreturn s;\n}\n})";
		using var session = new ProjectionSession(source, new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2, Debug = true });

		var paused = new ManualResetEventSlim(false);
		session.OnBreak = _ => paused.Set();
		session.SetBreakpoint(4);

		Exception? feedEx = null;
		var feed1 = new Thread(() => {
			try {
				session.Feed(MakeEvent());
			} catch (Exception ex) {
				feedEx = ex;
			}
		});
		feed1.Start();
		Assert.True(paused.Wait(TimeSpan.FromSeconds(5)));

		var racers = new Thread[2];
		for (var r = 0; r < racers.Length; r++) {
			racers[r] = new Thread(() => {
				try {
					session.Continue();
				} catch (InvalidOperationException) {
				}
			});
			racers[r].Start();
		}
		foreach (var racer in racers)
			Assert.True(racer.Join(TimeSpan.FromSeconds(10)), "a resume verb stranded: its Done was never signalled");
		Assert.True(feed1.Join(TimeSpan.FromSeconds(10)));
		Assert.Null(feedEx);

		// The second pause must wait for its own resume.
		paused.Reset();
		var feed2 = new Thread(() => {
			try {
				session.Feed(MakeEvent());
			} catch (Exception ex) {
				feedEx = ex;
			}
		});
		feed2.Start();
		Assert.True(paused.Wait(TimeSpan.FromSeconds(5)));
		Assert.False(feed2.Join(TimeSpan.FromMilliseconds(300)), "second pause auto-resumed: a stale command replayed");
		Assert.True(session.IsPaused);

		session.Continue();
		Assert.True(feed2.Join(TimeSpan.FromSeconds(10)));
		Assert.Null(feedEx);
		Assert.Contains("\"count\":2", session.GetState()!);
	}
}
