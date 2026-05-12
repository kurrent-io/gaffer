using System.Reflection;
using Acornima;
using Gaffer.Runtime.Projection;
using Gaffer.Sdk;

namespace Gaffer.Runtime.Tests;

/// <summary>
/// Unit tests for <see cref="ShapeCollector"/>. The walker is invoked
/// against pre-parsed AST nodes (skipping the FFI / ProjectionSession
/// surface) so each test pins one structural concern in isolation.
/// </summary>
public class ShapeCollectorTests {
	private static ProjectionShape Walk(string source, int fileSize = 0) {
		var ast = new Parser().ParseScript(source, "projection.js");
		return ShapeCollector.Walk(ast, fileSize, parsable: true);
	}

	[Fact]
	public void EmptyScript_ProducesAllZeroCounts() {
		var shape = Walk("");

		Assert.True(shape.Parsable);
		Assert.False(shape.Handlers.Any);
		Assert.False(shape.Handlers.Init);
		Assert.False(shape.Handlers.Deleted);
		Assert.Equal(0, shape.Handlers.DistinctEventNames);
		Assert.Null(shape.BuiltinCounts.FromAll);
		Assert.Null(shape.BuiltinCounts.When);
	}

	[Fact]
	public void TopLevelBuiltins_Counted() {
		var shape = Walk("fromAll(); fromStream('s'); fromStream('s2'); fromCategory('c');");

		Assert.Equal(1, shape.BuiltinCounts.FromAll);
		Assert.Equal(2, shape.BuiltinCounts.FromStream);
		Assert.Equal(1, shape.BuiltinCounts.FromCategory);
		Assert.Null(shape.BuiltinCounts.FromStreams);
	}

	[Fact]
	public void ChainedBuiltins_Counted() {
		var shape = Walk(
			"fromAll()" +
			"  .when({ A: function(s, e) { return s; } })" +
			"  .foreachStream()" +
			"  .outputState();");

		Assert.Equal(1, shape.BuiltinCounts.FromAll);
		Assert.Equal(1, shape.BuiltinCounts.When);
		Assert.Equal(1, shape.BuiltinCounts.ForeachStream);
		Assert.Equal(1, shape.BuiltinCounts.OutputState);
	}

	[Fact]
	public void RepeatedBuiltin_Accumulates() {
		var shape = Walk(
			"fromAll().transformBy(function(s) { return s; }).transformBy(function(s) { return s; });");

		Assert.Equal(2, shape.BuiltinCounts.TransformBy);
	}

	[Fact]
	public void AnyHandler_Detected() {
		var shape = Walk("fromAll().when({ $any: function(s, e) { return s; } });");

		Assert.True(shape.Handlers.Any);
		Assert.False(shape.Handlers.Init);
		Assert.False(shape.Handlers.Deleted);
		Assert.Equal(0, shape.Handlers.DistinctEventNames);
	}

	[Fact]
	public void InitAndInitSharedBothMapToInit() {
		var initShape = Walk("fromAll().when({ $init: function() { return {}; } });");
		var sharedShape = Walk("fromAll().when({ $initShared: function() { return {}; } });");

		Assert.True(initShape.Handlers.Init);
		Assert.True(sharedShape.Handlers.Init);
	}

	[Fact]
	public void DeletedHandler_Detected() {
		var shape = Walk("fromAll().when({ $deleted: function(s, e) { return s; } });");

		Assert.True(shape.Handlers.Deleted);
	}

	[Fact]
	public void DistinctEventNames_CountedDeduplicated() {
		var shape = Walk(
			"fromAll().when({\n" +
			"  Created: function(s, e) { return s; },\n" +
			"  Updated: function(s, e) { return s; },\n" +
			"  Deleted: function(s, e) { return s; },\n" +
			"});\n" +
			"fromAll().when({\n" +
			"  Created: function(s, e) { return s; },\n" + // dup across blocks
			"  Renamed: function(s, e) { return s; },\n" +
			"});");

		// 4 distinct: Created, Updated, Deleted, Renamed.
		Assert.Equal(4, shape.Handlers.DistinctEventNames);
	}

	[Fact]
	public void StringLiteralKeys_TreatedAsEventNames() {
		// `"$any": fn` is the same as `$any: fn` per JS spec; pin
		// the parser/walker handles both forms.
		var shape = Walk("fromAll().when({ \"$any\": function(s, e) { return s; } });");

		Assert.True(shape.Handlers.Any);
	}

	[Fact]
	public void ComputedKeys_Ignored() {
		// `[expr]: fn` can't be statically classified - dropping it
		// is safer than guessing. Walker must NOT crash on it.
		var shape = Walk(
			"var key = 'Foo';\n" +
			"fromAll().when({ [key]: function(s, e) { return s; } });");

		Assert.Equal(0, shape.Handlers.DistinctEventNames);
	}

