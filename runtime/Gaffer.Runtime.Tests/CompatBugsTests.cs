using Gaffer.Runtime.Errors;
using Gaffer.Runtime.Events;
using Gaffer.Sdk.Versioning;

namespace Gaffer.Runtime.Tests;

/// <summary>
/// Tests for upstream bug-compat behaviours that gaffer reproduces. Each
/// bug's <see cref="KnownBugs"/> entry currently has <c>FixedIn = null</c>
/// (no upstream PR has merged), so the buggy path fires in every reachable
/// configuration. The "clean" branch is intentionally unreachable today; it
/// activates when an upstream fix lands and we flip <c>FixedIn</c> to the
/// release version.
/// </summary>
public class CompatBugsTests {
	private static ProjectionSessionOptions Options(KurrentDbVersion? dbVersion = null) =>
		new() { EngineVersion = ProjectionVersion.V2, DbVersion = dbVersion };

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
		// Upstream's parameters.At(4) bug. Always fires (FixedIn = null).
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
		// FixedIn = null, so the bug fires regardless of which version is set.
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
		// Upstream multi-param bug: each primitive logs as its own line, then
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

	[Fact]
	public void Log_MixedPrimitiveAndObjects_PrimitivesEmit_ObjectsAccumulate() {
		// Mixed input shows the bug clearly: primitives emit immediately,
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
