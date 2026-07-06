using Gaffer.Runtime.Errors;
using Gaffer.Runtime.Events;
using Gaffer.Sdk.Diagnostics;
using Gaffer.Sdk.Versioning;

namespace Gaffer.Runtime.Tests;

/// <summary>
/// Tests for upstream quirk-compat behaviours that gaffer reproduces. The
/// <c>event.body</c> cast and non-finite-serialization quirks were fixed upstream
/// by PR #5610 (shipped 26.2.0), so their <see cref="DiagnosticCatalog"/> entries
/// carry <c>FixedIn = 26.2.0</c> and the clean (post-fix) path is reachable and
/// tested at that version. The remaining quirks still have <c>FixedIn = null</c>
/// (no upstream fix), so their quirky path fires in every reachable configuration.
/// </summary>
public class CompatQuirksTests {
	private static ProjectionSessionOptions Options(KurrentDbVersion? quirksVersion = null) =>
		new() { EngineVersion = ProjectionVersion.V2, QuirksVersion = quirksVersion };

	// -- linkStreamTo --

	[Fact]
	public void LinkStreamTo_TwoArgs_EmitsLinkWithoutMetadata() {
		using var session = new ProjectionSession("""
			fromAll().when({
				$any: function (s, e) { linkStreamTo("archive-" + e.streamId, e.streamId); return s; }
			});
		""", Options());
		var emitted = new List<EmittedEvent>();
		session.OnEmit = e => emitted.Add(e);

		session.Feed(new ProjectionEvent { EventType = "X", StreamId = "src-1", Data = "{}" });

		var e = Assert.Single(emitted);
		Assert.Equal("archive-src-1", e.StreamId);
		Assert.Equal("$@", e.EventType);
		Assert.Equal("src-1", e.Data);
		Assert.Null(e.Metadata);
	}

	[Fact]
	public void LinkStreamTo_ThreeArgs_Throws_Unversioned() {
		// Upstream's parameters.At(4) quirk. Always fires (FixedIn = null).
		using var session = new ProjectionSession("""
			fromAll().when({
				$any: function (s, e) { linkStreamTo("archive", e.streamId, { reason: "x" }); return s; }
			});
		""", Options());

		Assert.Throws<ProjectionHandlerException>(() =>
			session.Feed(new ProjectionEvent { EventType = "X", StreamId = "src-1", Data = "{}" }));
	}

	[Fact]
	public void LinkStreamTo_ThreeArgs_Throws_WhenVersioned() {
		// FixedIn = null, so the quirk fires regardless of which version is set.
		using var session = new ProjectionSession("""
			fromAll().when({
				$any: function (s, e) { linkStreamTo("archive", e.streamId, { reason: "x" }); return s; }
			});
		""", Options(new KurrentDbVersion(26, 1, 0)));

		Assert.Throws<ProjectionHandlerException>(() =>
			session.Feed(new ProjectionEvent { EventType = "X", StreamId = "src-1", Data = "{}" }));
	}

	[Fact]
	public void LinkStreamTo_FourArgs_EmitsWithoutMetadata() {
		// Upstream's metadata branch is gated on Length == 3 specifically;
		// 4+ args skip the block entirely so no metadata is captured. We
		// match.
		using var session = new ProjectionSession("""
			fromAll().when({
				$any: function (s, e) { linkStreamTo("archive", e.streamId, { reason: "x" }, "extra"); return s; }
			});
		""", Options());
		var emitted = new List<EmittedEvent>();
		session.OnEmit = e => emitted.Add(e);

		session.Feed(new ProjectionEvent { EventType = "X", StreamId = "src-1", Data = "{}" });

		var e = Assert.Single(emitted);
		Assert.Null(e.Metadata);
	}

