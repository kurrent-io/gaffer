using Gaffer.Runtime.Errors;
using Gaffer.Runtime.Projection;
using Gaffer.Runtime.Projection.Diagnostics;
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
	public void DuplicateOptions_WarnsOnEachCallPastFirst() {
		var source =
			"options({});\noptions({});\noptions({});\n" +
			"fromAll().when({ $any: function (s, e) { return s; } });";

		using var session = new ProjectionSession(source, Options);

		Assert.NotNull(session.Diagnostics);
		var dups = session.Diagnostics!.Where(d => d.Code == "usage.options.duplicate").ToArray();
		Assert.Equal(2, dups.Length); // first call is fine; calls 2 and 3 are flagged
		Assert.All(dups, d => Assert.Equal(DiagnosticSeverity.Information, d.Severity));
	}

	[Fact]
	public void SingleOptions_NoDuplicateWarning() {
		using var session = new ProjectionSession(
			"options({});\nfromAll().when({ $any: function (s, e) { return s; } });", Options);

		Assert.DoesNotContain(session.Diagnostics ?? [], d => d.Code == "usage.options.duplicate");
	}

	private static readonly ProjectionSessionOptions V1Options = new() { EngineVersion = ProjectionVersion.V1 };

	// --- engine_version 2: reorderEvents is a silent no-op -> Warning, regardless of source ---

	[Fact]
	public void ReorderEvents_OnV2_WarnsNoEffect() {
		using var session = new ProjectionSession(
			"options({ reorderEvents: true });\nfromAll().when({ $any: function (s, e) { return s; } });", Options);

		var d = Assert.Single(session.Diagnostics ?? [], x => x.Code == "usage.reorderEvents.noEffectOnV2");
		Assert.Equal(DiagnosticSeverity.Warning, d.Severity);
	}

	[Fact]
	public void ReorderEvents_OnV2_FromStreams_StillWarns() {
		// V2 ignores reorderEvents on every source, including a valid V1 fromStreams config.
		using var session = new ProjectionSession(
			"options({ reorderEvents: true, processingLag: 100 });\nfromStreams('a', 'b').when({ $any: function (s, e) { return s; } });", Options);

		// Both the reorderEvents and the riding-along processingLag keys are flagged.
		Assert.Equal(2, (session.Diagnostics ?? []).Count(d => d.Code == "usage.reorderEvents.noEffectOnV2"));
	}

	[Fact]
	public void ReorderEvents_StringLiteralKey_OnV2_Warns() {
		using var session = new ProjectionSession(
			"options({ \"reorderEvents\": true });\nfromAll().when({ $any: function (s, e) { return s; } });", Options);

		Assert.Contains(session.Diagnostics ?? [], d => d.Code == "usage.reorderEvents.noEffectOnV2");
	}

	[Fact]
	public void ReorderEventsFalse_OnV2_NoDiagnostic() {
		// Explicitly off - nothing is being dropped.
		using var session = new ProjectionSession(
			"options({ reorderEvents: false });\nfromAll().when({ $any: function (s, e) { return s; } });", Options);

		Assert.DoesNotContain(session.Diagnostics ?? [], d => d.Code == "usage.reorderEvents.noEffectOnV2");
	}

	[Fact]
	public void LoneProcessingLag_OnV2_NoDiagnostic() {
		// processingLag without reorderEvents is a no-op on every engine, not a V2 regression.
		using var session = new ProjectionSession(
			"options({ processingLag: 100 });\nfromCategory('order').when({ $any: function (s, e) { return s; } });", Options);

		Assert.DoesNotContain(session.Diagnostics ?? [], d => d.Code == "usage.reorderEvents.noEffectOnV2");
	}

	[Fact]
	public void ReorderEventsFalseAfterTrue_OnV2_NoDiagnostic() {
		// Duplicate options() is last-write-wins; the resolved reorderEvents is false, so the
		// stale `true` from the first call must not leave a warning behind.
		using var session = new ProjectionSession(
			"options({ reorderEvents: true });\noptions({ reorderEvents: false });\nfromAll().when({ $any: function (s, e) { return s; } });", Options);

		Assert.DoesNotContain(session.Diagnostics ?? [], d => d.Code == "usage.reorderEvents.noEffectOnV2");
	}

	[Fact]
	public void ReorderEvents_ComputedKey_OnV2_Ignored() {
		// A computed key isn't statically resolvable, so the best-effort AST rule skips it.
		using var session = new ProjectionSession(
			"var k = 'reorderEvents';\noptions({ [k]: true });\nfromAll().when({ $any: function (s, e) { return s; } });", Options);

		Assert.DoesNotContain(session.Diagnostics ?? [], d => d.Code == "usage.reorderEvents.noEffectOnV2");
	}

	[Fact]
	public void ShadowedOptions_OnV2_Suppresses_ReorderWarning() {
		// A top-level user-defined `options` means the call isn't the builtin.
		using var session = new ProjectionSession(
			"function options(o) { return o; }\noptions({ reorderEvents: true });\nfromAll().when({ $any: function (s, e) { return s; } });", Options);

		Assert.DoesNotContain(session.Diagnostics ?? [], d => d.Code == "usage.reorderEvents.noEffectOnV2");
	}

	[Fact]
	public void ReorderEvents_OnV1_NoV2Warning() {
		// The V2 no-op warning must not fire under V1, where reorderEvents is honoured.
		using var session = new ProjectionSession(
			"options({ reorderEvents: true, processingLag: 100 });\nfromStreams('a', 'b').when({ $any: function (s, e) { return s; } });", V1Options);

		Assert.DoesNotContain(session.Diagnostics ?? [], d => d.Code == "usage.reorderEvents.noEffectOnV2");
	}

	// --- engine_version 1: KurrentDB ReaderStrategy rejects invalid reorderEvents -> throw ---

	[Fact]
	public void ReorderEvents_OnV1_FromAll_Throws() {
		var ex = Assert.Throws<InvalidProjectionException>(() => new ProjectionSession(
			"options({ reorderEvents: true, processingLag: 100 });\nfromAll().when({ $any: function (s, e) { return s; } });", V1Options));
		Assert.Contains("fromAll", ex.Message);
	}

	[Fact]
	public void ReorderEvents_OnV1_SingleStream_Throws() {
		var ex = Assert.Throws<InvalidProjectionException>(() => new ProjectionSession(
			"options({ reorderEvents: true, processingLag: 100 });\nfromStreams('a').when({ $any: function (s, e) { return s; } });", V1Options));
		Assert.Contains("fromStreams", ex.Message);
	}

	[Fact]
	public void ReorderEvents_OnV1_LagBelowFloor_Throws() {
		var ex = Assert.Throws<InvalidProjectionException>(() => new ProjectionSession(
			"options({ reorderEvents: true, processingLag: 10 });\nfromStreams('a', 'b').when({ $any: function (s, e) { return s; } });", V1Options));
		Assert.Contains("50ms", ex.Message);
	}

	[Fact]
	public void ReorderEvents_OnV1_LagAbsent_Throws() {
		// No processingLag defaults below the 50ms floor.
		Assert.Throws<InvalidProjectionException>(() => new ProjectionSession(
			"options({ reorderEvents: true });\nfromStreams('a', 'b').when({ $any: function (s, e) { return s; } });", V1Options));
	}

	[Fact]
	public void ReorderEvents_OnV1_ValidFromStreams_NoThrowNoDiagnostic() {
		using var session = new ProjectionSession(
			"options({ reorderEvents: true, processingLag: 100 });\nfromStreams('a', 'b').when({ $any: function (s, e) { return s; } });", V1Options);

		Assert.DoesNotContain(session.Diagnostics ?? [], d => d.Code == "usage.reorderEvents.noEffectOnV2");
	}

	[Fact]
	public void ReorderEvents_OnV1_FromCategory_Throws() {
		// fromCategory resolves to no Streams, so it fails the "fromStreams 2+" rule.
		var ex = Assert.Throws<InvalidProjectionException>(() => new ProjectionSession(
			"options({ reorderEvents: true, processingLag: 100 });\nfromCategory('order').when({ $any: function (s, e) { return s; } });", V1Options));
		Assert.Contains("fromStreams", ex.Message);
	}

	[Fact]
	public void ReorderEventsFalse_OnV1_NoThrow() {
		// Explicitly off - the validation never runs regardless of source.
		using var session = new ProjectionSession(
			"options({ reorderEvents: false });\nfromAll().when({ $any: function (s, e) { return s; } });", V1Options);

		Assert.NotNull(session);
	}

	[Fact]
	public void LoneProcessingLag_OnV1_NoThrow() {
		// processingLag without reorderEvents is carried and ignored by KurrentDB - never validated.
		using var session = new ProjectionSession(
			"options({ processingLag: 10 });\nfromAll().when({ $any: function (s, e) { return s; } });", V1Options);

		Assert.NotNull(session);
	}

	[Fact]
	public void AsyncFunctionHandler_Warns() {
		using var session = new ProjectionSession(
			"fromAll().when({ Ping: async function (s, e) { return s; } });", Options);

		var d = Assert.Single(session.Diagnostics ?? [], x => x.Code == "usage.handler.async");
		Assert.Equal(DiagnosticSeverity.Error, d.Severity);
	}

	[Fact]
	public void AsyncArrowHandler_Warns() {
		using var session = new ProjectionSession(
			"fromAll().when({ Ping: async (s, e) => s });", Options);

		Assert.Contains(session.Diagnostics ?? [], d => d.Code == "usage.handler.async");
	}

	[Fact]
	public void ReturnPromiseResolve_Warns() {
		using var session = new ProjectionSession(
			"fromAll().when({ Ping: function (s, e) { return Promise.resolve({}); } });", Options);

		var d = Assert.Single(session.Diagnostics ?? [], x => x.Code == "usage.handler.promise");
		Assert.Equal(DiagnosticSeverity.Error, d.Severity);
	}

	[Fact]
	public void ConciseArrowReturningPromise_Warns() {
		using var session = new ProjectionSession(
			"fromAll().when({ Ping: (s, e) => Promise.resolve({}) });", Options);

		Assert.Contains(session.Diagnostics ?? [], d => d.Code == "usage.handler.promise");
	}

	[Fact]
	public void NewPromiseReturn_Warns() {
		using var session = new ProjectionSession(
			"fromAll().when({ Ping: function (s, e) { return new Promise(function (r) { r(s); }); } });", Options);

		Assert.Contains(session.Diagnostics ?? [], d => d.Code == "usage.handler.promise");
	}

	[Fact]
	public void SyncHandler_NoAsyncOrPromiseDiagnostic() {
		using var session = new ProjectionSession(
			"fromAll().when({ Ping: function (s, e) { return { ok: true }; } });", Options);

		Assert.DoesNotContain(session.Diagnostics ?? [],
			d => d.Code == "usage.handler.async" || d.Code == "usage.handler.promise");
	}

	[Fact]
	public void InnerAsyncInSyncHandler_NoWarning() {
		// The handler returns a plain object; an inner async helper and a nested
		// Promise return must not be flagged as the handler's own behavior.
		using var session = new ProjectionSession(
			"fromAll().when({ Ping: function (s, e) { var f = async function () { return 1; }; function g() { return Promise.resolve(1); } return { ok: true }; } });", Options);

		Assert.DoesNotContain(session.Diagnostics ?? [],
			d => d.Code == "usage.handler.async" || d.Code == "usage.handler.promise");
	}

	[Fact]
	public void AsyncHelperOutsideHandler_NoWarning() {
		// A top-level async helper isn't a handler, so it isn't flagged.
		using var session = new ProjectionSession(
			"async function helper() { return 1; }\nfromAll().when({ Ping: function (s, e) { return s; } });", Options);

		Assert.DoesNotContain(session.Diagnostics ?? [],
			d => d.Code == "usage.handler.async" || d.Code == "usage.handler.promise");
	}

	[Fact]
	public void LinkStreamTo_Detected() {
		var source = "fromAll().when({ $any: function (s, e) { linkStreamTo('a-' + e.streamId, e.streamId); return s; } });";
		var expectedCol = source.IndexOf("linkStreamTo", StringComparison.Ordinal) + 1;

		using var session = new ProjectionSession(source, Options);

		Assert.NotNull(session.Diagnostics);
		var d = Assert.Single(session.Diagnostics!);
		Assert.Equal("usage.linkStreamTo.deprecated", d.Code);
		Assert.Equal(DiagnosticSeverity.Information, d.Severity);
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
		Assert.All(session.Diagnostics, d => Assert.Equal("usage.linkStreamTo.deprecated", d.Code));
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
			.Where(d => d.Code == "usage.linkStreamTo.deprecated").ToArray();
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
		Assert.Null(DiagnosticCollector.Scan("this is not valid {{{{", quirksVersion: null, engineVersion: ProjectionVersion.V2));
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

	// -- quirk.linkStreamTo.outOfBoundsParameters --

	[Fact]
	public void LinkStreamTo_ThreeArgs_EmitsOutOfBoundsParametersWarning() {
		using var session = new ProjectionSession(
			"fromAll().when({ $any: function (s, e) { linkStreamTo('a', e.streamId, { reason: 'x' }); return s; } });",
			Options);

		Assert.NotNull(session.Diagnostics);
		// Both the deprecation warning AND the compat warning should fire
		// for a 3-arg linkStreamTo call.
		Assert.Contains(session.Diagnostics!, d => d.Code == "usage.linkStreamTo.deprecated");
		Assert.Contains(session.Diagnostics!, d => d.Code == DiagnosticCatalog.LinkStreamToOutOfBoundsParameters.Code);
	}

	[Fact]
	public void LinkStreamTo_TwoArgs_DoesNotEmitOutOfBoundsParametersWarning() {
		using var session = new ProjectionSession(
			"fromAll().when({ $any: function (s, e) { linkStreamTo('a', e.streamId); return s; } });",
			Options);

		Assert.NotNull(session.Diagnostics);
		// Deprecation fires (any call); compat doesn't (2-arg form is fine in upstream).
		Assert.Contains(session.Diagnostics!, d => d.Code == "usage.linkStreamTo.deprecated");
		Assert.DoesNotContain(session.Diagnostics!, d => d.Code == DiagnosticCatalog.LinkStreamToOutOfBoundsParameters.Code);
	}

	[Fact]
	public void LinkStreamTo_ThreeArgs_SuppressedWhenShadowed() {
		using var session = new ProjectionSession(
			"function linkStreamTo() {}\n" +
			"fromAll().when({ $any: function (s, e) { linkStreamTo('a', e.streamId, {}); return s; } });",
			Options);

		// User's local linkStreamTo masks the upstream quirk entirely - no
		// diagnostics at all (deprecation suppressed too).
		Assert.Null(session.Diagnostics);
	}

	// -- quirk.log.multiParam --

	[Fact]
	public void Log_MultipleArgs_EmitsMultiParamWarning() {
		using var session = new ProjectionSession(
			"fromAll().when({ $any: function (s, e) { log('a', 'b'); return s; } });",
			Options);

		Assert.NotNull(session.Diagnostics);
		Assert.Contains(session.Diagnostics!, d => d.Code == DiagnosticCatalog.LogMultiParam.Code);
	}

	[Fact]
	public void Log_SingleArg_DoesNotEmitMultiParamWarning() {
		using var session = new ProjectionSession(
			"fromAll().when({ $any: function (s, e) { log('hello'); return s; } });",
			Options);

		// Single-arg log() is fine in upstream; no diagnostic.
		Assert.Null(session.Diagnostics);
	}

	// -- usage.transforms.notInvoked (V2) --

	[Fact]
	public void Transforms_TransformBy_InV2_EmitsWarning() {
		var source = "fromAll().when({ $any: function (s, e) { return s; } }).transformBy(function (s) { return s; });";
		using var session = new ProjectionSession(source,
			new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		Assert.NotNull(session.Diagnostics);
		var d = Assert.Single(session.Diagnostics!);
		Assert.Equal("usage.transforms.notInvoked", d.Code);
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
		Assert.Equal("usage.transforms.notInvoked", d.Code);
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
		// engineVersion is independent of quirksVersion. The V2 diagnostic
		// fires whenever the engine is V2, regardless of quirksVersion.
		var source = "fromAll().when({ $any: function (s, e) { return s; } }).transformBy(function (s) { return s; });";
		using var session = new ProjectionSession(source,
			new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2, QuirksVersion = null });

		Assert.NotNull(session.Diagnostics);
		Assert.Contains(session.Diagnostics!, d => d.Code == "usage.transforms.notInvoked");
	}

	[Fact]
	public void Transforms_TransformBy_TopLevelShadowDoesNotSuppress() {
		// transformBy is a chain method, not a global. A top-level
		// `var transformBy = ...` doesn't change what `.transformBy()`
		// resolves to on the projection runtime, so the diagnostic still
		// fires - distinct from the linkStreamTo/log rules where shadow
		// suppression makes sense.
		var source =
			"var transformBy = function () {};\n" +
			"fromAll().when({ $any: function (s, e) { return s; } }).transformBy(function (s) { return s; });";
		using var session = new ProjectionSession(source,
			new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		Assert.NotNull(session.Diagnostics);
		Assert.Contains(session.Diagnostics!, d => d.Code == "usage.transforms.notInvoked");
	}

	[Fact]
	public void Transforms_TransformBy_MultipleChained_BothReported() {
		// Visitor walks every CallExpression; chained .transformBy(a).transformBy(b)
		// must report twice, not collapse.
		var source =
			"fromAll().when({ $any: function (s, e) { return s; } })" +
			".transformBy(function (s) { return s; })" +
			".transformBy(function (s) { return s; });";
		using var session = new ProjectionSession(source,
			new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		Assert.NotNull(session.Diagnostics);
		var transformDiagnostics = session.Diagnostics!
			.Where(d => d.Code == "usage.transforms.notInvoked").ToArray();
		Assert.Equal(2, transformDiagnostics.Length);
	}

	[Fact]
	public void Transforms_TransformBy_RangeAtPropertyIdentifier() {
		// Range must point at the property identifier (`transformBy`), not
		// the receiver or the whole call - so editor squiggles land on the
		// method name.
		var source = "fromAll().when({ $any: function (s, e) { return s; } }).transformBy(function (s) { return s; });";
		var expectedCol = source.IndexOf(".transformBy", StringComparison.Ordinal) + 2; // +1 to skip dot, +1 for 1-based
		using var session = new ProjectionSession(source,
			new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		Assert.NotNull(session.Diagnostics);
		var d = Assert.Single(session.Diagnostics!);
		Assert.NotNull(d.Range);
		Assert.Equal(1, d.Range!.Start.Line);
		Assert.Equal(expectedCol, d.Range.Start.Column);
		Assert.Equal(expectedCol + "transformBy".Length, d.Range.End.Column);
	}

	[Fact]
	public void Transforms_ComputedMemberAccess_NotDetected() {
		// `obj["transformBy"](fn)` is a computed member access; the rule
		// only matches static `.transformBy(fn)` to avoid false positives
		// where the property name happens to equal a chain method.
		var source =
			"var obj = { transformBy: function (fn) {} };\n" +
			"obj[\"transformBy\"](function (s) { return s; });\n" +
			"fromAll().when({ $any: function (s, e) { return s; } });";
		using var session = new ProjectionSession(source,
			new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		Assert.Null(session.Diagnostics);
	}

	// -- usage.outputState.unconditional (V2) --

	[Fact]
	public void OutputState_InV2_EmitsInformation() {
		var source = "fromAll().when({ $any: function (s, e) { return s; } }).outputState();";
		using var session = new ProjectionSession(source,
			new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		Assert.NotNull(session.Diagnostics);
		var d = Assert.Single(session.Diagnostics!);
		Assert.Equal("usage.outputState.unconditional", d.Code);
		Assert.Equal(DiagnosticSeverity.Information, d.Severity);
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
	public void OutputState_TopLevelShadowDoesNotSuppress() {
		// Same reason as transformBy: outputState is a chain method, so a
		// top-level shadow doesn't change resolution.
		var source =
			"var outputState = function () {};\n" +
			"fromAll().when({ $any: function (s, e) { return s; } }).outputState();";
		using var session = new ProjectionSession(source,
			new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		Assert.NotNull(session.Diagnostics);
		Assert.Contains(session.Diagnostics!, d => d.Code == "usage.outputState.unconditional");
	}

}
