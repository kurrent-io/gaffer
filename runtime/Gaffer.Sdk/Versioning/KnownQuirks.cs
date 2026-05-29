namespace Gaffer.Sdk.Versioning;

/// <summary>
/// Registry of every KurrentDB quirk gaffer reproduces. Adding a new quirk means
/// adding an entry here and gating the quirky code path on
/// <c>quirk.FiresAt(quirksVersion)</c>.
/// <para>
/// File references in descriptions point at upstream
/// <c>KurrentDB.Projections.JavaScript/Services/Interpreted/JintProjectionStateHandler.cs</c>
/// at tag <c>v26.1.0</c>.
/// </para>
/// </summary>
public static class KnownQuirks {
	/// <summary>
	/// <c>linkStreamTo</c> with metadata (3+ args) crashes in upstream because
	/// the metadata branch reads <c>parameters.At(4)</c> while the arity check
	/// gates on <c>parameters.Length == 3</c>. Index 4 is out of bounds for a
	/// 3-arg call, so <c>.AsObject()</c> throws "Argument is not an object".
	/// Net upstream behaviour: 2-arg <c>linkStreamTo(s, l)</c> works; 3-arg
	/// <c>linkStreamTo(s, l, md)</c> throws. Metadata is never captured.
	/// </summary>
	public static readonly Quirk LinkStreamToOutOfBoundsParameters = new() {
		Code = "compat.linkStreamTo.outOfBoundsParameters",
		Description = "linkStreamTo with metadata (3+ args) crashes; metadata is never captured.",
		FixedIn = null, // no upstream PR in flight
	};

	/// <summary>
	/// Multi-param <c>log()</c> has stacked quirks in upstream: separator gate
	/// is <c>i &gt; 1</c> instead of <c>&gt; 0</c>; separator string is
	/// <c>" ,"</c> (space-comma); primitives in the multi-param branch are
	/// logged directly inside the loop instead of appended to the buffer.
	/// Net behaviour: <c>log("a", "b", "c")</c> emits three separate log
	/// lines; <c>log({a:1}, {b:2})</c> emits one line <c>"{...} ,{...}"</c>.
	/// </summary>
	public static readonly Quirk LogMultiParam = new() {
		Code = "compat.log.multiParam",
		Description = "Multi-param log() emits primitives as separate lines and uses ' ,' separator for objects.",
		FixedIn = null, // no upstream PR
	};

	/// <summary>
	/// Accessing <c>event.body</c> throws <c>InvalidCastException</c> when
	/// <c>bodyRaw</c> is <c>null</c>, a number, a string, or a boolean. The
	/// upstream <c>EnsureBody</c> performs <c>(ObjectInstance)body</c> without
	/// checking the parsed type. Fixed in PR #5610 by removing the cast.
	/// </summary>
	public static readonly Quirk EventBodyCast = new() {
		Code = "compat.event.bodyCast",
		Description = "Accessing event.body throws InvalidCastException for non-object event bodies (null, primitive).",
		FixedIn = null, // PR #5610 open, expected 26.1.1
	};

	/// <summary>
	/// BiState <c>PrepareOutput</c> JSON-quotes raw string values in slot 0 of
	/// the state array. Upstream checks <c>_state.IsString()</c> (the array
	/// itself) instead of <c>state.IsString()</c> (the slot-0 element), so
	/// the string-passthrough branch is unreachable for biState. Fixed in PR
	/// #5610 by checking the inner element.
	/// </summary>
	public static readonly Quirk BiStateStringSlot = new() {
		Code = "compat.biState.stringSlot",
		Description = "BiState state JSON-quotes raw string values in slot 0 instead of passing them through.",
		FixedIn = null, // PR #5610
	};

	/// <summary>
	/// BiState <c>PrepareOutput</c> JSON-quotes raw string values in slot 1
	/// (the shared state). Unlike slot 0, upstream has no string-passthrough
	/// branch for shared state at all - it always runs
	/// <c>ConvertToStringHandlingNulls</c>. PR #5610's slot-0 fix does not
	/// touch this, so it has no fix version. Tracked separately from
	/// <see cref="BiStateStringSlot"/> because the upstream fix is partial.
	/// </summary>
	public static readonly Quirk BiStateSharedStringSlot = new() {
		Code = "compat.biState.sharedStringSlot",
		Description = "BiState shared state (slot 1) JSON-quotes raw string values instead of passing them through.",
		FixedIn = null, // no upstream fix; PR #5610 only fixed slot 0
	};

	/// <summary>
	/// JSON serializer throws <c>ArgumentException</c> on <c>NaN</c> or
	/// <c>+/-Infinity</c> in state because <c>Utf8JsonWriter.WriteNumberValue</c>
	/// rejects non-finite doubles regardless of <c>SkipValidation</c>. Fixed in
	/// PR #5610 by guarding with <c>double.IsFinite</c>; non-finite values
	/// then serialize as JSON <c>null</c>, matching <c>JSON.stringify</c>.
	/// </summary>
	public static readonly Quirk SerializeNonFinite = new() {
		Code = "compat.serialize.nonFinite",
		Description = "JSON serializer throws ArgumentException on NaN/Infinity instead of writing null.",
		FixedIn = null, // PR #5610
	};

	/// <summary>All known quirks, in registry order. Useful for enumeration in tests, MCP resources, and docs.</summary>
	public static readonly IReadOnlyList<Quirk> All = new[] {
		LinkStreamToOutOfBoundsParameters,
		LogMultiParam,
		EventBodyCast,
		BiStateStringSlot,
		BiStateSharedStringSlot,
		SerializeNonFinite,
	};
}