	[Fact]
	public void LinkStreamTo_ThreeArgs_ExceptionCarriesCompatCode() {
		using var session = new ProjectionSession("""
			fromAll().when({
				$any: function (s, e) { linkStreamTo("archive", e.streamId, { reason: "x" }); return s; }
			});
		""", Options());

		var ex = Assert.Throws<ProjectionHandlerException>(() =>
			session.Feed(new ProjectionEvent { EventType = "X", StreamId = "src-1", Data = "{}" }));
		Assert.Equal(DiagnosticCatalog.LinkStreamToOutOfBoundsParameters.Code, ex.CompatCode);
		// Quirks-always-diagnose: the throwing quirk also reaches the diagnostics channel.
		var d = Assert.Single(ex.Diagnostics, x => x.Code == DiagnosticCatalog.LinkStreamToOutOfBoundsParameters.Code);
		Assert.Equal(DiagnosticSeverity.Error, d.Severity);
	}

	// -- log multi-param --

	[Fact]
	public void Log_SinglePrimitive_OneLine() {
		using var session = new ProjectionSession("""
			fromAll().when({
				$any: function (s, e) { log("hello"); return s; }
			});
		""", Options());
		var logs = new List<string>();
		session.OnLog = m => logs.Add(m);

		session.Feed(new ProjectionEvent { EventType = "X", StreamId = "src-1", Data = "{}" });

		Assert.Single(logs);
		Assert.Equal("hello", logs[0]);
	}

	[Fact]
	public void Log_MultipleParams_EmitsPrimitivesAsSeparateLines() {
		// Upstream multi-param quirk: each primitive logs as its own line, then
		// a final accumulated buffer (empty for all-primitives input - the
		// separator path only adds content when objects are present).
		using var session = new ProjectionSession("""
			fromAll().when({
				$any: function (s, e) { log("a", "b", "c"); return s; }
			});
		""", Options());
		var logs = new List<string>();
		session.OnLog = m => logs.Add(m);

		session.Feed(new ProjectionEvent { EventType = "X", StreamId = "src-1", Data = "{}" });

		// Three primitives -> three separate log lines, plus the final
		// buffer line which is " ," (only the i>1 separator was appended;
		// primitives don't append to the buffer).
		Assert.Equal(new[] { "a", "b", "c", " ," }, logs);
	}

	[Fact]
	public void Log_AllObjects_FirstObjectHasNoLeadingSeparator() {
		// Headline demonstration of the i>1 separator quirk: with three
		// objects, only the third gets a separator prepended.
		using var session = new ProjectionSession("""
			fromAll().when({
				$any: function (s, e) { log({ a: 1 }, { b: 2 }, { c: 3 }); return s; }
			});
		""", Options());
		var logs = new List<string>();
		session.OnLog = m => logs.Add(m);

		session.Feed(new ProjectionEvent { EventType = "X", StreamId = "src-1", Data = "{}" });

		// Three objects accumulate into the buffer with the i>1 quirk: i=0
		// no separator, i=1 no separator, i=2 prepends " ,".
		Assert.Equal(new[] { "{\"a\":1}{\"b\":2} ,{\"c\":3}" }, logs);
	}

	// -- EnsureBody (event.body cast) --

	[Fact]
	public void EventBody_ObjectData_Works() {
		// Object event bodies work in both quirky and clean paths - the cast
		// succeeds because the parsed JSON is an object.
		using var session = new ProjectionSession("""
			fromAll().when({
				$init: function() { return { name: null }; },
				Test: function(s, e) { s.name = e.body.name; return s; }
			});
		""", Options());

		session.Feed(new ProjectionEvent {
			EventType = "Test",
			StreamId = "s-1",
			Data = """{"name":"alice"}""",
			IsJson = true,
		});

		Assert.Contains("alice", session.GetState());
	}

	[Theory]
	[InlineData("null")]
	[InlineData("42")]
	[InlineData("\"hello\"")]
	[InlineData("true")]
	public void EventBody_NonObjectData_Throws_Unversioned(string data) {
		// Upstream's EnsureBody casts the parsed body to ObjectInstance.
		// Non-object JSON values (null, number, string, boolean) throw
		// InvalidCastException. Fired here at the unversioned default;
		// suppressed at >= 26.2.0 (see EventBody_NonObjectData_Works_At26_2_0).
		using var session = new ProjectionSession("""
			fromAll().when({
				Test: function(s, e) { return e.body; }
			});
		""", Options());

		Assert.Throws<ProjectionHandlerException>(() =>
			session.Feed(new ProjectionEvent {
				EventType = "Test",
				StreamId = "s-1",
				Data = data,
				IsJson = true,
			}));
	}

