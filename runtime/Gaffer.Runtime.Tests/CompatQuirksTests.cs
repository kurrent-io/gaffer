using Gaffer.Runtime.Errors;
using Gaffer.Runtime.Events;
using Gaffer.Sdk.Diagnostics;
using Gaffer.Sdk.Versioning;

namespace Gaffer.Runtime.Tests;

/// <summary>
/// Tests for upstream quirk-compat behaviours that gaffer reproduces. Each
/// quirk's <see cref="DiagnosticCatalog"/> entry currently has <c>FixedIn = null</c>
/// (no upstream PR has merged), so the quirky path fires in every reachable
/// configuration. The "clean" branch is intentionally unreachable today; it
/// activates when an upstream fix lands and we flip <c>FixedIn</c> to the
/// release version.
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
		// InvalidCastException. Quirk always fires while FixedIn = null.
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

		Assert.Equal(2, ex.Diagnostics.Count); // exactly the two that fired, no duplication
		Assert.Contains(ex.Diagnostics, x => x.Code == DiagnosticCatalog.LogMultiParam.Code);
		Assert.Contains(ex.Diagnostics, x => x.Code == DiagnosticCatalog.EventBodyCast.Code);
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
	public void BiState_StringInSlot0_QuotedWhenQuirky() {
		// Upstream checks _state.IsString() (the array, always false) instead
		// of state.IsString() (the slot-0 element). Every value goes through
		// the JSON-serializer, so raw strings come out quoted.
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

		// Quirky: JSON-quoted (matches upstream). Clean would emit raw "alice".
		Assert.Equal("\"alice\"", session.GetState());
	}

	[Fact]
	public void BiState_StringInSlot0_EmitsRuntimeDiagnostic() {
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

		var diag = Assert.Single(result.Diagnostics);
		Assert.Equal(DiagnosticCatalog.BiStateStringSlot.Code, diag.Code);
		Assert.Equal(DiagnosticSeverity.Warning, diag.Severity);
		Assert.Null(diag.Range);
	}

	[Fact]
	public void BiState_StringInSharedSlot_EmitsSharedRuntimeDiagnostic() {
		using var session = new ProjectionSession("""
			options({ biState: true });
			fromAll().when({
				$init: function () { return {}; },
				$initShared: function () { return "initial"; },
				SetShared: function (s, e) { s[1] = e.data.name; return s; }
			});
		""", Options());

		var result = session.Feed(new ProjectionEvent {
			EventType = "SetShared",
			StreamId = "s-1",
			Data = """{"name":"alice"}""",
			IsJson = true,
		});

		Assert.Contains(result.Diagnostics, d => d.Code == DiagnosticCatalog.BiStateSharedStringSlot.Code);
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
	public void LogMultiParam_EmitsRuntimeDiagnostic() {
		// A multi-arg log() trips compat.log.multiParam at the point it runs,
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
		// doubles. Quirk always fires while FixedIn = null. Clean path writes
		// JSON null instead.
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
}
