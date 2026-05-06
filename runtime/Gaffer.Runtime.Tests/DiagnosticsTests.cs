using Gaffer.Sdk;

namespace Gaffer.Runtime.Tests;

public class DiagnosticsTests {
	private static readonly ProjectionSessionOptions Options = new() { EngineVersion = ProjectionVersion.V2 };

	[Fact]
	public void NoDiagnostics_ForCleanProjection() {
		using var session = new ProjectionSession(
			"fromAll().when({ $any: function (s, e) { return s; } });", Options);

		Assert.Null(session.Diagnostics);
	}

	[Fact]
	public void LinkStreamTo_Detected() {
		// linkStreamTo starts at column 42 (1-based) on line 1; "linkStreamTo" is 12 chars, end column 54.
		var source = "fromAll().when({ $any: function (s, e) { linkStreamTo('a-' + e.streamId, e.streamId); return s; } });";

		using var session = new ProjectionSession(source, Options);

		Assert.NotNull(session.Diagnostics);
		var d = Assert.Single(session.Diagnostics!);
		Assert.Equal("deprecated.linkStreamTo", d.Code);
		Assert.Equal(DiagnosticSeverity.Warning, d.Severity);
		Assert.Contains("linkStreamTo", d.Message);
		Assert.NotNull(d.Range);
		Assert.Equal(1, d.Range!.Start.Line);
		Assert.Equal(42, d.Range.Start.Column);
		Assert.Equal(1, d.Range.End.Line);
		Assert.Equal(54, d.Range.End.Column);
	}

	[Fact]
	public void LinkStreamTo_MultipleCalls_AllReported() {
		var source =
			"fromAll().when({\n" +
			"  A: function (s, e) { linkStreamTo('a', e.streamId); return s; },\n" +
			"  B: function (s, e) { linkStreamTo('b', e.streamId); return s; },\n" +
			"});";

		using var session = new ProjectionSession(source, Options);

		Assert.NotNull(session.Diagnostics);
		Assert.Equal(2, session.Diagnostics!.Length);
		Assert.All(session.Diagnostics, d => Assert.Equal("deprecated.linkStreamTo", d.Code));
		Assert.Equal(2, session.Diagnostics[0].Range!.Start.Line);
		Assert.Equal(3, session.Diagnostics[1].Range!.Start.Line);
	}

	[Fact]
	public void LinkStreamTo_InStringLiteral_NotDetected() {
		using var session = new ProjectionSession(
			"fromAll().when({ $any: function (s, e) { var x = 'linkStreamTo(...)'; return s; } });",
			Options);

		Assert.Null(session.Diagnostics);
	}

	[Fact]
	public void LinkStreamTo_InNestedHandler_Detected() {
		// transformBy callbacks, arrow functions, closures - the visitor walks
		// every CallExpression, so nesting shouldn't matter.
		var source =
			"fromAll().when({\n" +
			"  $any: function (s, e) {\n" +
			"    [1].forEach((n) => { linkStreamTo('a-' + n, e.streamId); });\n" +
			"    return s;\n" +
			"  },\n" +
			"}).transformBy(function (s) { linkStreamTo('t', 's'); return s; });";

		using var session = new ProjectionSession(source, Options);

		Assert.NotNull(session.Diagnostics);
		Assert.Equal(2, session.Diagnostics!.Length);
	}

	[Fact]
	public void LinkStreamTo_ZeroArgs_StillDetected() {
		// We match on callee identity, not arity - a zero-arg call is still a
		// linkStreamTo invocation regardless of whether it would do anything.
		using var session = new ProjectionSession(
			"fromAll().when({ $any: function (s, e) { linkStreamTo(); return s; } });",
			Options);

		Assert.NotNull(session.Diagnostics);
		Assert.Single(session.Diagnostics!);
	}

	[Fact]
	public void LinkStreamTo_AsMemberAccess_NotDetected() {
		// "obj.linkStreamTo()" - same name but a MemberExpression callee, not an Identifier.
		using var session = new ProjectionSession(
			"fromAll().when({ $any: function (s, e) { var o = { linkStreamTo: function () {} }; o.linkStreamTo(); return s; } });",
			Options);

		Assert.Null(session.Diagnostics);
	}
}