	[Fact]
	public void EventBody_NonObjectData_ExceptionCarriesCompatCode() {
		using var session = new ProjectionSession("""
			fromAll().when({
				Test: function(s, e) { return e.body; }
			});
		""", Options());

		var ex = Assert.Throws<ProjectionHandlerException>(() =>
			session.Feed(new ProjectionEvent {
				EventType = "Test",
				StreamId = "s-1",
				Data = "null",
				IsJson = true,
			}));
		Assert.Equal(DiagnosticCatalog.EventBodyCast.Code, ex.CompatCode);
		var d = Assert.Single(ex.Diagnostics, x => x.Code == DiagnosticCatalog.EventBodyCast.Code);
		Assert.Equal(DiagnosticSeverity.Error, d.Severity);
	}

	[Theory]
	[InlineData("null")]
	[InlineData("42")]
	[InlineData("\"hello\"")]
	[InlineData("true")]
	public void EventBody_NonObjectData_Works_At26_2_0(string data) {
		// PR #5610 (26.2.0) drops the ObjectInstance cast in EnsureBody, so a
		// non-object body is accessible instead of throwing. FixedIn=26.2.0
		// activates the clean path.
		using var session = new ProjectionSession("""
			fromAll().when({
				Test: function (s, e) { return { got: e.body }; }
			});
		""", Options(new KurrentDbVersion(26, 2, 0)));

		var result = session.Feed(new ProjectionEvent {
			EventType = "Test",
			StreamId = "s-1",
			Data = data,
			IsJson = true,
		});

		Assert.NotNull(session.GetState());
		Assert.Empty(result.Diagnostics); // at >= FixedIn the quirk neither throws nor emits
	}

	[Fact]
	public void ThrowingQuirk_CarriesEarlierNonThrowingQuirkDiagnostics() {
		// A non-throwing quirk (log multi-param) fires, then the same event throws (body cast).
		// Both must reach the error's diagnostics - the throw doesn't discard what fired first.
		using var session = new ProjectionSession("""
			fromAll().when({
				Test: function (s, e) { log("a", "b"); return e.body; }
			});
		""", Options());

		var ex = Assert.Throws<ProjectionHandlerException>(() =>
			session.Feed(new ProjectionEvent {
				EventType = "Test",
				StreamId = "s-1",
				Data = "null",
				IsJson = true,
			}));

		// Exactly the two that fired plus the V2 wedge rider, no duplication.
		Assert.Equal(3, ex.Diagnostics.Count);
		Assert.Contains(ex.Diagnostics, x => x.Code == DiagnosticCatalog.LogMultiParam.Code);
		Assert.Contains(ex.Diagnostics, x => x.Code == DiagnosticCatalog.EventBodyCast.Code);
		Assert.Contains(ex.Diagnostics, x => x.Code == DiagnosticCatalog.HandlerErrorWedgesOnV2.Code);
	}

	[Fact]
	public void ThrowingQuirk_ViaGetResult_CarriesDiagnostic() {
		// A quirk that throws from the transform (V1 serializes transform output) reaches the
		// error's diagnostics even though it never goes through Feed's catch. SetState seeds a
		// partition so GetResult runs the transform without a preceding Feed.
		using var session = new ProjectionSession("""
			fromAll().when({
				$init: function () { return {}; },
				Test: function (s, e) { return s; }
			}).transformBy(function (s) { return { v: NaN }; }).outputState()
			""", new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V1 });
		session.SetState(null, "{}");

