using Acornima;
using Gaffer.Runtime.Projection;

namespace Gaffer.Runtime.Tests;

/// <summary>
/// Unit tests for <see cref="EmitDetector"/>. Invoked against pre-parsed AST
/// nodes (skipping the FFI / ProjectionSession surface) so each test pins one
/// detection concern in isolation.
/// </summary>
public class EmitDetectorTests {
	private static bool Detect(string source) =>
		EmitDetector.Detect(new Parser().ParseScript(source, "projection.js"));

	[Theory]
	[InlineData("emit('s', 'E', {});")]
	[InlineData("linkTo('s', e);")]
	[InlineData("linkStreamTo('s', e);")]
	[InlineData("copyTo('s', e);")]
	public void WriteSink_Detected(string source) {
		Assert.True(Detect(source));
	}

	[Fact]
	public void DetectsSinkNestedInHandler() {
		const string source = """
			fromAll().when({
				$any: function (s, e) { emit('out', 'E', { v: e.data }); return s; }
			});
			""";
		Assert.True(Detect(source));
	}

	[Fact]
	public void ReadOnlyProjection_NotDetected() {
		const string source = """
			fromAll().when({
				$init: function () { return { n: 0 }; },
				$any: function (s, e) { s.n++; return s; }
			});
			""";
		Assert.False(Detect(source));
	}

	[Fact]
	public void EmptyScript_NotDetected() {
		Assert.False(Detect(""));
	}

	// Identifier-only dispatch: a member call that happens to be named `emit`
	// is user code, not a projection write sink, so it must not count. Mirrors
	// ShapeCollector's caller-position rule.
	[Fact]
	public void MemberCallNamedEmit_NotDetected() {
		Assert.False(Detect("someService.emit('x');"));
	}

	// Computed access (`obj["emit"]()`) is a MemberExpression callee, not an
	// Identifier, so it must not count - locks the identifier-only intent.
	[Fact]
	public void ComputedMemberCall_NotDetected() {
		Assert.False(Detect("var o = {}; o['emit']('s', 'E', {});"));
	}

	// The short-circuit must not skip a sink: a sink after a non-sink call, and
	// a second sink after the first, both still resolve to true.
	[Fact]
	public void SinkAfterNonSink_Detected() {
		Assert.True(Detect("doSomething(); emit('s', 'E', {});"));
	}

	[Fact]
	public void MultipleSinks_Detected() {
		Assert.True(Detect("emit('a', 'E', {}); linkTo('b', e);"));
	}

	// A sink nested in another call's arguments is reached by descending into
	// the outer (non-sink) call - guards the base-visitor recursion.
	[Fact]
	public void SinkAsCallArgument_Detected() {
		Assert.True(Detect("wrap(emit('s', 'E', {}));"));
	}
}