	[Fact]
	public void DollarPrefixedStrangerKeys_Ignored() {
		// Unknown $-prefixed keys (future spec extensions) don't get
		// folded into DistinctEventNames - that count must reflect
		// real domain events the user named.
		var shape = Walk(
			"fromAll().when({\n" +
			"  $unknown: function(s, e) { return s; },\n" +
			"  Order: function(s, e) { return s; },\n" +
			"});");

		Assert.Equal(1, shape.Handlers.DistinctEventNames);
	}

	[Fact]
	public void ChainHandlersBlock_AlsoHarvested() {
		// chainHandlers is the v2 composition form. Same handler-key
		// semantics inside, so the walker harvests from both `when`
		// and `chainHandlers` argument shapes.
		var shape = Walk(
			"fromAll().chainHandlers({\n" +
			"  $any: function(s, e) { return s; },\n" +
			"  Order: function(s, e) { return s; },\n" +
			"});");

		Assert.True(shape.Handlers.Any);
		Assert.Equal(1, shape.Handlers.DistinctEventNames);
		Assert.Equal(1, shape.BuiltinCounts.ChainHandlers);
	}

	[Fact]
	public void ComputedMemberAccess_NotCounted() {
		// `obj["fromAll"]()` should not match - we only walk
		// non-computed property accesses for chained builtins.
		var shape = Walk("var obj = {}; obj[\"fromAll\"]();");

		Assert.Null(shape.BuiltinCounts.FromAll);
	}

	[Fact]
	public void FileSizeAndParsable_PassedThrough() {
		var ast = new Parser().ParseScript("fromAll();", "projection.js");
		var shape = ShapeCollector.Walk(ast, fileSize: 1234, parsable: true);

		Assert.True(shape.Parsable);
		Assert.Equal(1234, shape.FileSize);

		var partial = ShapeCollector.Walk(ast, fileSize: 0, parsable: false);
		Assert.False(partial.Parsable);
	}

	[Fact]
	public void LinkStreamToCount_Tracked() {
		// Deprecated but tracked so we can size the impact of
		// retiring it - usage analytics drive the deprecation
		// timeline.
		var shape = Walk(
			"fromAll().when({ $any: function(s, e) { linkStreamTo('a', e.streamId); return s; } });");

		Assert.Equal(1, shape.BuiltinCounts.LinkStreamTo);
	}

	[Fact]
	public void NonBuiltinCalls_Ignored() {
		// Calls to arbitrary user functions shouldn't appear in
		// any builtin counter. Guards against an overzealous match
		// rule grabbing unrelated identifiers.
		var shape = Walk(
			"function helper() {}\n" +
			"helper(); helper(); helper();");

		Assert.Null(shape.BuiltinCounts.FromAll);
		Assert.Null(shape.BuiltinCounts.When);
	}

	[Fact]
	public void MethodShorthandHandlers_Detected() {
		// ES6 method-shorthand: { $any(s, e) { ... } }. Acornima
		// emits a Property whose Key is Identifier("$any") and
		// Value is FunctionExpression. The walker must classify
		// these identically to long-form `$any: function() { ... }`.
		var shape = Walk("fromAll().when({ $any(s, e) { return s; }, Order(s, e) { return s; } });");

		Assert.True(shape.Handlers.Any);
		Assert.Equal(1, shape.Handlers.DistinctEventNames);
	}

	[Fact]
	public void GetterPropertyHandler_StillCountsTheKey() {
		// `when({ get $any() { ... } })` is structurally weird but
		// legal JS. Acornima emits Property.Kind=Get; the walker
		// classifies by the key name, not the property kind. Pin
		// this so a future refactor doesn't drop it without notice.
		var shape = Walk("fromAll().when({ get $any() { return null; } });");

		Assert.True(shape.Handlers.Any);
	}

	[Fact]
	public void SpreadInWhenObject_DoesNotCrashOrLeak() {
		// `when({ ...userHandlers })`: SpreadElement isn't a
		// Property, so PropertyKeyName never sees it. Walker must
		// not crash and must not falsely populate anything from a
		// spread it can't resolve statically.
		var shape = Walk(
			"var userHandlers = {};\n" +
			"fromAll().when({ ...userHandlers, Order: function(s, e) { return s; } });");

		Assert.Equal(1, shape.Handlers.DistinctEventNames);
		Assert.False(shape.Handlers.Any);
	}

	[Fact]
	public void NumericKey_Ignored() {
		// `when({ 0: fn })` - NumericLiteral key falls through
		// PropertyKeyName's `_ => null`. Real projections don't do
		// this; the walker drops it silently.
		var shape = Walk("fromAll().when({ 0: function(s, e) { return s; } });");

		Assert.Equal(0, shape.Handlers.DistinctEventNames);
		Assert.False(shape.Handlers.Any);
	}