		var ex = Assert.Throws<ProjectionTransformException>(() => session.GetResult());
		Assert.Equal(DiagnosticCatalog.SerializeNonFinite.Code, ex.CompatCode);
		var d = Assert.Single(ex.Diagnostics, x => x.Code == DiagnosticCatalog.SerializeNonFinite.Code);
		Assert.Equal(DiagnosticSeverity.Error, d.Severity);
	}

	// -- BiState PrepareOutput string slot --

	[Fact]
	public void BiState_StringSlot_JsonEncoded() {
		// Bi-state slots always JSON-encode (matches upstream). A string slot
		// persists as "alice" with quotes - the correct contract, not a quirk.
		using var session = new ProjectionSession("""
			options({ biState: true });
			fromAll().when({
				$init: function () { return "initial"; },
				$initShared: function () { return {}; },
				SetName: function (s, e) { s[0] = e.data.name; return s; }
			});
		""", Options());

		session.Feed(new ProjectionEvent {
			EventType = "SetName",
			StreamId = "s-1",
			Data = """{"name":"alice"}""",
			IsJson = true,
		});

		Assert.Equal("\"alice\"", session.GetState());
	}

	[Fact]
	public void BiState_StringSlot_EmitsNoDiagnostic() {
		using var session = new ProjectionSession("""
			options({ biState: true });
			fromAll().when({
				$init: function () { return "initial"; },
				$initShared: function () { return {}; },
				SetName: function (s, e) { s[0] = e.data.name; return s; }
			});
		""", Options());

		var result = session.Feed(new ProjectionEvent {
			EventType = "SetName",
			StreamId = "s-1",
			Data = """{"name":"alice"}""",
			IsJson = true,
		});

		// A string in a bi-state slot is JSON-encoded (correct), not a quirk - no diagnostic.
		Assert.Empty(result.Diagnostics);
	}

	[Fact]
	public void BiState_ObjectSlots_EmitNoRuntimeDiagnostic() {
		using var session = new ProjectionSession("""
			options({ biState: true });
			fromAll().when({
				$init: function () { return {}; },
				$initShared: function () { return {}; },
				SetName: function (s, e) { s[0] = { name: e.data.name }; return s; }
			});
		""", Options());

		var result = session.Feed(new ProjectionEvent {
			EventType = "SetName",
			StreamId = "s-1",
			Data = """{"name":"alice"}""",
			IsJson = true,
		});

		Assert.Empty(result.Diagnostics);
	}

	[Fact]
	public void BiState_OnV2_EmitsCompileDiagnostic() {
		// Bi-state shared state isn't restored on restart under V2
		// (quirk.biState.sharedStateResetOnV2), detected off the resolved definition.
		using var session = new ProjectionSession("""
			options({ biState: true });
			fromAll().when({
				$init: function () { return {}; },
				$initShared: function () { return {}; },
				Set: function (s, e) { return s; }
			});
		""", new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V2 });

		Assert.Contains(session.Diagnostics ?? [], d => d.Code == DiagnosticCatalog.BiStateSharedStateResetOnV2.Code);
	}

	[Fact]
	public void BiState_OnV1_NoSharedStateResetDiagnostic() {
		// V1 supports shared state, so the V2-only quirk must not fire.
		using var session = new ProjectionSession("""
			options({ biState: true });
			fromAll().when({
				$init: function () { return {}; },
				$initShared: function () { return {}; },
				Set: function (s, e) { return s; }
			});
		""", new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V1 });

		Assert.DoesNotContain(session.Diagnostics ?? [], d => d.Code == DiagnosticCatalog.BiStateSharedStateResetOnV2.Code);
	}

	[Fact]
	public void UniStateString_EmitsDiagnostic_Unversioned() {
		// A bare string returned as state would be persisted un-encoded pre-26.2.0
		// (quirk.serialize.rawString), faulting on reload. gaffer JSON-encodes the state
		// (safe) and reports the quirk.
		using var session = new ProjectionSession("""
			fromAll().when({
				Set: function (s, e) { return e.data.name; }
			});
		""", Options());

		var result = session.Feed(new ProjectionEvent {
			EventType = "Set",
			StreamId = "s-1",
			Data = """{"name":"alice"}""",
			IsJson = true,
		});

		Assert.Equal("\"alice\"", session.GetState());
		var diag = Assert.Single(result.Diagnostics);
		Assert.Equal(DiagnosticCatalog.SerializeRawString.Code, diag.Code);
		Assert.Equal(DiagnosticSeverity.Error, diag.Severity);
	}

	[Fact]
	public void UniStateString_JsonEncoded_At26_2_0() {
		// PR #5610 (26.2.0) JSON-encodes string state so it round-trips; no quirk fires.
		using var session = new ProjectionSession("""
			fromAll().when({
				Set: function (s, e) { return e.data.name; }
			});
		""", Options(new KurrentDbVersion(26, 2, 0)));

		var result = session.Feed(new ProjectionEvent {
			EventType = "Set",
			StreamId = "s-1",
			Data = """{"name":"alice"}""",
			IsJson = true,
		});

		Assert.Equal("\"alice\"", session.GetState());
		Assert.Empty(result.Diagnostics);
	}

	[Fact]
	public void LogMultiParam_EmitsRuntimeDiagnostic() {
		// A multi-arg log() trips quirk.log.multiParam at the point it runs,
		// surfaced on the feed result (also a compile-time diagnostic).
		using var session = new ProjectionSession("""
			fromAll().when({
				$any: function (s, e) { log("a", "b"); return s; }
			});
		""", Options());

		var result = session.Feed(new ProjectionEvent {
			EventType = "Test",
			StreamId = "s-1",
			Data = "{}",
			IsJson = true,
		});

		Assert.Contains(result.Diagnostics, d => d.Code == DiagnosticCatalog.LogMultiParam.Code);
	}

	[Fact]
	public void OnDiagnostic_StreamsAtPointOfFiring() {
		// The streaming callback fires live during feed, not just on the result.
		var streamed = new List<string>();
		using var session = new ProjectionSession("""
			fromAll().when({
				$any: function (s, e) { log("a", "b"); return s; }
			});
		""", Options()) { OnDiagnostic = d => streamed.Add(d.Code) };

		session.Feed(new ProjectionEvent {
			EventType = "Test",
			StreamId = "s-1",
			Data = "{}",
			IsJson = true,
		});

		Assert.Contains(DiagnosticCatalog.LogMultiParam.Code, streamed);
	}

	[Fact]
	public void StateContainingNaN_ExceptionCarriesCompatCode() {
		using var session = new ProjectionSession("""
			fromAll().when({
				$init: function () { return { value: 0 }; },
				Test: function (s, e) { s.value = NaN; return s; }
			});
		""", Options());

		var ex = Assert.Throws<StateSerializationException>(() =>
			session.Feed(new ProjectionEvent {
				EventType = "Test",
				StreamId = "s-1",
				Data = "{}",
				IsJson = true,
			}));
		Assert.Equal(DiagnosticCatalog.SerializeNonFinite.Code, ex.CompatCode);
		var d = Assert.Single(ex.Diagnostics, x => x.Code == DiagnosticCatalog.SerializeNonFinite.Code);
		Assert.Equal(DiagnosticSeverity.Error, d.Severity);
	}

	// -- SerializePrimitive NaN/Infinity --

	[Theory]
	[InlineData("NaN")]
	[InlineData("Infinity")]
	[InlineData("-Infinity")]
	public void StateContainingNonFinite_Throws_Unversioned(string jsLiteral) {
		// Upstream's Utf8JsonWriter.WriteNumberValue throws on non-finite
		// doubles. Fired here at the unversioned default; the clean path
		// (>= 26.2.0) writes JSON null - see StateContainingNonFinite_WritesNull_At26_2_0.
		using var session = new ProjectionSession($$"""
			fromAll().when({
				$init: function () { return { value: 0 }; },
				Test: function (s, e) { s.value = {{jsLiteral}}; return s; }
			});
		""", Options());

		Assert.Throws<StateSerializationException>(() =>
			session.Feed(new ProjectionEvent {
				EventType = "Test",
				StreamId = "s-1",
				Data = "{}",
				IsJson = true,
			}));
	}

	[Theory]
	[InlineData("NaN")]
	[InlineData("Infinity")]
	[InlineData("-Infinity")]
	public void StateContainingNonFinite_WritesNull_At26_2_0(string jsLiteral) {
		// PR #5610 (26.2.0) serializes non-finite numbers as JSON null instead of
		// throwing. FixedIn=26.2.0 activates the clean path.
		using var session = new ProjectionSession($$"""
			fromAll().when({
				$init: function () { return { value: 0 }; },
				Test: function (s, e) { s.value = {{jsLiteral}}; return s; }
			});
		""", Options(new KurrentDbVersion(26, 2, 0)));

		var result = session.Feed(new ProjectionEvent {
			EventType = "Test",
			StreamId = "s-1",
			Data = "{}",
			IsJson = true,
		});

		Assert.Contains("\"value\":null", session.GetState());
		Assert.Empty(result.Diagnostics); // at >= FixedIn the quirk neither throws nor emits
	}

	[Fact]
	public void Log_MixedPrimitiveAndObjects_PrimitivesEmit_ObjectsAccumulate() {
		// Mixed input shows the quirk clearly: primitives emit immediately,
		// objects accumulate into the buffer with " ," separator (gated on
		// i>1, so the first object has no leading separator).
		using var session = new ProjectionSession("""
			fromAll().when({
				$any: function (s, e) { log({ a: 1 }, "b", { c: 2 }); return s; }
			});
		""", Options());
		var logs = new List<string>();
		session.OnLog = m => logs.Add(m);

		session.Feed(new ProjectionEvent { EventType = "X", StreamId = "src-1", Data = "{}" });

		// "b" emitted at i=1; objects at i=0 and i=2 go to buffer; i=2's
		// separator is " ," (i>1 is true).
		Assert.Equal(new[] { "b", "{\"a\":1} ,{\"c\":2}" }, logs);
	}

	// -- handler-error wedge (DB-2159) --
	// The deployed V2 engine never faults on a handler error - the projection wedges
	// silently. gaffer keeps faulting locally and rides this diagnostic on the error.

	[Fact]
	public void HandlerError_OnV2_CarriesWedgeDiagnostic() {
		using var session = new ProjectionSession("""
			fromAll().when({
				Boom: function (s, e) { throw new Error("intentional"); }
			});
		""", Options());
		var live = new List<Diagnostic>();
		session.OnDiagnostic = d => live.Add(d);

		var ex = Assert.Throws<ProjectionHandlerException>(() =>
			session.Feed(new ProjectionEvent { EventType = "Boom", StreamId = "s-1", Data = "{}" }));

		// Not the throw's cause, so it must not claim CompatCode - it rides Diagnostics only.
		Assert.Null(ex.CompatCode);
		var d = Assert.Single(ex.Diagnostics, x => x.Code == DiagnosticCatalog.HandlerErrorWedgesOnV2.Code);
		Assert.Equal(DiagnosticSeverity.Error, d.Severity);
		// Streamed live at the point of firing too, like every other quirk.
		Assert.Contains(live, x => x.Code == DiagnosticCatalog.HandlerErrorWedgesOnV2.Code);
	}

	[Fact]
	public void HandlerError_OnV2_Versioned_StillFires() {
		// FixedIn = null, so the wedge fires regardless of which version is set.
		using var session = new ProjectionSession("""
			fromAll().when({
				Boom: function (s, e) { throw new Error("intentional"); }
			});
		""", Options(new KurrentDbVersion(26, 2, 0)));

		var ex = Assert.Throws<ProjectionHandlerException>(() =>
			session.Feed(new ProjectionEvent { EventType = "Boom", StreamId = "s-1", Data = "{}" }));

		Assert.Single(ex.Diagnostics, x => x.Code == DiagnosticCatalog.HandlerErrorWedgesOnV2.Code);
	}

	[Fact]
	public void HandlerError_OnV1_NoWedgeDiagnostic() {
		// V1 faults properly on the server, so there is no divergence to explain.
		using var session = new ProjectionSession("""
			fromAll().when({
				Boom: function (s, e) { throw new Error("intentional"); }
			});
		""", new ProjectionSessionOptions { EngineVersion = ProjectionVersion.V1 });

		var ex = Assert.Throws<ProjectionHandlerException>(() =>
			session.Feed(new ProjectionEvent { EventType = "Boom", StreamId = "s-1", Data = "{}" }));

		Assert.Empty(ex.Diagnostics);
	}

	[Fact]
	public void PartitionByThrow_OnV2_NoWedgeDiagnostic() {
		// Partition keys are computed on the server's read loop, whose exceptions fault
		// the projection properly - only partition-processor work wedges.
		using var session = new ProjectionSession("""
			fromAll().partitionBy(function (e) { throw new Error("intentional"); }).when({
				$any: function (s, e) { return s; }
			});
		""", Options());

		var ex = Assert.Throws<ProjectionHandlerException>(() =>
			session.Feed(new ProjectionEvent { EventType = "X", StreamId = "s-1", Data = "{}" }));

		Assert.Empty(ex.Diagnostics);
	}

	[Fact]
	public void ThrowingDiagnosticSubscriber_DoesNotMaskProjectionError() {
		// OnDiagnostic invocations that happen while a projection error is unwinding
		// (the wedge rider and the thrown-quirk diagnostic) must not let a throwing
		// subscriber replace that error. The body-cast projection exercises both
		// sites in one feed; the diagnostics still arrive on the exception.
		using var session = new ProjectionSession("""
			fromAll().when({
				Test: function (s, e) { return e.body; }
			});
		""", Options());
		session.OnDiagnostic = _ => throw new InvalidOperationException("subscriber boom");

		var ex = Assert.Throws<ProjectionHandlerException>(() =>
			session.Feed(new ProjectionEvent {
				EventType = "Test",
				StreamId = "s-1",
				Data = "null",
				IsJson = true,
			}));

		Assert.Equal(DiagnosticCatalog.EventBodyCast.Code, ex.CompatCode);
		Assert.Contains(ex.Diagnostics, x => x.Code == DiagnosticCatalog.EventBodyCast.Code);
		Assert.Contains(ex.Diagnostics, x => x.Code == DiagnosticCatalog.HandlerErrorWedgesOnV2.Code);
	}

	[Fact]
	public void DeletedHandlerThrow_OnV2_CarriesWedgeDiagnostic() {
		// $deleted handling is partition-processor work on the server, so the
		// FeedStreamDeleted throw sites must ride the wedge diagnostic too.
		using var session = new ProjectionSession("""
			fromAll().foreachStream().when({
				$init: function() { return {}; },
				$deleted: function(s, e) { throw new Error("deleted boom"); },
				Ping: function(s, e) { return s; }
			});
		""", Options());

		session.Feed(new ProjectionEvent { EventType = "Ping", StreamId = "s-1", Data = "{}" });

		var ex = Assert.Throws<ProjectionHandlerException>(() =>
			session.Feed(new ProjectionEvent {
				EventType = ProjectionSession.StreamDeletedEventType,
				StreamId = "s-1",
				Data = "{}",
			}));

		Assert.Single(ex.Diagnostics, x => x.Code == DiagnosticCatalog.HandlerErrorWedgesOnV2.Code);
	}

	[Fact]
	public void StateSerializationError_OnV2_CarriesWedgeDiagnostic() {
		// Output serialization is partition-processor work on the server, so it wedges too.
		// NaN state throws (SerializeNonFinite, unfixed below 26.2.0); the wedge rides along.
		using var session = new ProjectionSession("""
			fromAll().when({
				Test: function (s, e) { return { v: NaN }; }
			});
		""", Options(new KurrentDbVersion(26, 1, 0)));

		var ex = Assert.Throws<StateSerializationException>(() =>
			session.Feed(new ProjectionEvent { EventType = "Test", StreamId = "s-1", Data = "{}" }));

		Assert.Equal(DiagnosticCatalog.SerializeNonFinite.Code, ex.CompatCode);
		Assert.Single(ex.Diagnostics, x => x.Code == DiagnosticCatalog.HandlerErrorWedgesOnV2.Code);
	}
}
