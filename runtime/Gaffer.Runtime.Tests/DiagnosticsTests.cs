using Gaffer.Runtime.Errors;
using Gaffer.Runtime.Projection;
using Gaffer.Sdk.Diagnostics;
using Gaffer.Sdk.Versioning;

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
		var source = "fromAll().when({ $any: function (s, e) { linkStreamTo('a-' + e.streamId, e.streamId); return s; } });";
		var expectedCol = source.IndexOf("linkStreamTo", StringComparison.Ordinal) + 1;

		using var session = new ProjectionSession(source, Options);

		Assert.NotNull(session.Diagnostics);
		var d = Assert.Single(session.Diagnostics!);
		Assert.Equal("deprecated.linkStreamTo", d.Code);
		Assert.Equal(DiagnosticSeverity.Warning, d.Severity);
		Assert.Contains("linkStreamTo", d.Message);
		Assert.NotNull(d.Range);
		Assert.Equal(1, d.Range!.Start.Line);
		Assert.Equal(expectedCol, d.Range.Start.Column);
		Assert.Equal(1, d.Range.End.Line);
		Assert.Equal(expectedCol + "linkStreamTo".Length, d.Range.End.Column);
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
		var linkStreamToDiagnostics = session.Diagnostics!
			.Where(d => d.Code == "deprecated.linkStreamTo").ToArray();
		Assert.Equal(2, linkStreamToDiagnostics.Length);
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
	public void Scan_ReturnsNullOnParseFailure() {
		// Source Acornima rejects. The fallback path in Scan returns null
		// so parser drift between Jint and Acornima doesn't break otherwise-
		// valid sessions.
		Assert.Null(DiagnosticCollector.Scan("this is not valid {{{{", dbVersion: null, engineVersion: ProjectionVersion.V2));
	}

	[Fact]
	public void LinkStreamTo_SuppressedWhenLocallyDeclared() {
		// User shadows the runtime global with their own linkStreamTo - the
		// call isn't the deprecated API, so we shouldn't warn. Conservative
		// suppression: any var/let/const/function declaration with the name
		// anywhere in the script switches the rule off.
		using var session = new ProjectionSession(
			"var linkStreamTo = function () {};\n" +
			"fromAll().when({ $any: function (s, e) { linkStreamTo('a', e.streamId); return s; } });",
			Options);

		Assert.Null(session.Diagnostics);
	}

	[Fact]
	public void LinkStreamTo_SuppressedWhenLocalFunction() {
		using var session = new ProjectionSession(
			"function linkStreamTo() {}\n" +
			"fromAll().when({ $any: function (s, e) { linkStreamTo(); return s; } });",
			Options);

		Assert.Null(session.Diagnostics);
	}

	[Fact]
	public void Ctor_ThrowsAndDisposes_OnPostHandlerValidation() {
		// Triggers the HandlesDeletedNotifications && !ByStreams validation
		// throw - which happens AFTER the Jint handler is constructed, so
		// the ctor's try/dispose block is responsible for tearing the
		// handler down. We can't easily observe disposal directly; the test
		// pins that the throw goes through that path without crashing.
		Assert.Throws<InvalidProjectionException>(() =>
			new ProjectionSession(
				"fromAll().when({ $deleted: function (s, e) { return s; } });",
				Options));
	}

	[Fact]
	public void LinkStreamTo_AsMemberAccess_NotDetected() {
		// "obj.linkStreamTo()" - same name but a MemberExpression callee, not an Identifier.
		using var session = new ProjectionSession(
			"fromAll().when({ $any: function (s, e) { var o = { linkStreamTo: function () {} }; o.linkStreamTo(); return s; } });",
			Options);

		Assert.Null(session.Diagnostics);
	}

	// -- compat.linkStreamTo.outOfBoundsParameters --

	[Fact]
	public void LinkStreamTo_ThreeArgs_EmitsOutOfBoundsParametersWarning() {
		using var session = new ProjectionSession(
			"fromAll().when({ $any: function (s, e) { linkStreamTo('a', e.streamId, { reason: 'x' }); return s; } });",
			Options);

		Assert.NotNull(session.Diagnostics);
		// Both the deprecation warning AND the compat warning should fire
		// for a 3-arg linkStreamTo call.
		Assert.Contains(session.Diagnostics!, d => d.Code == "deprecated.linkStreamTo");
		Assert.Contains(session.Diagnostics!, d => d.Code == KnownBugs.LinkStreamToOutOfBoundsParameters.Code);
	}

	[Fact]
	public void LinkStreamTo_TwoArgs_DoesNotEmitOutOfBoundsParametersWarning() {
		using var session = new ProjectionSession(
			"fromAll().when({ $any: function (s, e) { linkStreamTo('a', e.streamId); return s; } });",
			Options);

		Assert.NotNull(session.Diagnostics);
		// Deprecation fires (any call); compat doesn't (2-arg form is fine in upstream).
		Assert.Contains(session.Diagnostics!, d => d.Code == "deprecated.linkStreamTo");
		Assert.DoesNotContain(session.Diagnostics!, d => d.Code == KnownBugs.LinkStreamToOutOfBoundsParameters.Code);
	}

	[Fact]
	public void LinkStreamTo_ThreeArgs_SuppressedWhenShadowed() {
		using var session = new ProjectionSession(
			"function linkStreamTo() {}\n" +
			"fromAll().when({ $any: function (s, e) { linkStreamTo('a', e.streamId, {}); return s; } });",
			Options);

		// User's local linkStreamTo masks the upstream bug entirely - no
		// diagnostics at all (deprecation suppressed too).
		Assert.Null(session.Diagnostics);
	}

	// -- compat.log.multiParam --

	[Fact]
	public void Log_MultipleArgs_EmitsMultiParamWarning() {
		using var session = new ProjectionSession(
			"fromAll().when({ $any: function (s, e) { log('a', 'b'); return s; } });",
			Options);

		Assert.NotNull(session.Diagnostics);
		Assert.Contains(session.Diagnostics!, d => d.Code == KnownBugs.LogMultiParam.Code);
	}

	[Fact]
	public void Log_SingleArg_DoesNotEmitMultiParamWarning() {
		using var session = new ProjectionSession(
			"fromAll().when({ $any: function (s, e) { log('hello'); return s; } });",
			Options);

		// Single-arg log() is fine in upstream; no diagnostic.
		Assert.Null(session.Diagnostics);
	}

	// -- compat.transforms.notApplied (V2) --

	[Fact]
	public void Transforms_TransformBy_InV2_EmitsWarning() {
		var source = "fromAll().when({ $any: function (s, e) { return s; } }).transformBy(function (s) { return s; });";
		using var session = new ProjectionSession(source,
			new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		Assert.NotNull(session.Diagnostics);
		var d = Assert.Single(session.Diagnostics!);
		Assert.Equal("compat.transforms.notApplied", d.Code);
		Assert.Equal(DiagnosticSeverity.Warning, d.Severity);
		Assert.Contains("transformBy", d.Message);
	}

	[Fact]
	public void Transforms_FilterBy_InV2_EmitsWarning() {
		var source = "fromAll().when({ $any: function (s, e) { return s; } }).filterBy(function (s) { return true; });";
		using var session = new ProjectionSession(source,
			new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		Assert.NotNull(session.Diagnostics);
		var d = Assert.Single(session.Diagnostics!);
		Assert.Equal("compat.transforms.notApplied", d.Code);
		Assert.Contains("filterBy", d.Message);
	}

	[Fact]
	public void Transforms_InV1_NoDiagnostic() {
		var source = "fromAll().when({ $any: function (s, e) { return s; } }).transformBy(function (s) { return s; }).filterBy(function (s) { return true; });";
		using var session = new ProjectionSession(source,
			new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V1 });

		Assert.Null(session.Diagnostics);
	}

	[Fact]
	public void Transforms_InV2_Unversioned_StillEmits() {
		// engine_version is independent of db_version. The V2 diagnostic
		// fires whenever the engine is V2, regardless of dbVersion.
		var source = "fromAll().when({ $any: function (s, e) { return s; } }).transformBy(function (s) { return s; });";
		using var session = new ProjectionSession(source,
			new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2, DbVersion = null });

		Assert.NotNull(session.Diagnostics);
		Assert.Contains(session.Diagnostics!, d => d.Code == "compat.transforms.notApplied");
	}

	[Fact]
	public void Transforms_TransformBy_Shadowed_NoDiagnostic() {
		var source =
			"var transformBy = function () {};\n" +
			"fromAll().when({ $any: function (s, e) { return s; } }).transformBy(function (s) { return s; });";
		using var session = new ProjectionSession(source,
			new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		// Shadowed identifier means the call doesn't reach the V2 builtin.
		Assert.Null(session.Diagnostics);
	}

	// -- compat.outputState.unconditional (V2) --

	[Fact]
	public void OutputState_InV2_EmitsHint() {
		var source = "fromAll().when({ $any: function (s, e) { return s; } }).outputState();";
		using var session = new ProjectionSession(source,
			new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		Assert.NotNull(session.Diagnostics);
		var d = Assert.Single(session.Diagnostics!);
		Assert.Equal("compat.outputState.unconditional", d.Code);
		Assert.Equal(DiagnosticSeverity.Hint, d.Severity);
		Assert.Contains("outputState", d.Message);
	}

	[Fact]
	public void OutputState_InV1_NoDiagnostic() {
		var source = "fromAll().when({ $any: function (s, e) { return s; } }).outputState();";
		using var session = new ProjectionSession(source,
			new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V1 });

		Assert.Null(session.Diagnostics);
	}

	[Fact]
	public void OutputState_Shadowed_NoDiagnostic() {
		var source =
			"var outputState = function () {};\n" +
			"fromAll().when({ $any: function (s, e) { return s; } }).outputState();";
		using var session = new ProjectionSession(source,
			new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		Assert.Null(session.Diagnostics);
	}

}