	[Fact]
	public void TemplateLiteralKey_AlwaysComputed_Ignored() {
		// `when({ [`A${i}`]: fn })` - always Computed:true; rejected
		// at the property-key gate. Same as any other computed key.
		var shape = Walk(
			"var i = 1;\n" +
			"fromAll().when({ [`A${i}`]: function(s, e) { return s; } });");

		Assert.Equal(0, shape.Handlers.DistinctEventNames);
	}

	[Fact]
	public void MemberCallToTopLevelBuiltinName_NotCounted() {
		// `myService.fromAll()` should NOT bump FromAll - the
		// top-level builtins (fromAll, fromStream, fromCategor*)
		// are projection-API entries; using the same name as a
		// method on an unrelated object is user code, not a
		// projection builtin call. Similarly `obj.emit("foo")` is
		// a user call.
		var shape = Walk(
			"var myService = { fromAll: function() {}, emit: function() {} };\n" +
			"myService.fromAll();\n" +
			"myService.emit('not-a-projection-call');");

		Assert.Null(shape.BuiltinCounts.FromAll);
		Assert.Null(shape.BuiltinCounts.Emit);
	}

	[Fact]
	public void TopLevelCallToChainedBuiltinName_NotCounted() {
		// `when({...})` at top-level (not chained) is some user
		// function named `when`, not the projection builtin. Don't
		// count it - the chained `.when(...)` is the projection
		// pattern.
		var shape = Walk(
			"function when(handlers) { return handlers; }\n" +
			"when({ Order: function(s, e) { return s; } });");

		Assert.Null(shape.BuiltinCounts.When);
		// Likewise the harvest-handlers path must NOT fire on the
		// top-level call (since that path is keyed to the chained
		// dispatch arm).
		Assert.Equal(0, shape.Handlers.DistinctEventNames);
	}

	[Fact]
	public void ProjectionShape_ExposesNoStringFields_PrivacyInvariant() {
		// Structural guard for the privacy claim "no source content
		// (event names, stream names, projection text) crosses the
		// FFI". Walks every property reachable from ProjectionShape
		// and asserts no string-typed field exists. A maintainer
		// who adds `string[] EventNames` to Handlers (or any
		// string field at any depth) trips this test before the
		// data ever leaves the .NET side.
		var visited = new HashSet<Type>();
		AssertNoStringProperties(typeof(ProjectionShape), visited);

		static void AssertNoStringProperties(Type t, HashSet<Type> visited) {
			if (!visited.Add(t)) return;
			foreach (var prop in t.GetProperties(BindingFlags.Public | BindingFlags.Instance)) {
				var pt = Nullable.GetUnderlyingType(prop.PropertyType) ?? prop.PropertyType;
				Assert.False(
					pt == typeof(string) || pt == typeof(string[]),
					$"{t.Name}.{prop.Name} has type {prop.PropertyType} - ProjectionShape must not expose strings to the wire (privacy invariant). " +
					$"If a new string field is genuinely needed, weigh the leak vector and update this test deliberately.");
				if (pt.IsClass && pt != typeof(string) && pt.Namespace?.StartsWith("Gaffer.Sdk") == true) {
					AssertNoStringProperties(pt, visited);
				}
			}
		}
	}

	[Fact]
	public void RealisticProjection_AllFieldsPopulated() {
		// End-to-end happy-path: a typical v2 projection touching
		// every category. Confirms the walker doesn't drop pieces
		// when they appear together.
		var shape = Walk(
			"fromAll()\n" +
			"  .when({\n" +
			"    $init: function() { return { n: 0 }; },\n" +
			"    $any: function(s, e) { s.n++; return s; },\n" +
			"    OrderPlaced: function(s, e) { s.n++; return s; },\n" +
			"    OrderShipped: function(s, e) { s.n++; return s; },\n" +
			"  })\n" +
			"  .outputState()\n" +
			"  .transformBy(function(s) { return s.n; })\n" +
			"  .partitionBy(function(e) { return e.streamId; })\n" +
			"  .foreachStream();");

		Assert.True(shape.Handlers.Init);
		Assert.True(shape.Handlers.Any);
		Assert.False(shape.Handlers.Deleted);
		Assert.Equal(2, shape.Handlers.DistinctEventNames);
		Assert.Equal(1, shape.BuiltinCounts.FromAll);
		Assert.Equal(1, shape.BuiltinCounts.When);
		Assert.Equal(1, shape.BuiltinCounts.OutputState);
		Assert.Equal(1, shape.BuiltinCounts.TransformBy);
		Assert.Equal(1, shape.BuiltinCounts.PartitionBy);
		Assert.Equal(1, shape.BuiltinCounts.ForeachStream);
	}
}
